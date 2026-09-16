import { render } from 'preact';
import { useRef, useState, useEffect, useCallback } from 'preact/hooks';
import { usePlayerState } from './hooks/usePlayerState';
import { useHls } from './hooks/useHls';
import { useWatchHistory } from './hooks/useWatchHistory';
import { createSessionSeeker } from './session-seek';
import { applyCueOffset } from './cue-offset';
import { reloadSubtitleTrack, dropDeletedTracks } from './subtitle-track-reload.js';
import { readAllTracks, readTracks, resolveSubtitleLevel, selectEventData } from './subtitle-telemetry.js';
import { pickDefaultSubtitle, translationAction, hasSavedDefault } from './subtitle-rules.js';
import { pollProgress, withRev } from './subtitle-progress.js';
import {
    adoptUploadChips,
    refresh,
    refreshMarks,
    applyLangFilter,
    applyFlagSupport,
    applyOffState,
    offStateAfterActivate,
    restoreSavedTranslation,
    toggleDecision,
    setChipActive,
    expandedLang,
    toggleLangOverflow,
} from './track-picker.js';
import { Controls } from './Controls';
import { LoadingSpinner, ShareIcon } from './icons';
import { init as initI18n, t, tf } from './i18n';
import { getLang } from '../i18n';
import { shareResource } from '../share/share';
import '../../../styles/player.css';

let _currentPlayer = null;

// ENGAGEMENT_SECONDS is the playback time (not wall clock) after which a
// session counts as real viewing, so press-play-and-bounce does not skew
// the denominator. Telemetry only since 2026-09-16 — the AI translation
// auto-start used to hang off it and no longer exists.
const ENGAGEMENT_SECONDS = 5;

// TRACK_RELOAD_INTERVAL_MS throttles the <track> src swaps. Every swap
// refetches the whole partial VTT and leaves the track without cues
// while the browser reparses it, so reloading on each 3 s poll would
// blank the subtitles five times a minute. The percentage keeps updating
// on every poll; only the text catches up in steps.
const TRACK_RELOAD_INTERVAL_MS = 15000;

// Cast sender SDK loader — module-level so repeated player inits (one per
// file click) share a single <script> append and a single
// window.__onGCastApiAvailable callback. Without this every init while the
// SDK was still loading (or forever, with gstatic blocked by an adblocker)
// appended another script tag and overwrote the previous player's callback.
let castSenderPromise = null;
function loadCastSender() {
    if (window.cast) return Promise.resolve(true);
    if (!castSenderPromise) {
        castSenderPromise = new Promise((resolve) => {
            window.__onGCastApiAvailable = (available) => resolve(!!available);
            const s = document.createElement('script');
            s.src = 'https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1';
            s.onerror = () => resolve(false);
            document.body.appendChild(s);
        });
    }
    return castSenderPromise;
}

/**
 * Main Player Preact component.
 * Wraps <video>/<audio>, renders custom controls, manages HLS + session seeking.
 */
