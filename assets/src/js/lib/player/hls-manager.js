/**
 * HLS.js manager — setup, error handling, track remapping.
 * Extracted from mediaelement.js lines 30-143.
 */
import Hls from 'hls.js';

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
    hls.loadSource(sourceUrl);
    hls.attachMedia(videoEl);

    setupHlsEvents(hls);

    if (onReady) {
        hls.on(Hls.Events.MANIFEST_PARSED, onReady);
    }

    return hls;
}

function setupHlsEvents(hls) {
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
            if (data.type === Hls.ErrorTypes.MEDIA_ERROR && data.details === 'bufferStalledError') {
                setTimeout(() => hls.startLoad(), 5000);
            }
        }
    });
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

function remapTrackGroup(elements, hlsTracks) {
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
        const label = el.textContent.trim();

        let matched = false;
        // Exact lang+name match
        for (let i = 0; i < hlsTracks.length; i++) {
            if (used.has(i)) continue;
            const hlsLang = (hlsTracks[i].lang || '').toLowerCase();
            const hlsName = (hlsTracks[i].name || '').trim();
            if (lang && hlsLang && lang === hlsLang && label && hlsName && label === hlsName) {
                el.setAttribute('data-mp-id', String(i));
                used.add(i);
                matched = true;
                break;
            }
        }
        if (matched) continue;

        // Fallback: lang only
        for (let i = 0; i < hlsTracks.length; i++) {
            if (used.has(i)) continue;
            const hlsLang = (hlsTracks[i].lang || '').toLowerCase();
            if (lang && hlsLang && lang === hlsLang) {
                el.setAttribute('data-mp-id', String(i));
                used.add(i);
                matched = true;
                break;
            }
        }
        if (matched) continue;

        // Fallback: name only
        for (let i = 0; i < hlsTracks.length; i++) {
            if (used.has(i)) continue;
            const hlsName = (hlsTracks[i].name || '').trim();
            if (label && hlsName && label === hlsName) {
                el.setAttribute('data-mp-id', String(i));
                used.add(i);
                break;
            }
        }
    }
}

/**
 * Initialize default audio/subtitle tracks from DOM data-default attributes.
 */
export function initDefaultTracks(hls) {
    const defaultAudio = document.querySelector('.audio[data-default=true]');
    const defaultSub = document.querySelector('.subtitle[data-default=true]');
    const audioId = defaultAudio ? defaultAudio.getAttribute('data-mp-id') : null;
    const subId = defaultSub ? defaultSub.getAttribute('data-mp-id') : null;
    if (audioId) hls.audioTrack = parseInt(audioId);
    if (subId) {
        hls.subtitleDisplay = true;
        hls.subtitleTrack = parseInt(subId);
    }
}
