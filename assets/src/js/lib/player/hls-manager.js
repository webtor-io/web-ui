/**
 * HLS.js manager — setup, error handling, track remapping.
 * Extracted from mediaelement.js lines 30-143.
 */
import Hls from 'hls.js';
import { applySubtitleSelection, selectionFor } from './subtitle-apply.js';
import { markUnsnapshottedTracksStale } from './subtitle-track-reload.js';
import { createLoaderRestart } from './loader-restart.js';

const HLS_CONFIG = {
    autoStartLoad: true,
    startPosition: 0,
    manifestLoadingTimeOut: 1000 * 60 * 10,
    manifestLoadingMaxRetry: 100,
    manifestLoadingMaxRetryTimeout: 1000 * 10,
    levelLoadingMaxRetry: 100,
    levelLoadingMaxRetryTimeout: 1000 * 10,
    fragLoadingMaxRetry: 100,
    fragLoadingMaxRetryTimeout: 1000 * 10,
    maxBufferSize: 50 * 1000 * 1000,
    maxMaxBufferLength: 180,
};

/**
 * Create and attach HLS.js instance to a video element.
 * Returns { hls, destroy } or null if HLS is not supported / not needed.
 */
export { Hls };

// iOS/iPadOS detection — use native HLS there (ManagedMediaSource is unreliable)
const isIOS = /iPad|iPhone|iPod/.test(navigator.userAgent) ||
    (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);

export function createHls(videoEl, sourceUrl, onReady) {
    if (!Hls || !Hls.isSupported() || isIOS) {
        // Native HLS (Safari/iOS) — browser handles m3u8 natively
        if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
            videoEl.src = sourceUrl;
            videoEl.load();
            if (onReady) videoEl.addEventListener('loadedmetadata', onReady, { once: true });
            return null;
        }
        return null;
    }

    const hls = new Hls(HLS_CONFIG);
    // The initial loadSource wipes the cues of every <track> already on the
    // element, exactly as a seek's does (TimelineController._cleanTracks on
    // MANIFEST_LOADING) — and a saved side-loaded selection is restored
    // *before* the HLS instance exists, so its track can be loaded, showing
    // and empty for the whole session (reproduced on stage 2026-09-17: a
    // restored OpenSubtitles track at readyState 2, 353 cues in the file,
    // 0 in the track, no seek anywhere). Marked here the way the seeker
    // marks before its loadSource; the re-assert that runs on the manifest
    // events then refetches the one that is actually showing
    // (applySubtitleSelection → refreshStaleTrack).
    markUnsnapshottedTracksStale(Array.from(videoEl.querySelectorAll('track')), []);
    hls.loadSource(sourceUrl);
    hls.attachMedia(videoEl);

    setupHlsEvents(hls);

    if (onReady) {
        hls.on(Hls.Events.MANIFEST_PARSED, onReady);
    }

    return hls;
}

// Exported for the tests (loader-restart.test.js), with the loader restart's
// options: the error handling every instance goes through.
export function setupHlsEvents(hls, restartOpts) {
    hls.on(Hls.Events.MANIFEST_PARSED, (event, data) => {
        if (hls.levels.length > 1) {
            hls.startLevel = 1;
        }
        remapTrackIds(hls);
    });

    hls.on(Hls.Events.ERROR, (event, data) => {
        if (data.fatal) {
            switch (data.type) {
                case Hls.ErrorTypes.NETWORK_ERROR:
                    if (data.details === 'levelParsingError') {
                        setTimeout(() => hls.startLoad(), 3000);
                    } else {
                        hls.startLoad();
                    }
                    break;
                case Hls.ErrorTypes.MEDIA_ERROR:
                    hls.recoverMediaError();
                    break;
                default:
                    hls.destroy();
                    break;
            }
        } else {
            console.warn('HLS non-fatal error:', data.type, data.details);
        }
    });

    // A stall (the non-fatal bufferStalledError) restarts the loader only
    // when nothing is loading: at the plan's cap the segment the player
    // waits for is still arriving, and startLoad() would abort it
    // (loader-restart.js).
    return createLoaderRestart(hls, Hls, restartOpts);
}