function PlayerComponent({ videoEl, settings, containerEl, showControls, fixedSize, trackContainer, trackHooks }) {
    const containerRef = useRef(containerEl);
    const videoRef = useRef(videoEl);
    const [seekOffset, setSeekOffset] = useState(0);
    const [sessionSeeking, setSessionSeeking] = useState(false);
    const sessionSeekingRef = useRef(false);
    const [controlsVisible, setControlsVisible] = useState(true);
    const [castAvailable, setCastAvailable] = useState(false);
    const hideTimerRef = useRef(null);
    const sessionSeekerRef = useRef(null);

    const isVideo = videoEl.tagName === 'VIDEO';
    const duration = videoEl.getAttribute('data-duration') ? parseFloat(videoEl.getAttribute('data-duration')) : -1;
    const sessionId = videoEl.dataset.sessionId;
    const sessionSeekUrl = videoEl.dataset.sessionSeekUrl;
    const sessionDeletePath = videoEl.dataset.sessionDeletePath;
    const isSession = !!sessionId;
    const graceDurationSec = videoEl.dataset.graceDurationSec ? parseInt(videoEl.dataset.graceDurationSec, 10) : 0;
    const graceShownRef = useRef(false);
    // streamStarted is tracked via a ref, not state — the value is only
    // read to gate the one-shot Umami event below, never to drive UI, so
    // a re-render on transition would be pure overhead.
    const streamStartFiredRef = useRef(false);
    const poster = videoEl.getAttribute('poster');
    const resourceID = videoEl.dataset.resourceId;
    const path = videoEl.dataset.path;

    // Source URL from first <source> element
    const sourceEl = videoEl.querySelector('source');
    const sourceUrl = sourceEl ? sourceEl.getAttribute('src') : videoEl.src;

    // Parse features from settings
    const features = parseFeatures(settings, isVideo, duration, isSession);

    // Fetch initial seek offset from transcoder session
    useEffect(() => {
        if (!isSession || !sessionSeekUrl) return;
        fetch(sessionSeekUrl)
            .then(r => r.json())
            .then(data => {
                if (data.offset > 0) setSeekOffset(data.offset);
            })
            .catch(() => {}); // ignore — offset stays 0
    }, []);

    // Element-backed subtitle tracks (user uploads, OpenSubtitles, external)
    // carry cues in absolute movie time, while a transcoder session that
    // starts mid-movie (resume, seek) exposes a timeline that begins at
    // zero. Shift the cues by the session offset — and re-shift tracks that
    // finish loading later ('load' doesn't bubble, so listen in capture;
    // this also covers tracks added mid-session from the My Subtitles tab).
    useEffect(() => {
        const video = videoRef.current;
        if (!video || !isSession) return;
        const applyAll = () => {
            for (const el of video.querySelectorAll('track')) {
                if (el.track) applyCueOffset(el.track, seekOffset);
            }
        };
        applyAll();
        const onTrackLoad = (e) => {
            if (e.target.tagName === 'TRACK' && e.target.track) {
                applyCueOffset(e.target.track, seekOffset);
            }
        };
        video.addEventListener('load', onTrackLoad, true);
        return () => video.removeEventListener('load', onTrackLoad, true);
    }, [seekOffset, isSession]);

    // --- AI subtitle translation ------------------------------------
    // The translated .vtt is written cue by cue, so the player polls the
    // progress header and reloads the <track> with a bumped &rev= as
    // lines arrive instead of waiting for the whole file.
    //
    // manualSubtitleRef records that the viewer picked a subtitle
    // themselves, which switches the audio-language rule off for the
    // rest of the session: re-deciding over an explicit choice reads as
    // the player fighting the viewer.
    const manualSubtitleRef = useRef(false);
    // Whether the saved translation this page opened with has been brought
    // back. The effect that does it can re-run (its callbacks are
    // dependencies); the restore must happen once.
    const savedTranslationRef = useRef(false);
    const pollStopRef = useRef(null);
    // What this page load already did to each AI item: id → 'running' |
    // 'done'. translationAction turns it into start / resume / none, so
    // a warm cache cannot double-count and an interrupted run can be
    // picked up again without a second start event.
    const translationStatusRef = useRef(new Map());
    // When each item's first run began. A resumed run reports its total
    // wall time, not the time since the resume.
    const translationStartedAtRef = useRef(new Map());
    // The item the running poll belongs to, so re-selecting it does not
    // kill its own progress.
    const pollingIdRef = useRef('');
    // Monotonic run id. A <track> error can arrive long after the run that
    // asked for that revision was stopped (the element keeps loading), and
    // without an identity check the late event would kill whatever run is
    // current and report an error against the wrong translation.
    const runSeqRef = useRef(0);
    // The ".tr-progress" span of the running poll, so stopping the poll can
    // hide it. Left visible it freezes at whatever percent was last seen and
    // reads as a translation stuck forever.
    const progressSpanRef = useRef(null);
    // The chip's spinner, paired with progressSpanRef: a spinner left
    // running after the poll stops says a translation is still going.
    const progressSpinnerRef = useRef(null);

    const stopTranslationProgress = useCallback(() => {
        pollingIdRef.current = '';
        // Any event from the run being stopped is now stale.
        runSeqRef.current++;
        if (progressSpanRef.current) {
            progressSpanRef.current.hidden = true;
            progressSpanRef.current = null;
        }
        if (progressSpinnerRef.current) {
            progressSpinnerRef.current.hidden = true;
            progressSpinnerRef.current = null;
        }
        if (pollStopRef.current) {
            pollStopRef.current();
            pollStopRef.current = null;
        }
    }, []);

    // startTranslationProgress polls one translation to completion.
    // Callers ask translationAction first; `resume` is its 'resume'
    // answer — same run, so no second subtitle-translate-start.
    const startTranslationProgress = useCallback((el, resume = false) => {
        stopTranslationProgress();
        const src = el.getAttribute('data-src') || '';
        const id = el.getAttribute('data-id') || '';
        if (!src || !id) return;
        translationStatusRef.current.set(id, 'running');
        if (!resume || !translationStartedAtRef.current.has(id)) {
            translationStartedAtRef.current.set(id, Date.now());
        }
        pollingIdRef.current = id;
        // stopTranslationProgress above already bumped the counter; this run
        // owns the value it left behind.
        const runID = runSeqRef.current;
        const lang = el.getAttribute('data-srclang') || '';
        const span = el.querySelector('.tr-progress');
        const spinner = el.querySelector('.tr-spinner');
        progressSpanRef.current = span;
        progressSpinnerRef.current = spinner;
        const startedAt = translationStartedAtRef.current.get(id);
        let cues = 0;
        let lastReloadAt = 0;
        let trackErrorReported = false;
        if (span) {
            span.hidden = false;
            // Design (docs/uikit.html §19): the chip says "· 0%". The
            // sentence "Translating… 0%" is still the one localized string,
            // and it lives in the title.
            span.textContent = '· 0%';
            span.title = tf('player.subtitleTranslating', 0);
        }
        if (spinner) spinner.hidden = false;
        const fail = (code) => {
            pollStopRef.current = null;
            pollingIdRef.current = '';
            if (progressSpanRef.current === span) progressSpanRef.current = null;
            if (progressSpinnerRef.current === spinner) progressSpinnerRef.current = null;
            if (span) span.hidden = true;
            if (spinner) spinner.hidden = true;
            if (window.umami) window.umami.track('subtitle-translate-error', { lang, code });
        };
        // One 'track' error per run: a broken revision usually stays
        // broken, and a report per poll would drown the real rate. The
        // identity check is the other half — a <track> whose src this run
        // set can still fail after the run was stopped, and that event
        // belongs to nobody.
        const onTrackError = () => {
            if (runID !== runSeqRef.current) return;
            if (trackErrorReported) return;
            trackErrorReported = true;
            stopTranslationProgress();
            fail('track');
        };
        const reload = (done, force) => {
            const now = Date.now();
            if (!force && now - lastReloadAt < TRACK_RELOAD_INTERVAL_MS) return;
            // Only a reload that actually swapped the src spends the
            // throttle window: stamping on a no-op (same revision, or no
            // <track> element yet) would hold off the next real one for
            // another 15 s.
            if (reloadSubtitleTrack(videoRef.current, id, withRev(src, done), onTrackError)) {
                lastReloadAt = now;
            }
        };
        if (!resume && window.umami) window.umami.track('subtitle-translate-start', {
            lang,
            // Which human track the machine works from: a translation of
            // an OpenSubtitles imdb match is a weaker claim than one of
            // the viewer's own upload.
            source: el.getAttribute('data-source-badge') || '',
        });
        pollStopRef.current = pollProgress(src, {
            onProgress: (p) => {
                cues = p.total;
                const pct = p.total > 0 ? Math.round((100 * p.done) / p.total) : 0;
                if (span) {
                    span.textContent = `· ${pct}%`;
                    span.title = tf('player.subtitleTranslating', pct);
                }
                // total === 0 means the job has not counted the cues yet:
                // the file on the other end is still empty, so a reload
                // would only replace subtitles with nothing.
                if (p.total > 0) reload(p.done, false);
            },
            onDone: (p) => {
                pollStopRef.current = null;
                pollingIdRef.current = '';
                if (progressSpanRef.current === span) progressSpanRef.current = null;
                if (progressSpinnerRef.current === spinner) progressSpinnerRef.current = null;
                // Final: a later re-selection must neither poll nor
                // report this translation again.
                translationStatusRef.current.set(id, 'done');
                cues = p.total || cues;
                reload(p.done, true);
                if (span) span.hidden = true;
                if (spinner) spinner.hidden = true;
                if (window.umami) window.umami.track('subtitle-translate-done', {
                    lang,
                    seconds: Math.round((Date.now() - startedAt) / 100) / 10,
                    cues,
                });
            },
            onError: fail,
        });
    }, [stopTranslationProgress]);

    // translationActionFor answers translationAction for a list element,
    // with one addition the pure rule cannot know: the item whose poll is
    // running right now needs nothing done to it at all.
    const translationActionFor = useCallback((el) => {
        const data = itemData(el);
        if (data.id && data.id === pollingIdRef.current) return 'none';
        return translationAction(data, translationStatusRef.current);
    }, []);

    // Track-list hooks. wireTrackHandlers() runs before this component
    // mounts (initPlayer wires the modals first), so it is handed a plain
    // object that stays empty until this effect fills it in: the refs and
    // tf() belong to the component that owns them, and the module-scope
    // handler only calls through.
    useEffect(() => {
        if (!trackHooks) return;
        // A default the viewer saved earlier (ud.SubtitleID, rendered as
        // data-saved) is already a manual choice — made in an earlier
        // session rather than this one. Without this seed the audio
        // switch would re-decide over it and turn off subtitles the
        // viewer had explicitly asked for.
        // readAllTracks, not readTracks: "None" is a saved choice like any
        // other, and readTracks drops it. Missing it meant an audio switch
        // turned subtitles back on over an explicit off.
        const modalAtMount = findSubtitlesModal(trackContainer);
        if (modalAtMount && hasSavedDefault(readAllTracks(modalAtMount))) {
            manualSubtitleRef.current = true;
        }
        trackHooks.onSubtitleSelect = (el) => {
            manualSubtitleRef.current = true;
            // Decided before stopping anything: stopTranslationProgress
            // clears pollingIdRef, which is part of the answer.
            const action = translationActionFor(el);
            // Clicking the item that is translating right now (a viewer
            // who thinks nothing happened) must not stop its own poll.
            if (itemData(el).id !== pollingIdRef.current) stopTranslationProgress();
            if (action !== 'none') startTranslationProgress(el, action === 'resume');
        };
        trackHooks.onAudioSelect = (el) => {
            if (manualSubtitleRef.current) return;
            const modal = findSubtitlesModal(trackContainer);
            if (!modal) return;
            const id = pickDefaultSubtitle(
                readTracks(modal),
                el.getAttribute('data-srclang') || '',
                modal.getAttribute('data-preferred-lang') || '',
            );
            const item = findSubtitleItem(modal, id);
            // Already the active one — leave it, and any running poll, alone.
            if (!item || item.getAttribute('data-default') === 'true') return;
            const action = translationActionFor(item);
            stopTranslationProgress();
            // The audio rule decided this, not the viewer: marking it saved
            // would make the next page load treat the player's own pick as
            // an explicit choice and stop re-running the rule.
            activateSubtitle(trackContainer, item, { persist: false });
            if (action !== 'none') startTranslationProgress(item, action === 'resume');
        };
        // A translation the viewer chose in an earlier session comes back on
        // its own. Every other saved choice resumes from the <track> the
        // server rendered; a translation has none (markPreload skips it, or
        // opening the page would start a run for everyone), so it needs this
        // one call -- the same path a click takes, minus the PUT, since
        // nothing was chosen here that was not already chosen. The run is
        // the viewer's own and cached, which is why this is the one
        // automatic start left after the engagement gate went away.
        if (modalAtMount && trackContainer && !savedTranslationRef.current) {
            const id = restoreSavedTranslation(readAllTracks(modalAtMount));
            const el = id ? findSubtitleItem(modalAtMount, id) : null;
            if (el) {
                savedTranslationRef.current = true;
                activateSubtitle(trackContainer, el, { persist: false });
                trackHooks.onSubtitleSelect(el);
            }
        }
        return () => {
            trackHooks.onSubtitleSelect = null;
            trackHooks.onAudioSelect = null;
            stopTranslationProgress();
        };
    }, [trackHooks, trackContainer, startTranslationProgress, stopTranslationProgress, translationActionFor]);

    // Sync ref with state for use in closures that don't re-bind
    const setSessionSeekingWithRef = useCallback((val) => {
        sessionSeekingRef.current = val;
        setSessionSeeking(val);
    }, []);

    // Player state hook
    const state = usePlayerState(videoRef, containerRef, { duration, seekOffset, seeking: sessionSeeking });

    // HLS hook
    const hlsRef = useHls(videoRef, sourceUrl);

    // Resume prompt state — must be declared before useWatchHistory which reads it.
    const [showResumePrompt, setShowResumePrompt] = useState(false);

    // Watch history hook (position tracking + resume).
    // `paused` prevents overwriting saved position while resume prompt is open.
    const { resumePosition, resumeReady, forceSendPosition } = useWatchHistory(videoRef, {
        resourceID, path,
        currentTime: state.currentTime,
        duration: state.duration,
        playing: state.playing,
        paused: showResumePrompt,
    });

    // Seek handler (session or direct)
    const handleSeek = useCallback((time) => {
        if (sessionSeekingRef.current) return;
        if (isSession && sessionSeekUrl) {
            // Immediately show target position on timeline
            state.setCurrentTime(time);
            // Lazily create session seeker (works with HLS.js or native HLS)
            if (!sessionSeekerRef.current) {
                sessionSeekerRef.current = createSessionSeeker({
                    hls: hlsRef.current, // null for native HLS (iOS)
                    videoEl,
                    sessionSeekUrl,
                    sourceUrl,
                    onSeekOffsetChange: setSeekOffset,
                    onSeekingChange: setSessionSeekingWithRef,
                });
            }
            if (sessionSeekerRef.current) {
                sessionSeekerRef.current.seek(time);
            }
        } else {
            const video = videoRef.current;
            if (video) {
                const maxTime = video.duration && isFinite(video.duration) ? video.duration : time;
                video.currentTime = Math.min(time, maxTime);
            }
        }
    }, [isSession, sessionSeekUrl, sourceUrl]);

    // Auto-hide controls
    const resetHideTimer = useCallback(() => {
        setControlsVisible(true);
        if (hideTimerRef.current) clearTimeout(hideTimerRef.current);
        if (isVideo) {
            hideTimerRef.current = setTimeout(() => {
                if (!videoRef.current?.paused) setControlsVisible(false);
            }, 3000);
        }
    }, [isVideo]);

    useEffect(() => {
        resetHideTimer();
        return () => { if (hideTimerRef.current) clearTimeout(hideTimerRef.current); };
    }, []);

    // Show controls on pause
    useEffect(() => {
        if (!state.playing) setControlsVisible(true);
    }, [state.playing]);

    // Stream-start Umami event — fires once per player session when playback
    // actually advances past 5s (real engagement, not just press-play-and-bounce).
    // Canonical denominator for share-rate analysis (share-resource events
    // divided by stream-start sessions). No UI gating; the in-player share
    // button is always visible.
    useEffect(() => {
        if (streamStartFiredRef.current) return;
        if (state.currentTime < ENGAGEMENT_SECONDS) return;
        streamStartFiredRef.current = true;
        if (window.umami) window.umami.track('stream-start', {
            isVideo,
            isSession,
            resourceID: resourceID || '',
        });
        const modal = document.getElementById('subtitles');
        // getLang(), not document.documentElement.lang: the embed
        // layouts render <html> without a lang attribute, which would
        // report every embedded play as having no UI language.
        const uiLang = getLang();
        if (!modal) return;
        const audioEl = modal.querySelector('.audio[data-default="true"]');
        const audioLang = audioEl ? (audioEl.getAttribute('data-srclang') || '') : '';
        // The ladder ran on the preferred content language, so `needed`
        // is measured against that one; the UI language only stands in
        // when no preference is configured.
        const preferredLang = modal.getAttribute('data-preferred-lang') || '';
        if (window.umami) {
            window.umami.track('subtitle-resolved', {
                ...resolveSubtitleLevel(readTracks(modal), uiLang, { audioLang, preferredLang }),
                uiLang,
                audioLang,
            });
        }
        // No AI auto-start here any more (owner, 2026-09-16). The player
        // used to activate a server-defaulted translation once playback
        // passed this gate; the server offers a translation now and never
        // turns one on. A run begins on a click of the chip, on the switch
        // restoring data-last-subtitle, or on the mount-time restore of a
        // translation the viewer saved in an earlier session — all three
        // are the viewer's own choice, and the last two are already cached.
        // This effect is telemetry only.
    }, [state.currentTime, isVideo, isSession, resourceID]);

    // Grace soft CTA — fires once when movie-time crosses the grace window.
    // The popup is server-rendered by the action template (stream_video.html)
    // as a sibling of the player container, NOT inside containerRef — so we
    // search globally. CTA is a per-page singleton (one player per action page).
    // If the user is in fullscreen, exit first — the CTA lives outside the
    // fullscreen element and would be invisible otherwise.
    useEffect(() => {
        if (!graceDurationSec || graceShownRef.current) return;
        if (state.currentTime < graceDurationSec) return;
        const el = document.querySelector('#grace-cta');
        if (!el) return;
        graceShownRef.current = true;
        if (document.fullscreenElement) {
            document.exitFullscreen().catch(() => {});
        }
        el.classList.remove('hidden');
        if (window.umami) window.umami.track('grace-soft-cta-shown');
        const hide = (action) => {
            el.classList.add('hidden');
            if (window.umami) window.umami.track('grace-soft-cta-click', { action });
        };
        const closeBtn = el.querySelector('.grace-cta-close');
        if (closeBtn) closeBtn.addEventListener('click', () => hide('dismiss'), { once: true });
        const contBtn = el.querySelector('.grace-cta-continue');
        if (contBtn) contBtn.addEventListener('click', () => hide('continue'), { once: true });
    }, [state.currentTime, graceDurationSec]);

    // Keyboard shortcuts
    useEffect(() => {
        function onKeyDown(e) {
            if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
            if (sessionSeeking) return;
            switch (e.key) {
                case ' ':
                case 'k':
                    e.preventDefault();
                    state.togglePlay();
                    resetHideTimer();
                    break;
                case 'ArrowLeft':
                    e.preventDefault();
                    handleSeek(Math.max(0, state.currentTime - 15));
                    resetHideTimer();
                    break;
                case 'ArrowRight':
                    e.preventDefault();
                    handleSeek(Math.min(state.duration, state.currentTime + 15));
                    resetHideTimer();
                    break;
                case 'ArrowUp':
                    e.preventDefault();
                    state.setVolume(Math.min(1, state.volume + 0.1));
                    resetHideTimer();
                    break;
                case 'ArrowDown':
                    e.preventDefault();
                    state.setVolume(Math.max(0, state.volume - 0.1));
                    resetHideTimer();
                    break;
                case 'f':
                    e.preventDefault();
                    state.toggleFullscreen();
                    resetHideTimer();
                    break;
                case 'm':
                    e.preventDefault();
                    state.toggleMute();
                    resetHideTimer();
                    break;
            }
        }
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, [state.currentTime, state.duration, state.volume, state.playing, sessionSeeking]);

    // player_play / player_paused custom events
    useEffect(() => {
        let forcePaused = false;
        function onPlayerPaused() {
            videoRef.current?.pause();
            forcePaused = true;
        }
        function onPlayerPlay() {
            forcePaused = false;
            videoRef.current?.play().catch(() => {});
        }
        function onPlaying() {
            if (forcePaused) videoRef.current?.pause();
        }
        window.addEventListener('player_paused', onPlayerPaused);
        window.addEventListener('player_play', onPlayerPlay);
        videoEl.addEventListener('playing', onPlaying);
        return () => {
            window.removeEventListener('player_paused', onPlayerPaused);
            window.removeEventListener('player_play', onPlayerPlay);
            videoEl.removeEventListener('playing', onPlaying);
        };
    }, []);

    // Dispatch player_ready on canplay + set aspect-ratio from video
    useEffect(() => {
        let dispatched = false;
        function onCanPlay() {
            if (!dispatched) {
                dispatched = true;
                // Native HLS (iOS) starts at live edge — force start from beginning
                if (!hlsRef.current && videoEl.currentTime > 1) {
                    videoEl.currentTime = 0;
                }
                // Set container aspect-ratio from actual video dimensions
                if (videoEl.videoWidth && videoEl.videoHeight) {
                    containerEl.style.aspectRatio = `${videoEl.videoWidth} / ${videoEl.videoHeight}`;
                }
                window.dispatchEvent(new CustomEvent('player_ready'));
            }
        }
        videoEl.addEventListener('canplay', onCanPlay);
        return () => videoEl.removeEventListener('canplay', onCanPlay);
    }, []);

    // Once resume check completes: show resume prompt if there's a saved position
    useEffect(() => {
        if (!resumeReady) return;
        if (resumePosition && resumePosition > 0) {
            setShowResumePrompt(true);
        }
    }, [resumeReady]);

    // Handle resume choice
    const handleResume = useCallback(() => {
        setShowResumePrompt(false);
        const video = videoRef.current;
        if (!video) return;
        if (isSession && sessionSeekUrl) {
            handleSeek(resumePosition);
        } else {
            video.currentTime = resumePosition;
        }
        // Save resumed position immediately
        const dur = duration > 0 ? duration : (video.duration || 0);
        if (dur > 0) forceSendPosition(resumePosition, dur);
    }, [resumePosition, isSession, sessionSeekUrl, handleSeek, duration, forceSendPosition]);

    const handleStartOver = useCallback(() => {
        setShowResumePrompt(false);
        // Save position 0 immediately
        const video = videoRef.current;
        const dur = duration > 0 ? duration : (video?.duration || 0);
        if (dur > 0) forceSendPosition(0, dur);
    }, [duration, forceSendPosition]);

    // Chromecast integration
    useEffect(() => {
        if (!features.chromecast) return;
        let cancelled = false;
        function initCast() {
            if (cancelled) return;
            if (!window.cast || !window.chrome?.cast) return;
            const ctx = cast.framework.CastContext.getInstance();
            ctx.setOptions({
                receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
                autoJoinPolicy: chrome.cast.AutoJoinPolicy.PAGE_SCOPED,
                androidReceiverCompatible: true,
            });
            setCastAvailable(true);
        }
        loadCastSender().then((available) => { if (available) initCast(); });
        return () => { cancelled = true; setCastAvailable(false); };
    }, [features.chromecast, isVideo]);

    // Session cleanup removed — sessions have server-side TTL

    // Captions / embed modals are native <dialog>s that live OUTSIDE the
    // player container (see stream_video.html). They must be opened with
    // showModal(): fullscreen is requested on the player container, and
    // only the fullscreen element's subtree is rendered — a CSS-toggled
    // sibling would stay invisible until the user left fullscreen. A modal
    // dialog joins the top layer above the fullscreen element instead.
    const handleCaptionsClick = useCallback(() => {
        toggleDialog('subtitles');
        // Opening the picker is the one moment the viewer is guaranteed to
        // be looking at it, and plenty can have moved while it was closed:
        // an audio switch re-picking the subtitle, an upload, a translation
        // the viewer started. Recompute the counts, the dot and both
        // "Now:" lines against what is actually playing.
        const modal = document.getElementById('subtitles');
        if (modal && modal.open) {
            applyFlagSupport(modal);
            refresh(modal);
        }
    }, []);
    const handleEmbedClick = useCallback(() => toggleDialog('embed'), []);

    // In-player share click — same handler as the header button (see
    // assets/src/js/lib/share/share.js), tagged `location:'player'` so we
    // can A/B which placement converts better.
    //
    // Use the explicit `data-share-url` baked into the <video> by the
    // server (always points at the resource page on webtor.io). On the
    // resource page this equals window.location.href; on embed players
    // it sends the recipient to the real resource page, not the iframe
    // URL on the embedding site.
    const handleShareClick = useCallback(() => {
        shareResource({
            location: 'player',
            url: videoEl?.dataset?.shareUrl,
        });
    }, []);

    // Click on video to toggle play (video only).
    // Use ref for showResumePrompt to avoid re-registering native DOM listeners.
    const showResumePromptRef = useRef(false);
    showResumePromptRef.current = showResumePrompt;

    const handleVideoClick = useCallback((e) => {
        if (!isVideo || sessionSeekingRef.current || showResumePromptRef.current) return;
        if (e.target.closest('.wt-player-controls')) return;
        if (e.target.closest('.wt-resume-prompt')) return;
        state.togglePlay();
        resetHideTimer();
    }, [isVideo, state.togglePlay]);

    // Double-click for fullscreen
    const handleDoubleClick = useCallback((e) => {
        if (!isVideo) return;
        if (e.target.closest('.wt-player-controls')) return;
        state.toggleFullscreen();
    }, [isVideo, state.toggleFullscreen]);

    // Apply classes to the container element (managed outside Preact)
    useEffect(() => {
        const el = containerEl;
        if (!el) return;
        el.className = `wt-player ${isVideo ? 'wt-player--video' : 'wt-player--audio'}${fixedSize ? ' wt-player--fixed' : ''}`;

        const onMove = () => resetHideTimer();
        const onTouch = () => resetHideTimer();
        const onClick = (e) => handleVideoClick(e);
        const onDblClick = (e) => handleDoubleClick(e);
        el.addEventListener('mousemove', onMove);
        el.addEventListener('touchstart', onTouch);
        el.addEventListener('click', onClick);
        el.addEventListener('dblclick', onDblClick);

        return () => {
            el.removeEventListener('mousemove', onMove);
            el.removeEventListener('touchstart', onTouch);
            el.removeEventListener('click', onClick);
            el.removeEventListener('dblclick', onDblClick);
        };
    }, [containerEl, isVideo]);

    // Update dynamic classes
    useEffect(() => {
        const el = containerEl;
        if (!el) return;
        el.classList.toggle('wt-player--fullscreen', state.fullscreen);
        el.classList.toggle('wt-player--controls-visible', controlsVisible);
    }, [state.fullscreen, controlsVisible, containerEl]);

    return (
        <>
            {/* Resume prompt — ask user to continue or start over */}
            {showResumePrompt && isVideo && (
                <div class="wt-player-overlay wt-resume-prompt" style="background:rgba(0,0,0,0.75);z-index:50;display:flex;align-items:center;justify-content:center"
                     onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
                    <div style="display:flex;flex-direction:column;gap:10px;align-items:center">
                        <button type="button" onClick={(e) => { e.stopPropagation(); handleResume(); }}
                            class="wt-resume-btn wt-resume-btn--primary">
                            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" width="16" height="16"><path fill-rule="evenodd" d="M4.5 5.653c0-1.427 1.529-2.33 2.779-1.643l11.54 6.347c1.295.712 1.295 2.573 0 3.286L7.28 19.99c-1.25.687-2.779-.217-2.779-1.643V5.653Z" clip-rule="evenodd" /></svg>
                            {tf('player.continueFrom', formatTime(resumePosition))}
                        </button>
                        <button type="button" onClick={(e) => { e.stopPropagation(); handleStartOver(); }}
                            class="wt-resume-btn wt-resume-btn--ghost">
                            {t('player.startOver')}
                        </button>
                    </div>
                </div>
            )}

            {/* Loading spinner (only when playing + buffering, or seeking) */}
            {showControls && isVideo && (sessionSeeking || (state.playing && state.loading)) && (
                <div class="wt-player-overlay wt-player-overlay--loading">
                    <LoadingSpinner />
                </div>
            )}

            {/* Big play button — shown when paused, regardless of loading state */}
            {showControls && isVideo && !state.playing && !sessionSeeking && !showResumePrompt && (
                <div class="wt-player-overlay wt-player-overlay--play" onDblClick={(e) => e.stopPropagation()}>
                    <button type="button" class="wt-player-big-play" onClick={(e) => { e.stopPropagation(); state.togglePlay(); }} aria-label={t('player.play')}>
                        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-16 h-16">
                            <path fill-rule="evenodd" d="M4.5 5.653c0-1.427 1.529-2.33 2.779-1.643l11.54 6.347c1.295.712 1.295 2.573 0 3.286L7.28 19.99c-1.25.687-2.779-.217-2.779-1.643V5.653Z" clip-rule="evenodd" />
                        </svg>
                    </button>
                </div>
            )}

            {/* Controls */}
            {showControls && (
                <Controls
                    playing={state.playing}
                    currentTime={state.currentTime}
                    duration={state.duration}
                    volume={state.volume}
                    muted={state.muted}
                    fullscreen={state.fullscreen}
                    buffered={state.buffered}
                    seeking={sessionSeeking}
                    onTogglePlay={state.togglePlay}
                    onSeek={handleSeek}
                    onVolumeChange={state.setVolume}
                    onToggleMute={state.toggleMute}
                    onToggleFullscreen={state.toggleFullscreen}
                    onCaptionsClick={handleCaptionsClick}
                    onEmbedClick={handleEmbedClick}
                    isVideo={isVideo}
                    features={features}
                />
            )}

            {/* Top-right share affordance — visible immediately, fades with
                the controls. Title on the left, share icon on the right.
                The title comes from data-resource-title when provided by
                the template, else from document.title stripped of the
                " | Webtor.io" site-suffix that layouts/main.html appends. */}
            {/* Gated on features.share, NOT castAvailable: castAvailable only
                means the Cast SDK loaded (true in every desktop Chrome), and
                a paid embed that disabled share must not suddenly grow a
                Webtor gradient + torrent title over its video. Share-less
                players get the floating cast launcher below instead. */}
            {showControls && isVideo && features.share && (() => {
                const title = getResourceTitle(videoEl);
                // Overlay container has pointer-events:none in CSS so the
                // gradient stays click-through (clicks land on the video
                // for play-toggle). Only the action buttons need explicit
                // stopPropagation — their own pointer-events:auto means they
                // capture the click before it can reach video.
                return (
                    <div class="wt-player-top-overlay">
                        {title && <div class="wt-player-top-title" title={title}>{title}</div>}
                        <div class="wt-player-top-actions">
                            {castAvailable && (
                                <div class="wt-player-cast-button" onClick={(e) => e.stopPropagation()}>
                                    <google-cast-launcher></google-cast-launcher>
                                </div>
                            )}
                            {features.share && (
                                <button type="button" class="wt-player-btn wt-player-top-share" onClick={(e) => { e.stopPropagation(); handleShareClick(); }} aria-label={t('player.share')}>
                                    <ShareIcon />
                                </button>
                            )}
                        </div>
                    </div>
                );
            })()}
            {/* Controls-less players have no top overlay, and share-less
                embeds deliberately render none — keep the cast launcher
                reachable via the legacy floating position in both cases. */}
            {isVideo && castAvailable && (!showControls || !features.share) && (
                <div class="wt-player-cast-button wt-player-cast-button--floating" onClick={(e) => e.stopPropagation()}>
                    <google-cast-launcher></google-cast-launcher>
                </div>
            )}
        </>
    );
}

// getResourceTitle pulls the leaf label for the top overlay.
// Server computes the right title (file basename minus extension,
// falling back to the torrent name) in jobs/scripts/action.go
// resourceLeafTitle() and passes it via data-resource-title on the
// <video>. Empty string when the attribute is missing — the overlay
// renders without a label rather than mis-parsing document.title.
function getResourceTitle(videoEl) {
    return videoEl?.dataset?.resourceTitle || '';
}

function formatTime(seconds) {
    const s = Math.floor(seconds);
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`;
    return `${m}:${String(sec).padStart(2, '0')}`;
}

function parseFeatures(settings, isVideo, duration, isSession) {
    const adsOn = !!(window._domainSettings && window._domainSettings.ads === true);
    const defaults = {
        playpause: true,
        progress: true,
        duration: true,
        volume: true,
        advancedtracks: isVideo,
        fullscreen: isVideo,
        chromecast: isVideo,
        embed: isVideo,
        // In-player share button — same handler as the header button,
        // fires share-resource Umami with location:'player'. Treated as
        // a growth feature: locked on for ads-enabled (free) embed
        // domains, so the override loop below ignores settings.features.share
        // unless the domain has earned NoAds (paid tier). On the resource
        // page itself there's no _domainSettings, so adsOn is false and
        // the override is always honoured.
        share: true,
        availableprogress: duration > 0 && !isSession,
        logo: adsOn,
    };
    if (settings && settings.features) {
        for (const name in settings.features) {
            // Growth-feature lock: settings can only disable share when
            // the host domain isn't already paying us back via ads.
            if (name === 'share' && adsOn) continue;
            defaults[name] = settings.features[name];
        }
    }
    return defaults;
}

/**
 * Initialize player on a target container that contains a <video> or <audio> with class="player".
 * Called from action/stream.js.
 */
export async function initPlayer(target) {
    const videoEl = target.querySelector('.player');
    if (!videoEl) return;

    // Load player translations before rendering (sync t() reads from cached instance)
    await initI18n();

    let settings = {};
    if (videoEl.dataset.settings) {
        settings = JSON.parse(videoEl.dataset.settings);
    }

    // Check if controls should be shown (from HTML controls attribute)
    const showControls = videoEl.hasAttribute('controls');

    // Remove native controls — we render our own (or none)
    videoEl.removeAttribute('controls');

    // Transfer fixed dimensions from video to container
    const fixedWidth = videoEl.getAttribute('width');
    const fixedHeight = videoEl.getAttribute('height');
    if (fixedWidth) videoEl.removeAttribute('width');
    if (fixedHeight) videoEl.removeAttribute('height');

    // Wrap the video in a player container div
    const mountEl = document.createElement('div');
    mountEl.className = 'wt-player-mount';
    if (fixedWidth) mountEl.style.width = fixedWidth;
    if (fixedHeight) mountEl.style.height = fixedHeight;
    videoEl.parentNode.insertBefore(mountEl, videoEl);

    // Build the player container with video inside
    const playerContainer = document.createElement('div');
    mountEl.appendChild(playerContainer);
    playerContainer.appendChild(videoEl);

    // Wire track handlers on original modals (stay outside player, no overflow issues).
    // The modals are wired before Preact renders, so `trackHooks` is the
    // hand-off point: the component fills it in on mount.
    const trackHooks = {};
    wireTrackHandlers(target, trackHooks);

    // Wire embed copy button
    wireEmbedCopy(target);

    // Wire logo (ads download overlay) — only when ads enabled
    let showLogo = !!(window._domainSettings && window._domainSettings.ads === true);
    if (settings.features && settings.features.logo !== undefined) {
        showLogo = settings.features.logo;
    }
    if (showLogo) wireLogo(target, mountEl);

    // Render Preact controls into the player container (after video)
    render(
        <PlayerComponent videoEl={videoEl} settings={settings} containerEl={playerContainer} showControls={showControls} fixedSize={!!(fixedWidth || fixedHeight)} trackContainer={target} trackHooks={trackHooks} />,
        playerContainer
    );

    _currentPlayer = { mountEl, playerContainer, videoEl };
}

// Ensure a <track> with id=<trackID> exists inside <video>. The server
// pre-wraps user-subtitle URLs through /ext/ with the correct auth
// (subdomain/path/query baked in by torrent-http-proxy) and returns
// them in data-src. For subs uploaded after the initial render, the
// <video> has no matching <track> yet — create one on first click.
// ensureTrackElement creates the <track> for a side-loaded subtitle the
// first time it is selected. The server renders only the default track:
// browsers fetch every <track> element on page load regardless of mode,
// and 40+ OpenSubtitles tracks per page tripped the ingress per-IP rate
// limit and OpenSubtitles' 5 req/s (a burst of 40 requests in one second
// per viewer). Creating tracks lazily makes each selection one download.
function ensureTrackElement(video, trackID, wrappedSrc, label, srclang, kind) {
    for (const t of video.querySelectorAll('track')) {
        if (t.id === trackID) return true;
    }
    if (!wrappedSrc) return false;
    const track = document.createElement('track');
    track.id = trackID;
    track.kind = kind || 'subtitles';
    track.src = wrappedSrc;
    track.label = label || 'Subtitle';
    // HTML requires srclang on a subtitles track; 'und' when the upload
    // declares no language. An empty attribute is not a valid tag.
    track.srclang = srclang || 'und';
    video.appendChild(track);
    return true;
}

// findSubtitleItem locates a list item by data-id without CSS.escape:
// track ids come from the torrent (file paths, stream indexes) and are
// not guaranteed to be valid selector literals.
function findSubtitleItem(modal, id) {
    for (const el of modal.querySelectorAll('.subtitle')) {
        if (el.getAttribute('data-id') === id) return el;
    }
    return null;
}

// trackSubtitleSelect reports an activation the viewer asked for, whether
// they pressed a chip or flipped the switch: the same two events either
// way, so the ladder-level metric keeps counting every deliberate
// selection exactly once. "None" is not a track and reports nothing.
function trackSubtitleSelect(el) {
    if (!el || !window.umami) return;
    const id = el.getAttribute('data-id');
    if (!id || id === 'none') return;
    window.umami.track('subtitle-select', selectEventData(el));
    if (el.getAttribute('data-provider') === 'UserSubtitle') window.umami.track('user-subtitle-select');
}

// setSubtitlesOff performs the switch: decide what to activate, then
// activate it. Nothing else — the activation is what writes the state
// (markTrack: the attribute, the memory of what comes back, and the muted
// redraw), so this function never touches data-subtitles-off itself.
//
// The one exception is the dead end: when the rule finds nothing
// activatable there is no activation to carry the state, so the switch is
// put back where it was from here.
//
// Returns the element it activated, or null, so the caller can report the
// choice (telemetry, the manual-choice hook).
function setSubtitlesOff(container, modal, off) {
    if (!modal) return null;
    const audioEl = modal.querySelector('.audio[data-default="true"]');
    const decision = toggleDecision({
        on: !off,
        lastId: modal.getAttribute('data-last-subtitle') || '',
        suggestedId: suggestedSubtitleID(modal),
        tracks: readTracks(modal),
        audioLang: audioEl ? (audioEl.getAttribute('data-srclang') || '') : '',
        preferredLang: modal.getAttribute('data-preferred-lang') || '',
    });
    const item = decision.activateId ? findSubtitleItem(modal, decision.activateId) : null;
    if (!item) {
        // Nothing to turn on (no track in the viewer's language, or the only
        // one is locked). The switch must not claim otherwise: put it back
        // where it was instead of leaving it on over silence.
        applyOffState(modal, true);
        return null;
    }
    // No applyOffState here: activating the item is what moves the switch
    // (markTrack), so the state cannot be written twice and cannot disagree.
    activateSubtitle(container, item, { persist: decision.persist });
    return item;
}

// suggestedSubtitleID is the server's answer to "what would be playing if
// subtitles were on" (ListItem.Suggested, rendered as data-suggested) —
// the only thing a page opened with subtitles off has to restore from.
function suggestedSubtitleID(modal) {
    const el = modal && modal.querySelector('.subtitle[data-suggested="true"]');
    return el ? (el.getAttribute('data-id') || '') : '';
}

function findSubtitlesModal(container) {
    return (container && container.querySelector('#subtitles')) || document.getElementById('subtitles');
}

// itemData reads the fields the pure rules in subtitle-rules.js need off
// a picker list item.
function itemData(el) {
    return {
        id: el.getAttribute('data-id') || '',
        provider: el.getAttribute('data-provider') || '',
        locked: el.getAttribute('data-locked') === 'true',
    };
}

// activateSubtitle switches playback to the subtitle the given list item
// stands for. Shared by the click handler and by the auto-selection that runs
// after an upload, so both paths create the <track>, mark the list item
// through markTrack and end with the same textTracks state.
//
// persist says whether the choice is written back to the session
// (ud.SubtitleID, which renders as ListItem.Saved). Only what the viewer did
// on purpose counts: clicking an item, or uploading a file to watch with.
// The activations the player performs for the viewer — the audio-switch
// re-pick and the mount-time restore of a saved translation — pass
// persist:false, so `Saved` keeps meaning
// exactly "the viewer chose this" and a rule the player applied for them
// never comes back as a choice the next rule has to respect.
function activateSubtitle(container, target, { persist = true } = {}) {
    const provider = target.getAttribute('data-provider');
    const id = target.getAttribute('data-id');
    // Side-loaded tracks (OpenSubtitles, sidecar files, embed externals,
    // user uploads) exist as <track> elements only once selected — see
    // ensureTrackElement.
    if (provider !== 'MediaProbe' && id && id !== 'none') {
        const video = container.querySelector('video.player, audio.player');
        if (video) {
            // data-label only. A chip's text is decorated now — the origin
            // code ("OS"), the property tag ("forced"), the source suffix
            // ("· opensubtitles") — and that whole string used to land in
            // <track label>, which is what the native iOS track menu
            // shows. Every chip that reaches here is side-loaded and
            // carries data-label; the "None" carrier, the one without it,
            // is excluded by the id guard above. ensureTrackElement supplies
            // its own last-resort name.
            ensureTrackElement(
                video,
                id,
                target.getAttribute('data-src') || '',
                target.getAttribute('data-label') || '',
                target.getAttribute('data-srclang') || '',
                target.getAttribute('data-kind') || 'subtitles',
            );
        }
    }
    markTrack(container, target, 'subtitle', persist);
    const hls = window.hlsPlayer;
    const mpId = target.getAttribute('data-mp-id');

    // Side-loaded <track> elements (they carry an id) go to 'disabled', not
    // 'hidden': a hidden track is still fetched by the browser, so with 40+
    // OpenSubtitles tracks in the list every selection fired a burst of
    // downloads that tripped OpenSubtitles' 5 req/s limit and came back as
    // 404s (87% of track fetches arrived in bursts of 5+ per torrent).
    // Only the selected track loads. hls.js-managed tracks have no id and
    // keep the previous handling.
    if (hls && provider === 'MediaProbe') {
        hls.subtitleDisplay = true;
        hls.subtitleTrack = parseInt(mpId);
        for (const p of document.querySelectorAll('video.player')) {
            for (const t of p.textTracks) {
                if (t.id) t.mode = 'disabled';
            }
        }
    } else {
        if (hls) {
            hls.subtitleTrack = -1;
            hls.subtitleDisplay = false;
        }
        for (const p of document.querySelectorAll('video.player, audio.player')) {
            for (const t of p.textTracks) {
                if (id && id !== 'none' && t.id === id) t.mode = 'showing';
                else t.mode = t.id ? 'disabled' : 'hidden';
            }
        }
    }
}

function toggleDialog(id) {
    const dialog = document.getElementById(id);
    if (!dialog || typeof dialog.showModal !== 'function') return;
    if (dialog.open) dialog.close();
    else dialog.showModal();
}

// setUploadPanel opens or closes the "My subtitles" panel. The open state
// is written on #my-subtitles, the wrapper the async swap does NOT replace
// (the toggle and the panel inside it are), so a delete or an upload can
// put the panel back the way the viewer had it.
function setUploadPanel(modal, open) {
    const panel = modal.querySelector('#my-uploads-panel');
    if (!panel) return;
    panel.hidden = !open;
    const toggle = modal.querySelector('#my-uploads-toggle');
    if (toggle) toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    const wrap = modal.querySelector('#my-subtitles');
    if (wrap) wrap.setAttribute('data-upload-open', open ? 'true' : 'false');
}

// wireTrackHandlers binds the picker modals. It stays module-scope and
// ref-free: `hooks` is the object the mounted component fills with
// onSubtitleSelect/onAudioSelect (see the trackHooks effect), so the
// session-scoped state those need lives in the component, not here.
function wireTrackHandlers(container, hooks = {}) {
    // Delegate subtitle clicks on #subtitles so items swapped into
    // #my-subtitles via async still work without re-binding.
    const subtitlesModal = container.querySelector('#subtitles');
    if (subtitlesModal) {
        subtitlesModal.addEventListener('click', (e) => {
            const target = e.target.closest('.subtitle');
            if (!target || !subtitlesModal.contains(target)) return;
            // A locked item (the AI translation on a free account) has no
            // Src to activate — turning it on would leave subtitles
            // "selected" with nothing on screen. Reveal the upgrade card
            // and leave the current selection untouched.
            if (target.getAttribute('data-locked') === 'true') {
                const cta = subtitlesModal.querySelector('#translate-cta');
                if (cta) cta.hidden = false;
                if (window.umami) window.umami.track('subtitle-translate-lock-click', {
                    lang: target.getAttribute('data-srclang') || '',
                });
                return;
            }
            trackSubtitleSelect(target);
            // Picking a track while the switch is off means "on, with this
            // one": one act for the viewer, so one activation and one PUT.
            // Nothing to flip here — activating a track that is not "None"
            // is what turns the switch on (markTrack).
            activateSubtitle(container, target);
            if (hooks.onSubtitleSelect) hooks.onSubtitleSelect(target);
        });

        // The subtitles switch. It is a real checkbox (DaisyUI toggle), so
        // the browser owns the pressed state and this only reacts to it —
        // change, not click, so keyboard and label clicks arrive too.
        const toggleEl = subtitlesModal.querySelector('#subtitles-toggle');
        if (toggleEl) {
            toggleEl.addEventListener('change', () => {
                const item = setSubtitlesOff(container, subtitlesModal, !toggleEl.checked);
                // Flipping the switch is as manual as pressing a chip, and
                // the component has to hear about it: onSubtitleSelect is
                // what sets manualSubtitleRef, without which the next audio
                // switch re-runs the ladder and turns subtitles back on over
                // an explicit off. It also stops a translation poll the
                // viewer just switched away from, and starts one when the
                // track that came back is an AI item.
                if (item) {
                    trackSubtitleSelect(item);
                    if (hooks.onSubtitleSelect) hooks.onSubtitleSelect(item);
                }
            });
        }

        // Language row, "+N" and the uploads disclosure. One more delegate
        // on the same modal, for the same reason as the one above: the
        // uploads toggle is part of the markup the async swap replaces.
        subtitlesModal.addEventListener('click', (e) => {
            // A language chip never changes what is playing — it only
            // filters the track row. The one chip that does change playback
            // is "Off", and that is a .subtitle handled above.
            const lang = e.target.closest('.lang[data-lang]');
            if (lang && subtitlesModal.contains(lang)) {
                applyLangFilter(subtitlesModal, lang.getAttribute('data-lang'));
                return;
            }
            // "+N" is a toggle, not a one-way reveal: expanded it reads "×"
            // and is the only way back to the short row.
            const more = e.target.closest('#subtitle-lang-more');
            if (more && subtitlesModal.contains(more)) {
                toggleLangOverflow(subtitlesModal);
                return;
            }
            const upload = e.target.closest('#my-uploads-toggle');
            if (upload && subtitlesModal.contains(upload)) {
                const panel = subtitlesModal.querySelector('#my-uploads-panel');
                if (!panel) return;
                setUploadPanel(subtitlesModal, panel.hidden);
                return;
            }
            // The panel's own "×". Same state change as pressing the chip
            // again, through the same function: two controls, one way the
            // panel can be open or closed.
            const uploadClose = e.target.closest('#my-uploads-close');
            if (uploadClose && subtitlesModal.contains(uploadClose)) setUploadPanel(subtitlesModal, false);
        });
    }

    // Audio click handlers (no async swap — direct binding is enough).
    // e.target.closest, not e.target: a chip's click lands on the flag
    // <span> or the check <svg> as often as on the button itself, and
    // markTrack would then mark a <span> and read data-mp-id as null.
    for (const audio of container.querySelectorAll('.audio')) {
        audio.addEventListener('click', (e) => {
            const target = e.target.closest('.audio');
            if (!target) return;
            markTrack(container, target, 'audio');
            if (window.hlsPlayer && target.getAttribute('data-provider') === 'MediaProbe') {
                window.hlsPlayer.audioTrack = parseInt(target.getAttribute('data-mp-id'));
            }
            if (hooks.onAudioSelect) hooks.onAudioSelect(target);
        });
    }

    // The <track default> in <video> drives playback correctly on reload,
    // but the uploads' chips are re-rendered by the partial and never go
    // through the click path, so they lose the active marker. Re-derive it
    // from the live textTracks and re-run on every async swap.
    const setMark = (el, on) => {
        setChipActive(el, on);
        if (on) el.setAttribute('data-default', 'true');
        else el.removeAttribute('data-default');
    };
    // isUpload is what "my uploads" means to this function now that the MY
    // chips live in the radiogroup with everything else (adoptUploadChips
    // moves the ones an async reload delivers). Containment in
    // #my-subtitles used to be the test and no longer can be: that wrapper
    // holds the disclosure and the panel, and — between the swap and the
    // adoption — a fresh chip for one instant. The provider is the honest
    // question anyway: these are the chips the uploads partial re-renders
    // and therefore the ones whose markers the client owns.
    const isUpload = (el) => el.getAttribute('data-provider') === 'UserSubtitle';
    const syncUploadMarks = () => {
        const video = container.querySelector('video.player');
        if (!video) return;
        const scope = subtitlesModal || container;
        const mine = Array.from(scope.querySelectorAll('.subtitle')).filter(isUpload);
        if (!mine.length) return;
        let activeID = null;
        for (const t of video.textTracks) {
            if (t.mode === 'showing' && t.id) { activeID = t.id; break; }
        }
        if (!activeID) {
            // The <track default> attribute answers "what is playing" only
            // while nothing else claims it. An embedded (MediaProbe) track
            // is driven by hls.js and has no <track> element at all, so
            // "no showing textTrack" does not mean "nothing is playing" —
            // and a stale default left on a side-loaded track would then
            // hand the marker to an upload as well, leaving two chips with
            // data-default and two check marks.
            const marked = scope.querySelectorAll('.subtitle[data-default="true"]');
            for (const el of marked) {
                if (!isUpload(el)) return;
            }
            const dt = video.querySelector('track[default]');
            if (dt && dt.id) activeID = dt.id;
        }
        if (!activeID) return;
        const active = mine.find((el) => el.getAttribute('data-id') === activeID) || null;
        for (const el of mine) setMark(el, el === active);
        if (!active) return;
        // Exactly one marker per group, as after a click: markTrack's
        // clearing loop without the PUT — nothing was chosen here, the DOM
        // is only catching up with what is already playing.
        for (const el of scope.querySelectorAll('.subtitle')) {
            if (el === active || isUpload(el)) continue;
            setMark(el, false);
        }
    };
    syncUploadMarks();
    // loadAsyncView dispatches an 'async' CustomEvent after swapping a
    // target's innerHTML; re-sync when #my-subtitles content is replaced.
    const mySubsContainer = container.querySelector('#my-subtitles');
    if (mySubsContainer) {
        window.addEventListener('async', (e) => {
            if (!e.detail || e.detail.target !== mySubsContainer) return;
            // The panel and its toggle are part of the swapped markup; the
            // wrapper is not, so it is what remembers whether the viewer had
            // the upload form open. After a delete the viewer is still
            // looking at the panel: it must come back open, or removing two
            // files in a row means re-opening it between them.
            if (mySubsContainer.getAttribute('data-upload-open') === 'true') {
                setUploadPanel(subtitlesModal || container, true);
            }
            // The response renders the MY chips into this wrapper, which
            // sits outside the radiogroup — move them in before anything
            // reads the row. Everything below (the autoselect marker, the
            // chip ids a delete is measured against, refresh's counts and
            // filter) looks at #subtitle-tracks, and a chip still sitting
            // in the wrapper is invisible to all of it.
            adoptUploadChips(subtitlesModal || container);
            // A freshly uploaded subtitle comes back marked by the server.
            // Switch to it right away: the viewer uploaded a file to watch
            // with, and making them hunt for it in the row afterwards reads
            // as "subtitles don't work".
            const fresh = (subtitlesModal || container).querySelector('.subtitle[data-autoselect="true"]');
            if (fresh) activateSubtitle(container, fresh);
            else {
                // A delete takes the chip away but not the <track>: that
                // one lives in <video>, which this swap never touches, so
                // the deleted file's subtitles stayed on screen with
                // nothing marked and "Now:" blank. Drop the orphans and,
                // when the viewer deleted what was playing, land on "Off" —
                // the honest reading of "I removed that file". persist:
                // false, because they chose a deletion, not a track.
                const video = container.querySelector('video.player, audio.player');
                const chipIDs = [];
                for (const el of (subtitlesModal || container).querySelectorAll('.subtitle')) {
                    const cid = el.getAttribute('data-id');
                    if (cid) chipIDs.push(cid);
                }
                const wasShowing = dropDeletedTracks(video, chipIDs);
                const off = wasShowing && subtitlesModal ? findSubtitleItem(subtitlesModal, 'none') : null;
                if (off) activateSubtitle(container, off, { persist: false });
                else syncUploadMarks();
            }
            // The set of chips itself changed, so this is the full pass and
            // not refreshMarks: an upload can bring a language the server
            // never rendered a chip for, and a delete can empty the expanded
            // one. `current` keeps the viewer where they were whenever that
            // language survived the swap.
            if (subtitlesModal) refresh(subtitlesModal, { current: expandedLang(subtitlesModal) });
        });
    }

    // First pass: hide the tracks of every collapsed language, drop the
    // flags where the platform draws regional indicators as letter pairs,
    // and fill both "Now:" lines. (refresh ends with applyFlagSupport of
    // its own; the explicit call is what makes the mount-time guarantee
    // independent of refresh's internals.)
    if (subtitlesModal) {
        applyFlagSupport(subtitlesModal);
        refresh(subtitlesModal);
    }
}

// markTrack moves the active marker of one group (audio or subtitle) onto
// `el`. The look is a cyan fill plus the check icon that is already in
// every chip's markup — toggled, never rebuilt: a chip carries its origin
// badge, its property tag and, on the AI item, the .tr-progress span of a
// running translation, and innerHTML would throw all three away mid-poll.
//
// `persist` is what separates a choice from a rule the player applied for
// the viewer: the audio-switch re-pick and the mount-time restore of a
// saved translation pass false and never PUT, so `Saved` keeps meaning "the viewer
// chose this".
function markTrack(container, el, type, persist = true) {
    if (el.getAttribute('data-default') === 'true') return;
    const s = container.querySelector('#subtitles');
    // Read before anything moves: the switch's memory is the track being
    // replaced, and a line further down there is no way to tell which chip
    // that was.
    const prev = s && type === 'subtitle' ? s.querySelector('.subtitle[data-default="true"]') : null;
    const prevID = prev ? (prev.getAttribute('data-id') || '') : '';

    setChipActive(el, true);
    el.setAttribute('data-default', 'true');

    if (!s) return;
    const es = s.querySelectorAll(`.${type}`);
    for (const ee of es) {
        if (ee === el) continue;
        setChipActive(ee, false);
        ee.removeAttribute('data-default');
    }
    if (type === 'subtitle') {
        // The one writer of "are subtitles off": every activation moves the
        // switch with it, including the ones the player performs for the
        // viewer (a deleted upload landing on None, the audio-switch
        // re-pick, an upload selected right after it was added). Kept here
        // rather than at each call site because a second writer is exactly
        // how the switch and the row drifted apart. After the clearing loop,
        // because applyOffState puts the muted mark back.
        const next = offStateAfterActivate(prevID, el.getAttribute('data-id') || '', s.getAttribute('data-last-subtitle') || '');
        if (next.lastId) s.setAttribute('data-last-subtitle', next.lastId);
        else s.removeAttribute('data-last-subtitle');
        applyOffState(s, next.off);
    }
    // The dot on the language chip and the "Now:" line, not the language
    // filter: the viewer's expanded language is their own choice and must
    // not jump under them because playback moved. After applyOffState: with
    // subtitles off the row follows the muted choice, which the line above
    // has just named.
    refreshMarks(s);

    if (!persist) return;
    fetch(`/stream-video/${type}`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-TOKEN': window._CSRF,
        },
        body: JSON.stringify({
            id: el.getAttribute('data-id'),
            resourceID: s.getAttribute('data-resource-id'),
            itemID: s.getAttribute('data-item-id'),
        }),
    });
}

function wireEmbedCopy(container) {
    // type="button" — deliberately not a method="dialog" submit, so copying
    // leaves the embed dialog open.
    const copy = container.querySelector('#embed .copy');
    if (!copy) return;
    copy.addEventListener('click', () => {
        const textarea = container.querySelector('#embed textarea');
        if (!textarea) return;
        navigator.clipboard.writeText(textarea.value).then(() => {
            if (window.toast) window.toast.success(t('player.copied'));
        });
    });
}

function wireLogo(container, playerContainer) {
    const logo = container.querySelector('#logo');
    if (!logo) return;
    // Replace template classes with player-managed class for CSS transitions
    logo.className = 'wt-player-logo';
    playerContainer.appendChild(logo);
}

/**
 * Destroy current player instance.
 */
export function destroyPlayer() {
    if (!_currentPlayer) return;
    const { mountEl, playerContainer, videoEl } = _currentPlayer;

    // Destroy HLS
    if (window.hlsPlayer) {
        window.hlsPlayer.stopLoad();
        window.hlsPlayer.destroy();
        window.hlsPlayer = null;
    }

    // Unmount Preact
    render(null, playerContainer);

    // Remove video
    videoEl.remove();

    // Remove mount point
    mountEl.remove();

    _currentPlayer = null;
}