/**
 * Remap track IDs between HTML elements and HLS.js track indices.
 */
export function remapTrackIds(hls) {
    const containers = document.querySelectorAll('#subtitles');
    if (!containers.length) return;

    const hlsSubTracks = hls.subtitleTracks || [];
    const hlsAudioTracks = hls.audioTracks || [];

    for (const container of containers) {
        const subEls = Array.from(container.querySelectorAll('.subtitle[data-provider="MediaProbe"]'));
        remapTrackGroup(subEls, hlsSubTracks);

        const audioEls = Array.from(container.querySelectorAll('.audio[data-provider="MediaProbe"]'));
        remapTrackGroup(audioEls, hlsAudioTracks);
    }
}

/**
 * Pair rendered track elements with HLS.js track indices.
 *
 * Equal counts mean the manifest and the picker list the same tracks in
 * the same order, so a sequential assignment is exact.
 *
 * Differing counts are a normal state since GetSubtitles started hiding
 * bitmap and "forced" embedded tracks (handlers/action/helper.go): the
 * hidden track still occupies an index in the transcoder's HLS group,
 * so there are more manifest tracks than elements. In that case the
 * server-rendered `data-mp-id` is authoritative and already correct —
 * only an exact lang+name match may refine it. The old lang-only and
 * name-only fallbacks are deliberately gone: with a hidden neighbour
 * sharing the language they hand a visible element the hidden track's
 * index, which plays the wrong (or no) subtitle.
 */
export function remapTrackGroup(elements, hlsTracks) {
    if (!elements.length || !hlsTracks.length) return;

    if (elements.length === hlsTracks.length) {
        for (let i = 0; i < elements.length; i++) {
            elements[i].setAttribute('data-mp-id', String(i));
        }
        return;
    }

    const used = new Set();
    for (const el of elements) {
        const lang = (el.getAttribute('data-srclang') || '').toLowerCase();
        // data-label, not textContent: a picker chip's text now includes the
        // origin code ("EM", "OS~") and the property tag ("forced"), so an
        // exact compare against the manifest's
        // track name would never match again.
        const label = (el.getAttribute('data-label') || el.textContent || '').trim();
        if (!lang || !label) continue;

        for (let i = 0; i < hlsTracks.length; i++) {
            if (used.has(i)) continue;
            const hlsLang = (hlsTracks[i].lang || '').toLowerCase();
            const hlsName = (hlsTracks[i].name || '').trim();
            if (hlsLang && hlsName && lang === hlsLang && label === hlsName) {
                el.setAttribute('data-mp-id', String(i));
                used.add(i);
                break;
            }
        }
    }
}

/**
 * Initialize default audio/subtitle tracks from DOM data-default attributes.
 *
 * The subtitle half goes through applySubtitleSelection, which is what makes
 * a side-loaded default (an upload, an OpenSubtitles track, a translation
 * restored from an earlier session) turn hls.js's own subtitles OFF here.
 * Reading `data-mp-id` and stopping when there was none used to leave
 * `subtitleDisplay` at hls.js's default of `true` with no track selected —
 * and the first textTracks change event after that handed hls.js a track of
 * its own choosing, which it then drew over the chosen one. The activation
 * that ran at mount could not do this itself: it happens before the HLS
 * instance exists.
 */
export function initDefaultTracks(hls, video) {
    const defaultAudio = document.querySelector('.audio[data-default=true]');
    const defaultSub = document.querySelector('.subtitle[data-default=true]');
    const audioId = defaultAudio ? defaultAudio.getAttribute('data-mp-id') : null;
    if (audioId) hls.audioTrack = parseInt(audioId);
    // No picker (a bare embed) means no answer to apply: hls.js keeps
    // whatever the manifest declared.
    const selection = selectionFor(defaultSub);
    if (!selection) return;
    applySubtitleSelection(video || document.querySelector('video.player, audio.player'), hls, selection);
}
