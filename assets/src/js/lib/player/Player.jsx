import { render } from 'preact';
import { useRef, useState, useEffect, useCallback } from 'preact/hooks';
import { usePlayerState } from './hooks/usePlayerState';
import { useHls } from './hooks/useHls';
import { useWatchHistory } from './hooks/useWatchHistory';
import { useSubtitleTranslation } from './hooks/useSubtitleTranslation';
import { createSessionSeeker } from './session-seek';
import { createGraceHold } from './grace-hold';
import { Hls } from './hls-manager';
import { applyCueOffset, setTrackDelay, normalizeDelay, SUBTITLE_DELAY_STEP } from './cue-offset';
import { stepRate, rateLabel, loadSubtitleDelay, saveSubtitleDelay, loadPrefs, savePrefs } from './player-prefs';
import { createTapSeek } from './tap-seek';
import { localSeekTarget, producedEnd } from './local-seek';
import { bindMediaSession } from './media-session';
import { readNext, advancePlan, atEnd, resumeAt, readStreak, writeStreak, countdown } from './next-item';
import { createNextItemGo, canMoveOn, takeFallbackNote } from './next-item-go';
import { HAS_POPOVER, useDockedPopover } from './useAnchoredPopover';
import { creditsStart, cuesOfLoadedTracks, parseVttTimings, timingSourceURL, creditsFromElement } from './credits';
import { track, settled } from './player-telemetry';
import { reportCodecSupport, whenPlaying, sourceCodec, playbackPath } from './codec-support';
import { applySubtitleSelection, isEmbedded, readSelection, selectionHolds } from './subtitle-apply.js';
import { readTracks, resolveSubtitleLevel } from './subtitle-telemetry.js';
import { markAutoResume, takeAutoResume } from './preferred-lang.js';
import {
    refresh,
    applyFlagSupport,
    readChips,
} from './track-picker.js';
import { offerTiming, offerVisible, pickOffer, suppressUpsell, upsellSuppressed } from './subtitle-offer.js';
import {
    findSubtitleItem,
    findSubtitlesModal,
    offerStorage,
    readUpsellCard,
    releaseTrackDialog,
    toggleDialog,
    wireTrackHandlers,
} from './track-dialog.js';
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
function PlayerComponent({ videoEl, settings, containerEl, showControls, fixedSize, trackContainer, trackHooks, awaitStart = false }) {
    const containerRef = useRef(containerEl);
    const videoRef = useRef(videoEl);
    const [seekOffset, setSeekOffset] = useState(0);
    const [sessionSeeking, setSessionSeeking] = useState(false);
    const sessionSeekingRef = useRef(false);
    // A resume-prompt answer given while a session seek was in flight, and
    // handleSeek behind a ref so the seeking callback (declared before it)
    // can carry that answer out.
    const pendingResumeRef = useRef(null);
    const handleSeekRef = useRef(() => {});
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
    // The grace popup holds the film until the viewer answers it
    // (grace-hold.js), and the answer while it is up -- the popup's own
    // buttons or Play (togglePlay) -- goes through graceAnswerRef.
    const graceHoldRef = useRef(null);
    if (!graceHoldRef.current) graceHoldRef.current = createGraceHold(videoEl);
    const graceAnswerRef = useRef(null);
    useEffect(() => () => graceHoldRef.current.dispose(), []);
    // streamStarted is tracked via a ref, not state — the value is only
    // read to gate the one-shot Umami event below, never to drive UI, so
    // a re-render on transition would be pure overhead.
    const streamStartFiredRef = useRef(false);
    const poster = videoEl.getAttribute('poster');
    const resourceID = videoEl.dataset.resourceId;
    const path = videoEl.dataset.path;

    // The viewer's subtitle delay (cue-offset.js setTrackDelay): seconds,
    // positive = later, remembered per file. Element-backed tracks only --
    // subtitles muxed into the film are timed by the film and hls.js owns
    // their cues.
    const delayKey = resourceID && path ? `${resourceID}:${path}` : '';
    const [subDelay, setSubDelayState] = useState(() => normalizeDelay(loadSubtitleDelay(delayKey)));
    const subDelayRef = useRef(subDelay);
    subDelayRef.current = subDelay;
    const [toast, setToast] = useState(null); // { text, n }
    const toastTimerRef = useRef(null);
    const showToast = useCallback((text) => {
        setToast({ text, n: Date.now() });
        if (toastTimerRef.current) clearTimeout(toastTimerRef.current);
        toastTimerRef.current = setTimeout(() => setToast(null), 1200);
    }, []);
    useEffect(() => () => { if (toastTimerRef.current) clearTimeout(toastTimerRef.current); }, []);
    const formatDelay = (d) => `${d > 0 ? '+' : d < 0 ? '\u2212' : ''}${Math.abs(d).toFixed(2)}`;
    // Telemetry (player-telemetry.js): one event per decision.
    const delayEventRef = useRef(null);
    if (!delayEventRef.current) delayEventRef.current = settled((v) => track('subtitle-delay', v), 2000);
    const tapEventRef = useRef(null);
    if (!tapEventRef.current) tapEventRef.current = settled((v) => track('player-tap-seek', v), 900);
    useEffect(() => () => { delayEventRef.current.flush(); tapEventRef.current.flush(); }, []);

    const changeSubDelay = useCallback((next, { announce = true } = {}) => {
        const d = normalizeDelay(next);
        subDelayRef.current = d;
        setSubDelayState(d);
        saveSubtitleDelay(delayKey, d);
        if (announce) showToast(tf('player.subtitleDelayToast', formatDelay(d)));
        // `announce` is true for the keys and false for the dialog's buttons.
        delayEventRef.current.push({ delay: d, source: announce ? 'key' : 'dialog' });
    }, [delayKey, showToast]);

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
    // Not only for sessions since 2026-09-20: the viewer's subtitle delay
    // (below) shifts the same cues, and a direct stream has offset 0.
    useEffect(() => {
        const video = videoRef.current;
        if (!video) return;
        const applyAll = () => {
            for (const el of video.querySelectorAll('track')) {
                if (!el.track) continue;
                setTrackDelay(el.track, subDelayRef.current);
                applyCueOffset(el.track, seekOffset);
            }
        };
        applyAll();
        // The ref, not the closure's `seekOffset`: a seek sets the ref at
        // once (onSeekOffsetChange) while this effect re-subscribes a render
        // later, and a <track> that loads in between was shifted by the OLD
        // run's offset -- minutes off, on a resume from 0.
        const onTrackLoad = (e) => {
            if (e.target.tagName === 'TRACK' && e.target.track) {
                setTrackDelay(e.target.track, subDelayRef.current);
                applyCueOffset(e.target.track, seekOffsetRef.current);
            }
        };
        video.addEventListener('load', onTrackLoad, true);
        return () => video.removeEventListener('load', onTrackLoad, true);
    }, [seekOffset, isSession, subDelay]);

    // Movie time is video.currentTime + the session offset (see
    // applyCueOffset in cue-offset.js for the same arithmetic on cues).
    // Mirrored into a ref because the poll callbacks are built once and
    // would otherwise read the offset the run started with.
    const seekOffsetRef = useRef(0);
    seekOffsetRef.current = seekOffset;
    // And onto the element, for the code outside this component that needs
    // movie time (activateSubtitle's first request for a translation, the
    // transfer status's grace window: lib/playerActivity.js inGrace).
    if (videoEl) videoEl.dataset.runOffset = String(seekOffset);
    // The keyboard handler is declared before the toggle wrapper it must
    // call (see togglePlay below), so it goes through this.
    const togglePlayRef = useRef(() => {});
    // --- AI subtitle translation ------------------------------------
    // The run, its progress poll, the catch-up hold and everything that
    // decides them live in hooks/useSubtitleTranslation.js. Called HERE, where
    // that code used to stand, so its effects keep their place in the
    // component's effect order. `late` carries what is only declared further
    // down (the hls.js instance, the on-screen offer): the hook reads it when
    // it acts, never while rendering.
    const late = useRef({ hlsRef: null, withdrawOffer: () => {}, redecideOffer: () => {} });
    const translation = useSubtitleTranslation({
        videoRef, videoEl, trackContainer, trackHooks, seekOffset, seekOffsetRef, sessionSeekingRef, late,
    });
    const {
        catchUp, preHolding, manualSubtitleRef,
        kickTranslationPoll, onSeekSettled, onDirectSeek, cancelPreHold,
        handleWait, handleKeepWatching, handleDismissCatchUp,
    } = translation;

    // Sync ref with state for use in closures that don't re-bind
    const setSessionSeekingWithRef = useCallback((val) => {
        sessionSeekingRef.current = val;
        setSessionSeeking(val);
    }, []);

    // Player state hook
    const state = usePlayerState(videoRef, containerRef, { duration, seekOffset, seeking: sessionSeeking });

    // HLS hook
    const hlsRef = useHls(videoRef, sourceUrl);

    // Re-assert the picker's answer on hls.js's own transitions.
    //
    // hls.js selects subtitle tracks by itself: every textTracks `change`
    // event makes its SubtitleTrackController re-scan the element's tracks
    // and adopt one (see subtitle-apply.js). Our own mode writes are change
    // events, so a selection applied once can be taken away a tick later —
    // measured on stage as an embedded track loading cues and drawing them
    // over the AI subtitle the viewer had chosen, while `subtitleTrack` read
    // -1 and `subtitleDisplay` false.
    //
    // Only a side-loaded or "None" selection is guarded, and only when what
    // hls.js is actually doing disagrees with it. An embedded selection
    // needs no guard because it is self-healing: it leaves hls.js's own
    // track 'showing', which is exactly what onTextTracksChanged finds and
    // re-adopts — the same index, so nothing changes. The one transition
    // that does lose it is loadSource, and the seeker re-applies there.
    //
    // Two things make this terminate. The disagreement check: an apply
    // writes modes, the modes wake hls.js, hls.js calls back, and the
    // second pass finds the state already correct. And, for the pass that
    // does not get that far, applySubtitleSelection's own re-entrancy
    // guard — hls.js triggers SUBTITLE_TRACK_SWITCH synchronously from
    // inside the write, so this listener runs nested in the apply that is
    // still only half-finished.
    useEffect(() => {
        const hls = hlsRef.current || window.hlsPlayer;
        if (!hls || typeof hls.on !== 'function' || !Hls || !Hls.Events) return;
        const reassert = () => {
            const selection = readSelection(trackContainer || document);
            if (!selection || isEmbedded(selection)) return;
            const video = videoRef.current;
            if (selectionHolds(video, hls, selection)) return;
            applySubtitleSelection(video, hls, selection);
        };
        const events = [Hls.Events.SUBTITLE_TRACK_SWITCH, Hls.Events.SUBTITLE_TRACKS_UPDATED];
        for (const e of events) hls.on(e, reassert);
        return () => {
            if (typeof hls.off !== 'function') return;
            for (const e of events) hls.off(e, reassert);
        };
        // sourceUrl, because useHls destroys and re-creates the instance
        // when it changes, and the listeners have to move with it. That
        // effect is declared above this one, so the new instance is already
        // in the ref by the time this re-runs.
    }, [trackContainer, sourceUrl]);

    // Resume prompt state — must be declared before useWatchHistory which reads it.
    const [showResumePrompt, setShowResumePrompt] = useState(false);
    // The viewer has answered the resume prompt (either way). Until then a
    // film with a saved position is held: see the hold effect below.
    const [resumeAnswered, setResumeAnswered] = useState(false);
    // The same fact for the hold's 'play' listener: the answer calls play()
    // at once, and that event lands before the effect cleanup that would
    // take the listener off -- state alone would pause the film the viewer
    // just started.
    const resumeAnsweredRef = useRef(false);
    const autoResumeRef = useRef(false);

    // Watch history hook (position tracking + resume).
    // `paused` prevents overwriting saved position while resume prompt is open.
    // Where the credits begin (credits.js), filled in further down. Declared
    // here because the watch history reports it with every position.
    const creditsAtRef = useRef(null);
    const { resumePosition, resumeReady, forceSendPosition } = useWatchHistory(videoRef, {
        resourceID, path, creditsAtRef,
        currentTime: state.currentTime,
        duration: state.duration,
        playing: state.playing,
        paused: showResumePrompt,
    });

    // A settings change that re-renders the player (the preferred language,
    // preferred-lang.js) calls this first: the position is saved now, not at
    // the next periodic write, and the player that comes back is told to
    // continue from it without asking.
    useEffect(() => {
        if (!trackHooks) return undefined;
        trackHooks.beforeRestart = () => {
            const video = videoRef.current;
            if (!video) return;
            const at = (video.currentTime || 0) + seekOffsetRef.current;
            const dur = duration > 0 ? duration : (video.duration || 0);
            if (!(at > 0) || !(dur > 0)) return;
            forceSendPosition(at, dur);
            markAutoResume(resourceID, path);
        };
        return () => { trackHooks.beforeRestart = null; };
    }, [trackHooks, duration, forceSendPosition, resourceID, path]);

    // The seek has settled (the new run is playing): ask the service where
    // the translation is now, rather than on the next 3 s tick, so a hold
    // for the new position's subtitles starts within one request.
    const onSessionSeekingChange = useCallback((val) => {
        setSessionSeekingWithRef(val);
        if (!val && pendingResumeRef.current !== null) {
            // The resume prompt was answered during this seek (handleResume).
            // Its seek replaces this one's settle: no hold window for a run
            // that is being left.
            const at = pendingResumeRef.current;
            pendingResumeRef.current = null;
            handleSeekRef.current(at, { play: true });
            return;
        }
        if (val) return;
        onSeekSettled();
    }, [setSessionSeekingWithRef, onSeekSettled]);


    // ---- the on-screen translation offer (subtitle-offer.js) ----------
    //
    // Decided once, on the first play: the pill is about "this film has no
    // subtitles in your language", and that is a property of the page the
    // server rendered, not something to re-ask on every resume. Reading it
    // at first play rather than at mount keeps it off a page that never
    // plays (autoplay blocked, resume prompt up) and gives the mount-time
    // restore of a saved translation time to make the chip the default --
    // which withdraws the offer by pickOffer's own rule.
    const [offer, setOffer] = useState(null);
    const [offerLingering, setOfferLingering] = useState(false);
    const [offerCard, setOfferCard] = useState(false);
    const offerArmedRef = useRef(false);
    // Bumped when the dialog is re-rendered for another language, so the
    // offer below is decided again.
    const [dialogVersion, setDialogVersion] = useState(0);
    const offerTimerRef = useRef(null);
    // What the translation hook needs from further down than its call site.
    late.current.hlsRef = hlsRef;
    late.current.withdrawOffer = () => { setOffer(null); setOfferCard(false); };
    late.current.redecideOffer = () => { offerArmedRef.current = false; setDialogVersion((v) => v + 1); };

    const trackOffer = useCallback((name, o, extra) => {
        if (window.umami) window.umami.track(name, { lang: o.lang, kind: o.kind, ...(extra || {}) });
    }, []);

    useEffect(() => {
        if (!isVideo || !state.playing || offerArmedRef.current) return;
        // Not under the resume prompt: <video autoplay> can get a moment of
        // playback in before the saved position arrives, and a pill decided
        // then would spend its ten seconds behind the prompt's overlay.
        if (!resumeReady || (resumePosition > 0 && !resumeAnswered)) return;
        offerArmedRef.current = true;
        const modal = findSubtitlesModal(trackContainer);
        if (!modal) return;
        const next = pickOffer(readChips(modal));
        if (!next || !next.label) return;
        if (next.kind === 'upsell' && upsellSuppressed(offerStorage(), Date.now())) return;
        setOffer(next);
        setOfferLingering(true);
        offerTimerRef.current = setTimeout(() => setOfferLingering(false), offerTiming.lingerMs);
        trackOffer('subtitle-offer-shown', next);
    }, [state.playing, isVideo, trackContainer, trackOffer, resumeReady, resumePosition, resumeAnswered, dialogVersion]);

    useEffect(() => () => { if (offerTimerRef.current) clearTimeout(offerTimerRef.current); }, []);

    const handleOfferClick = useCallback(() => {
        if (!offer) return;
        trackOffer('subtitle-offer-click', offer);
        if (offer.kind === 'upsell') {
            setOfferCard((open) => !open);
            return;
        }
        // Taking the offer IS pressing the chip: the same delegated handler,
        // so the same PUT, the same marks, the same poll. The chip is asked
        // again whether it is still on offer -- an audio switch or a manual
        // pick may have spent it since the pill was decided.
        const modal = findSubtitlesModal(trackContainer);
        const el = modal ? findSubtitleItem(modal, offer.id) : null;
        setOffer(null);
        if (el && el.getAttribute('data-offered') === 'true') el.click();
    }, [offer, trackContainer, trackOffer]);

    // how: 'close' (the ×), 'later' (Not now), 'never' (Don't offer).
    // Only the upsell is remembered; see subtitle-offer.js.
    const handleOfferDismiss = useCallback((how) => {
        if (!offer) return;
        if (offer.kind === 'upsell') suppressUpsell(offerStorage(), Date.now(), { never: how === 'never' });
        trackOffer('subtitle-offer-dismiss', offer, { how });
        setOffer(null);
        setOfferCard(false);
    }, [offer, trackOffer]);

    // How seeks in a session split between "inside the run" and "new FFmpeg".
    // Counted and sent once the seeking stops: a held arrow key is thirty
    // seeks a second, and an event for each would be a flood saying one thing.
    const seekCountsRef = useRef({ local: 0, session: 0 });
    const seekEventRef = useRef(null);
    if (!seekEventRef.current) {
        seekEventRef.current = settled(() => {
            const c = seekCountsRef.current;
            seekCountsRef.current = { local: 0, session: 0 };
            track('player-seek', c);
        }, 5000);
    }
    const countSeek = (kind) => {
        seekCountsRef.current[kind] += 1;
        seekEventRef.current.push(true);
    };
    useEffect(() => () => seekEventRef.current.flush(), []);

    // Seek handler (session or direct)
    const handleSeek = useCallback((time, { play = false } = {}) => {
        if (sessionSeekingRef.current) return;
        if (isSession && sessionSeekUrl) {
            // Inside the run that is playing? Then it is a plain seek
            // (local-seek.js): no POST, no new FFmpeg, no frozen frame. The
            // offset does not change, so cues and the translation's timeline
            // stay where they are; the rest is what a direct seek does.
            const video = videoRef.current;
            const local = video ? localSeekTarget(time, seekOffsetRef.current, producedEnd(video, hlsRef.current)) : null;
            if (local !== null) {
                state.setCurrentTime(time);
                video.currentTime = local;
                if (play && video.paused) video.play().catch(() => {});
                onDirectSeek();
                countSeek('local');
                return;
            }
            countSeek('session');
            // Immediately show target position on timeline
            state.setCurrentTime(time);
            // Lazily create session seeker (works with HLS.js or native HLS)
            if (!sessionSeekerRef.current) {
                sessionSeekerRef.current = createSessionSeeker({
                    hls: hlsRef.current, // null for native HLS (iOS)
                    videoEl,
                    sessionSeekUrl,
                    sourceUrl,
                    onSeekOffsetChange: (offset) => {
                        setSeekOffset(offset);
                        // Now, not on the next render: the kick below polls
                        // with it, and the answer is judged against it --
                        // and the element's copy is read by code outside
                        // this component (the transfer status's grace window,
                        // lib/playerActivity.js inGrace) on the source
                        // restart's own events, before any render.
                        seekOffsetRef.current = offset;
                        videoEl.dataset.runOffset = String(offset);
                        kickTranslationPoll();
                    },
                    onSeekingChange: onSessionSeekingChange,
                    trackContainer,
                    // The grace popup up: the new run is loaded, not played,
                    // until the viewer answers it (grace-hold.js).
                    holdPlayback: () => graceHoldRef.current.holds(),
                });
            }
            if (sessionSeekerRef.current) {
                sessionSeekerRef.current.seek(time, { play });
            }
        } else {
            const video = videoRef.current;
            if (video) {
                const maxTime = video.duration && isFinite(video.duration) ? video.duration : time;
                video.currentTime = Math.min(time, maxTime);
                // The same seek treatment a session gets, minus the
                // transcoder round-trip: the poll is kicked with the new
                // position and the hold window opens now — a direct seek
                // has no `playing` settle to wait for. Unlike a session
                // seek, nothing rations this path (no POST, no
                // sessionSeeking guard), so the kick itself is: a held
                // arrow key repeats the seek ~30 times a second, and each
                // kick is an immediate HEAD.
                onDirectSeek();
            }
        }
    }, [isSession, sessionSeekUrl, sourceUrl, kickTranslationPoll, onSessionSeekingChange, onDirectSeek]);
    handleSeekRef.current = handleSeek;

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
            // The remembered settings this stream started with: `player-speed`
            // counts changes, and a viewer who set 1.5x a week ago makes none.
            rate: videoRef.current ? videoRef.current.playbackRate : 1,
            subtitleDelay: subDelayRef.current,
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
                // "We never got an answer", not "there was none": the
                // OpenSubtitles lookup was still warming up when this
                // render was built, and the render is cached for ten
                // minutes, so without this a level of 'none' would count
                // as a file with no subtitles.
                notReady: modal.getAttribute('data-subtitles-not-ready') === 'true',
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
    // The film stops while the popup is up (owner, 2026-09-26) and goes on
    // with the answer -- only if it was the popup that stopped it
    // (grace-hold.js). A session seek past the window puts the popup up as
    // it starts (the timeline shows the target at once): the seek has paused
    // the film itself, and its new run is loaded, not played, until the
    // answer (session-seek.js holdPlayback).
    useEffect(() => {
        if (!graceDurationSec || graceShownRef.current) return;
        if (state.currentTime < graceDurationSec) return;
        const el = document.querySelector('#grace-cta');
        if (!el) return;
        graceShownRef.current = true;
        // Up from here on (closed later or not): the transfer status has
        // counted it as on its way since the element crossed the window
        // (lib/playerActivity.js graceOfferDue) and now reads the popup
        // itself.
        videoEl.dataset.graceCtaShown = '';
        if (document.fullscreenElement) {
            document.exitFullscreen().catch(() => {});
        }
        const hold = graceHoldRef.current;
        hold.start();
        el.classList.remove('hidden');
        // `paused`: the popup stopped a playing film. A seek's run held
        // behind it is known only at the answer (its `paused` below).
        if (window.umami) window.umami.track('grace-soft-cta-shown', { paused: hold.held() });
        // `via`: the popup's own button, or Play (the key, the big button,
        // a click on the picture, the headset) -- the answer "continue".
        const hide = (action, via = 'button') => {
            if (graceAnswerRef.current !== hide) return;
            graceAnswerRef.current = null;
            // The viewer's answer, on the element: they have just been told
            // of the cap, so the transfer status sells the way out again
            // only once they hit it -- this player's first real stall
            // (lib/playerActivity.js offerAnswered), not the moment the
            // popup closes. On the element, so the next file, a reload or
            // another grace window starts without it. Before the film goes
            // on: its first events are read against it.
            videoEl.dataset.graceCtaAnswered = action;
            el.classList.add('hidden');
            const paused = hold.held();
            hold.release({ play: via === 'play' });
            if (window.umami) window.umami.track('grace-soft-cta-click', { action, via, paused });
        };
        graceAnswerRef.current = hide;
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
                    togglePlayRef.current();
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
                // The keys every player uses for speed (Shift+, / Shift+.);
                // e.key is the produced character, so layouts agree.
                // Next episode / track. Shift+N as well, the habit from YouTube.
                case 'n':
                case 'N':
                    if (!next) break;
                    e.preventDefault();
                    goNext('key');
                    break;
                // Subtitle delay, VLC's keys: g earlier, h later.
                case 'g':
                case 'h':
                    e.preventDefault();
                    changeSubDelay(subDelayRef.current + (e.key === 'h' ? SUBTITLE_DELAY_STEP : -SUBTITLE_DELAY_STEP));
                    resetHideTimer();
                    break;
                case '<':
                case '>':
                    if (!features.speed) break;
                    e.preventDefault();
                    {
                        const next = stepRate(state.rate, e.key === '>' ? +1 : -1);
                        state.setRate(next);
                        showToast(rateLabel(next));
                        if (next !== state.rate) track('player-speed', { rate: next, source: 'key' });
                    }
                    resetHideTimer();
                    break;
            }
        }
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, [state.currentTime, state.duration, state.volume, state.rate, state.playing, sessionSeeking]);

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

    // Which codecs this viewer's browser decodes (codec-support.js,
    // docs/player.md): the question for an HEVC/AV1 passthrough in the
    // transcoder. Gated on the first frame, so the count is of viewers; the
    // report itself waits for an idle moment and goes once a week per
    // browser. What the stream is travels with it: the source's codec from
    // the job's media probe, whether a transcoder session serves it, and
    // which path plays it.
    useEffect(() => {
        if (!isVideo) return;
        return whenPlaying(videoEl, () => reportCodecSupport({
            src: sourceCodec(videoEl.dataset.videoCodecs),
            tc: isSession,
            pl: playbackPath(hlsRef.current, sourceUrl),
            emb: !!window._embedSettings,
        }));
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
        // Video only, like the prompt's markup and the hold below: an
        // <audio> with a saved position has no prompt to answer, and a
        // prompt that is "open" and invisible kept position saving paused.
        if (isVideo && resumePosition && resumePosition > 0) {
            // A player that came back from a settings change (the preferred
            // language, preferred-lang.js) continues without asking: the
            // viewer never left.
            autoResumeRef.current = takeAutoResume(resourceID, path);
            setShowResumePrompt(true);
        }
    }, [resumeReady]);

    // A film with a saved position does not start by itself (owner,
    // 2026-09-18): <video autoplay> would play it from the beginning behind
    // the prompt, sound included, while the viewer is still choosing. Held
    // from the moment the saved position is known until the prompt is
    // answered; 'play' is listened for because autoplay may fire after the
    // prompt is already up (canplay arrives when the stream is ready, not
    // when the page is). Either answer starts playback. Not for <audio>:
    // the prompt renders for video only, so nothing could ever answer it
    // and the track would be re-paused on every play, for good.
    useEffect(() => {
        if (!isVideo || !resumeReady || !(resumePosition > 0) || resumeAnswered) return;
        const video = videoRef.current;
        if (!video) return;
        const hold = () => { if (!resumeAnsweredRef.current && !video.paused) video.pause(); };
        hold();
        video.addEventListener('play', hold);
        return () => video.removeEventListener('play', hold);
    }, [isVideo, resumeReady, resumePosition, resumeAnswered]);

    const playAfterPrompt = useCallback(() => {
        const r = videoRef.current ? videoRef.current.play() : null;
        if (r && r.catch) r.catch(() => {});
    }, []);

    // Handle resume choice
    const handleResume = useCallback(() => {
        setShowResumePrompt(false);
        resumeAnsweredRef.current = true;
        setResumeAnswered(true);
        const video = videoRef.current;
        if (!video) return;
        if (isSession && sessionSeekUrl) {
            // Not play() and then seek: the old run would be heard from the
            // film's beginning for as long as the new one takes to start.
            // The seeker starts the new run itself. A seek already in flight
            // (an arrow key pressed just before the prompt came up) makes
            // handleSeek a no-op, and the prompt is closed by now: the
            // answer is kept and carried out when that seek lets go.
            if (sessionSeekingRef.current) pendingResumeRef.current = resumePosition;
            else handleSeek(resumePosition, { play: true });
        } else {
            video.currentTime = resumePosition;
            playAfterPrompt();
        }
        // Save resumed position immediately
        const dur = duration > 0 ? duration : (video.duration || 0);
        if (dur > 0) forceSendPosition(resumePosition, dur);
    }, [resumePosition, isSession, sessionSeekUrl, handleSeek, duration, forceSendPosition, playAfterPrompt]);

    // The prompt is answered for the viewer when the note says so. Through
    // the prompt's own state rather than around it: handleResume is the one
    // place that knows how to resume a session and a direct source.
    const handleStartOver = useCallback(() => {
        setShowResumePrompt(false);
        resumeAnsweredRef.current = true;
        setResumeAnswered(true);
        // Whatever autoplay got through before the hold is not "the
        // beginning". Session playback has no direct seek, and its head
        // start is a fraction of a second.
        if (!isSession && videoRef.current && videoRef.current.currentTime > 0) videoRef.current.currentTime = 0;
        playAfterPrompt();
        // Save position 0 immediately
        const video = videoRef.current;
        const dur = duration > 0 ? duration : (video?.duration || 0);
        if (dur > 0) forceSendPosition(0, dur);
    }, [duration, forceSendPosition, isSession, playAfterPrompt]);

    // ...and this is where the note answers it. A file the viewer had all but
    // finished starts from the top (next-item.js resumeAt): an automatic move
    // to the next episode that resumed at 98% would end at once and chain on.
    useEffect(() => {
        if (!showResumePrompt || !autoResumeRef.current) return;
        autoResumeRef.current = false;
        const dur = duration > 0 ? duration : ((videoRef.current && videoRef.current.duration) || 0);
        if (resumeAt(resumePosition, dur) > 0) handleResume();
        else handleStartOver();
    }, [showResumePrompt, handleResume, handleStartOver]);

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
        paintSubDelay();
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

    // One toggle for every control (click, space, the big button, the
    // bar). During the silent hold the element is paused by the player
    // while the viewer is looking at a seek that has not finished: their
    // toggle means "pause", not "play". It ends the hold without playing
    // and withdraws the hold question for this seek.
    const togglePlay = useCallback(() => {
        // Leaving for the next file: the film was paused on purpose
        // (goNext), and a stray space or tap must not start it again.
        if (nextLoadingRef.current) return;
        // The grace popup is up and holds the film: Play is the answer
        // "continue", and plays -- whoever paused it. Not a dead key: a
        // Play that does nothing reads as a broken player, and pressing it
        // says what "Continue at N Mbps" says.
        if (graceHoldRef.current.isActive() && graceAnswerRef.current) {
            graceAnswerRef.current('continue', 'play');
            return;
        }
        if (cancelPreHold()) return;
        state.togglePlay();
    }, [cancelPreHold, state.togglePlay]);
    togglePlayRef.current = togglePlay;

    // Click on video to toggle play (video only).
    // Use ref for showResumePrompt to avoid re-registering native DOM listeners.
    const showResumePromptRef = useRef(false);
    showResumePromptRef.current = showResumePrompt;

    // Double-tap to seek (tap-seek.js). Touch only: with a mouse a double
    // click is fullscreen and there are arrow keys. A click counts as a tap
    // when a touchstart came just before it -- the one signal every mobile
    // browser gives (iOS Safari's click is a plain MouseEvent).
    const TAP_SEEK_STEP = 10;
    const lastTouchAtRef = useRef(0);
    const fromTouch = () => Date.now() - lastTouchAtRef.current < 800;
    const seekPosRef = useRef({ currentTime: 0, duration: 0 });
    seekPosRef.current = { currentTime: state.currentTime, duration: state.duration };
    const [tapFx, setTapFx] = useState(null); // { dir, total, n }
    const tapFxTimerRef = useRef(null);
    const tapSeekRef = useRef(null);
    if (!tapSeekRef.current) {
        tapSeekRef.current = createTapSeek({
            onSingle: () => togglePlayRef.current(),
            onSeek: (dir, streak) => {
                const { currentTime, duration } = seekPosRef.current;
                const to = Math.max(0, Math.min(duration || Infinity, currentTime + dir * TAP_SEEK_STEP));
                handleSeekRef.current(to);
                setTapFx({ dir, total: streak * TAP_SEEK_STEP, n: Date.now() });
                tapEventRef.current.push({ dir: dir > 0 ? 'forward' : 'back', seconds: streak * TAP_SEEK_STEP });
                if (tapFxTimerRef.current) clearTimeout(tapFxTimerRef.current);
                tapFxTimerRef.current = setTimeout(() => setTapFx(null), 650);
            },
        });
    }
    useEffect(() => () => {
        if (tapSeekRef.current) tapSeekRef.current.cancel();
        if (tapFxTimerRef.current) clearTimeout(tapFxTimerRef.current);
    }, []);

    // The subtitle-delay row in the #subtitles dialog (stream_video.html):
    // server markup, painted and driven from here. Delegated on the document
    // because the dialog is swapped whole on a preferred-language change, and
    // repainted after ANY click inside it -- a track pick or the on/off switch
    // changes whether there is anything to shift.
    const paintSubDelay = useCallback(() => {
        const modal = document.getElementById('subtitles');
        const row = modal && modal.querySelector('#subtitle-delay');
        if (!row) return;
        const d = subDelayRef.current;
        const value = row.querySelector('#subtitle-delay-value');
        if (value) value.textContent = tf('player.subtitleDelayValue', formatDelay(d));
        const reset = row.querySelector('[data-sub-delay="reset"]');
        // Always in the row, disabled at zero (see the template). `hidden` and
        // `invisible` are lifted for renders cached with the earlier markup.
        if (reset) {
            reset.hidden = false;
            reset.classList.remove('invisible');
            reset.removeAttribute('tabindex');
        }
        // The switch is the one live statement of on/off (markTrack moves it).
        const toggle = modal.querySelector('#subtitles-toggle');
        const off = toggle ? !toggle.checked : false;
        const embedded = isEmbedded(readSelection(modal));
        for (const b of row.querySelectorAll('button[data-sub-delay]')) {
            b.disabled = off || embedded || (b.dataset.subDelay === 'reset' && d === 0);
        }
        row.classList.toggle('opacity-50', off || embedded);
        row.title = embedded && !off ? (row.dataset.embeddedHint || '') : '';
    }, []);
    useEffect(() => { paintSubDelay(); }, [subDelay, paintSubDelay]);
    useEffect(() => {
        const onClick = (e) => {
            const modal = e.target.closest && e.target.closest('#subtitles');
            if (!modal) return;
            const btn = e.target.closest('[data-sub-delay]');
            if (btn && !btn.disabled) {
                const op = btn.dataset.subDelay;
                const cur = subDelayRef.current;
                changeSubDelay(op === 'reset' ? 0 : cur + (op === 'later' ? SUBTITLE_DELAY_STEP : -SUBTITLE_DELAY_STEP), { announce: false });
                return;
            }
            // After the dialog's own handlers have settled the selection.
            setTimeout(paintSubDelay, 0);
        };
        document.addEventListener('click', onClick);
        return () => document.removeEventListener('click', onClick);
    }, [changeSubDelay, paintSubDelay]);

    // A player mounted by a move to the next file (next-item-go.js) is about
    // to play by itself. Until it does, it is LOADING, not paused: the big
    // Play button of a paused film under a spinner was two answers at once
    // (owner, 2026-09-20). Ends with the first frame, with a question only
    // the viewer can answer (the resume prompt), or after ten seconds -- if
    // autoplay was refused, Play is exactly what the viewer needs.
    const [awaitingStart, setAwaitingStart] = useState(awaitStart);
    useEffect(() => {
        if (!awaitingStart) return undefined;
        const video = videoRef.current;
        const done = () => setAwaitingStart(false);
        const timer = setTimeout(done, 10000);
        if (video) video.addEventListener('playing', done, { once: true });
        return () => {
            clearTimeout(timer);
            if (video) video.removeEventListener('playing', done);
        };
    }, [awaitingStart]);
    useEffect(() => { if (showResumePrompt) setAwaitingStart(false); }, [showResumePrompt]);

    // Next episode / next track (next-item.js decides, next-item-go.js moves).
    // `next` is the server's answer on the player element; without it none of
    // this exists -- a film, the last file, an embed.
    // ...or a page the move cannot happen on (canMoveOn).
    const next = useRef(canMoveOn(document) ? readNext(videoEl) : null).current;
    // The page before this one gave up on a quiet move and reloaded: say why,
    // here, where the console survived -- and count it.
    useEffect(() => {
        let store = null;
        try { store = window.sessionStorage; } catch (e) { return; }
        const note = takeFallbackNote(store);
        if (!note) return;
        console.warn('next item: the previous page fell back to a reload --', note.reason, note);
        track('next-item-fallback', { reason: String(note.reason).slice(0, 120) });
    }, []);
    const [autoplayNext, setAutoplayNext] = useState(() => loadPrefs().autoplayNext);
    const [nextCard, setNextCard] = useState(null); // null | 'soon' | 'offer' | 'ask'
    const nextCancelledRef = useRef(false);
    const [nextLoading, setNextLoading] = useState(false);
    // The latest line of the next file's start log. Kept from the silent
    // prewarm too, so a viewer who presses Next midway sees where it is.
    const [nextProgress, setNextProgress] = useState('');
    const nextLoadingRef = useRef(false);
    nextLoadingRef.current = nextLoading;
    const nextGoRef = useRef(null);
    const earlyGoneRef = useRef(false); // the credits countdown fires once
    const cardShownAtRef = useRef(null); // film time the card came up at (countdown)
    if (next && !nextGoRef.current) {
        nextGoRef.current = createNextItemGo({
            next, resourceID, root: trackContainer,
            getStage: currentStage, getAspectRatio: currentAspectRatio, initPlayer, destroyPlayer,
            onEvent: (name, data) => {
                if (name === 'go') track('next-item-go', { kind: next.kind, ...data });
                if (name === 'prepared') track('next-item-prepared', { kind: next.kind, ...data });
                if (name === 'loading') setNextLoading(!!data.on);
                if (name === 'progress') setNextProgress(data.text || '');
            },
        });
    }
    const safeSession = () => { try { return window.sessionStorage; } catch (e) { return null; } };
    const goNext = useCallback((how) => {
        if (!nextGoRef.current) return;
        // An automatic move extends the streak; anything the viewer did
        // themselves ends it (see the listener below).
        if (how === 'auto') writeStreak(safeSession(), readStreak(safeSession()) + 1);
        else writeStreak(safeSession(), 0);
        // The card stays (or comes up) while the next file loads: it is what
        // names the thing the spinner is for.
        setNextCard((c) => c || 'offer');
        // The film the viewer is leaving stops here (owner, 2026-09-20). A
        // next file that was not prewarmed takes its minute to start, and the
        // old one kept playing under the spinner meanwhile: the viewer had
        // said "next" and was shown more of "this" -- and its position kept
        // moving, so coming back to it later resumed past what they saw.
        // The pause also saves that position (useWatchHistory).
        const video = videoRef.current;
        if (video && !video.paused) video.pause();
        // The grace popup's hold goes too: its answer, given while the next
        // file loads, must not start this one again.
        graceHoldRef.current.dispose();
        nextGoRef.current.go(how);
    }, []);
    // Any sign of a viewer ends the "is anyone there" streak.
    useEffect(() => {
        if (!next) return undefined;
        const alive = () => writeStreak(safeSession(), 0);
        document.addEventListener('pointerdown', alive, true);
        document.addEventListener('keydown', alive, true);
        return () => {
            document.removeEventListener('pointerdown', alive, true);
            document.removeEventListener('keydown', alive, true);
        };
    }, []);
    // Where the credits begin, from subtitle timings (credits.js). Looked for
    // once per file, past the middle -- by then the tracks that are going to
    // load have loaded, and a viewer who leaves early costs nothing. Loaded
    // cues first; otherwise ONE request for a whole-file track, timings only.
    const creditsTriedRef = useRef(false);
    useEffect(() => {
        // For whom: a viewer with a next episode to be offered, or one whose
        // "watched" marks the server keeps (signed in). Anyone else has no
        // use for the answer, and the lookup can cost a request.
        if (!isVideo || creditsTriedRef.current || !(next ? next.kind !== 'track' : !!window._userId)) return;
        // The container's chapters first: the file's own answer, no subtitles
        // needed, and known from the first second rather than from 60%.
        if (state.duration > 0) {
            const fromChapters = creditsFromElement(videoEl, state.duration);
            if (fromChapters !== null) {
                creditsTriedRef.current = true;
                creditsAtRef.current = fromChapters;
                track('next-item-credits', { source: 'chapters', found: true, lead_s: Math.round(state.duration - fromChapters), next: !!next });
                return;
            }
        }
        if (!(state.duration > 0) || state.currentTime < state.duration * 0.6) return;
        creditsTriedRef.current = true;
        const duration = state.duration;
        const settle = (cues, source) => {
            const at = creditsStart(cues, duration);
            creditsAtRef.current = at;
            track('next-item-credits', { source, found: at !== null, lead_s: at !== null ? Math.round(duration - at) : 0, next: !!next });
        };
        // Loaded cues are free, so they are asked first -- but they may be a
        // track that cannot answer (an AI translation still being produced
        // has no last line yet): then a whole-file track is fetched after all.
        const loaded = cuesOfLoadedTracks(videoRef.current);
        if (loaded.length && creditsStart(loaded, duration) !== null) { settle(loaded, 'loaded'); return; }
        const src = timingSourceURL(document.getElementById('subtitles'));
        if (!src) {
            if (loaded.length) settle(loaded, 'loaded');
            else track('next-item-credits', { source: 'none', found: false, lead_s: 0, next: !!next });
            return;
        }
        fetch(src).then((r) => (r.ok ? r.text() : '')).then((text) => settle(parseVttTimings(text), 'fetched')).catch(() => {});
    }, [state.currentTime, state.duration]);

    // Prewarm and the "coming up" card follow the clock.
    useEffect(() => {
        if (!next || !nextGoRef.current) return;
        const plan = advancePlan({
            currentTime: state.currentTime, duration: state.duration, playing: state.playing,
            prewarmed: nextGoRef.current.isPrepared(), kind: next.kind, creditsAt: creditsAtRef.current,
        });
        // The prewarm is not decided here: see the `timeupdate` effect below.
        if (plan.card && !nextCancelledRef.current && nextCard === null) {
            cardShownAtRef.current = state.currentTime;
            setNextCard('soon');
            track('next-item-shown', { kind: next.kind, autoplay: autoplayNext });
        }
        if (!plan.card && nextCard === 'soon') { setNextCard(null); cardShownAtRef.current = null; } // sought back
        // The countdown ran out inside the credits: the same verdict `ended`
        // gets, a little earlier. Only while actually playing -- a paused
        // film does not leave by itself.
        if (plan.card && nextCard === 'soon' && state.playing && !nextCancelledRef.current && !earlyGoneRef.current) {
            const cd = countdown({ currentTime: state.currentTime, duration: state.duration, creditsAt: creditsAtRef.current, shownAt: cardShownAtRef.current });
            if (cd.early && cd.left === 0) {
                earlyGoneRef.current = true;
                const verdict = atEnd({ autoplay: autoplayNext, autoStreak: readStreak(safeSession()), cancelled: false, kind: next.kind });
                if (verdict === 'go') goNext('auto');
                else if (verdict === 'ask') { setNextCard('ask'); track('next-item-still-watching', { kind: next.kind }); }
            }
        }
    }, [state.currentTime, state.duration, state.playing]);
    // The prewarm follows the element's own `timeupdate`, for every kind of
    // file. The effect above follows state.currentTime, which is fed by
    // requestAnimationFrame -- and a background tab, where music lives and
    // where a film may well be left to play out, runs no animation frames at
    // all: the prewarm never got its turn there. `timeupdate` keeps firing
    // (about once a second). The card stays with the effect above: it is
    // something to look at, and a hidden tab has nobody looking.
    useEffect(() => {
        const video = videoRef.current;
        if (!next || !video || !nextGoRef.current) return undefined;
        const onTime = () => {
            const dur = seekPosRef.current.duration || video.duration || 0;
            const plan = advancePlan({
                currentTime: (video.currentTime || 0) + seekOffsetRef.current, duration: dur,
                playing: !video.paused, prewarmed: nextGoRef.current.isPrepared(),
                kind: next.kind, creditsAt: creditsAtRef.current,
            });
            if (plan.prewarm) nextGoRef.current.prepare();
        };
        video.addEventListener('timeupdate', onTime);
        return () => video.removeEventListener('timeupdate', onTime);
    }, []);

    // The end of the file.
    useEffect(() => {
        const video = videoRef.current;
        if (!next || !video) return undefined;
        const onEnded = () => {
            const verdict = atEnd({ autoplay: autoplayNext, autoStreak: readStreak(safeSession()), cancelled: nextCancelledRef.current, kind: next.kind });
            if (verdict === 'go') goNext('auto');
            else if (verdict === 'offer' || verdict === 'ask') {
                setNextCard(verdict);
                if (verdict === 'ask') track('next-item-still-watching', { kind: next.kind });
            }
        };
        video.addEventListener('ended', onEnded);
        return () => video.removeEventListener('ended', onEnded);
    }, [autoplayNext, goNext]);
    // The card lives in the top layer, docked to the player's corner: inside
    // the frame a narrow player cut its top off (useDockedPopover).
    const nextCardRef = useRef(null);
    const nextCardVisible = !!(next && next.kind !== 'track' && nextCard);
    useDockedPopover(nextCardVisible, containerEl, nextCardRef, `${nextLoading}|${nextProgress ? 1 : 0}|${nextCard}`);

    const cancelNext = useCallback(() => {
        nextCancelledRef.current = true;
        setNextCard(null);
        track('next-item-cancel', { kind: next ? next.kind : '' });
    }, []);
    const toggleAutoplayNext = useCallback(() => {
        setAutoplayNext((v) => {
            savePrefs({ autoplayNext: !v });
            track('next-item-autoplay', { on: !v });
            return !v;
        });
    }, []);

    // Media Session (media-session.js): lock screen, headset, media keys.
    // Bound once; position is reported in film time on the events that change
    // the timeline, and seeks go through handleSeek like every other seek.
    const mediaSessionRef = useRef(null);
    useEffect(() => {
        const video = videoRef.current;
        if (!video || typeof navigator === 'undefined') return undefined;
        const seen = new Set();
        const msUsed = (action) => { if (!seen.has(action)) { seen.add(action); track('player-media-session', { action }); } };
        const ms = bindMediaSession({
            session: navigator.mediaSession,
            Metadata: typeof window.MediaMetadata === 'function' ? window.MediaMetadata : null,
            title: getResourceTitle(videoEl),
            artwork: video.poster || '',
            // Counted once per action per player: a headset's play/pause can
            // fire dozens of times in a film and says nothing new after the first.
            onPlay: () => { msUsed('play'); if (video.paused) togglePlayRef.current(); },
            onPause: () => { msUsed('pause'); if (!video.paused) togglePlayRef.current(); },
            onSeekTo: (t) => { msUsed('seek'); handleSeekRef.current(t); },
            getPosition: () => ({ ...seekPosRef.current, rate: video.playbackRate }),
        });
        mediaSessionRef.current = ms;
        const evs = ['play', 'pause', 'seeked', 'ratechange', 'loadedmetadata'];
        const onEv = () => ms.update();
        evs.forEach((n) => video.addEventListener(n, onEv));
        return () => {
            evs.forEach((n) => video.removeEventListener(n, onEv));
            ms.destroy();
            mediaSessionRef.current = null;
        };
    }, []);

    const handleVideoClick = useCallback((e) => {
        if (!isVideo || sessionSeekingRef.current || showResumePromptRef.current) return;
        if (e.target.closest('.wt-player-controls')) return;
        if (e.target.closest('.wt-resume-prompt')) return;
        if (e.target.closest('.wt-catchup')) return;
        if (e.target.closest('.wt-offer-card')) return;
        resetHideTimer();
        if (fromTouch() && containerEl) {
            const r = containerEl.getBoundingClientRect();
            tapSeekRef.current.tap(e.clientX - r.left, r.width, performance.now());
            return;
        }
        togglePlayRef.current();
    }, [isVideo, containerEl]);

    // Double-click for fullscreen. Not for taps: Android fires dblclick on a
    // double tap, and that gesture now means "seek".
    const handleDoubleClick = useCallback((e) => {
        if (!isVideo || fromTouch()) return;
        if (e.target.closest('.wt-player-controls')) return;
        state.toggleFullscreen();
    }, [isVideo, state.toggleFullscreen]);

    // Apply classes to the container element (managed outside Preact)
    useEffect(() => {
        const el = containerEl;
        if (!el) return;
        el.className = `wt-player ${isVideo ? 'wt-player--video' : 'wt-player--audio'}${fixedSize ? ' wt-player--fixed' : ''}`;

        const onMove = () => resetHideTimer();
        const onTouch = () => { lastTouchAtRef.current = Date.now(); resetHideTimer(); };
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

            {/* The AI translation is behind the playhead — offer to wait
                for it. A top-centre pill: the subtitles it is about live
                at the bottom of the picture and the controls under them. */}
            {/* The subtitles are back: said once, for a few seconds, so the
                pill that explained their absence does not just vanish. */}
            {catchUp && catchUp.caughtUp && isVideo && (
                <div class="wt-catchup wt-catchup--done" role="status"
                     onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="20 6 9 17 4 12"/></svg>
                    <span class="wt-catchup-text">{t('player.subtitleCaughtUp')}</span>
                </div>
            )}
            {catchUp && !catchUp.caughtUp && isVideo && (
                <div class="wt-catchup" role="status"
                     onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
                    <LoadingSpinner />
                    <span class="wt-catchup-text">
                        {t(catchUp.waiting ? 'player.subtitleCatchUpWaitingShort' : 'player.subtitleCatchUpShort')}
                    </span>
                    <button type="button" class="wt-catchup-btn"
                        onClick={catchUp.waiting ? handleKeepWatching : handleWait}>
                        {t(catchUp.waiting ? 'player.subtitleCatchUpResume' : 'player.subtitleCatchUpWait')}
                    </button>
                    <button type="button" class="wt-catchup-close"
                        aria-label={t('player.subtitleCatchUpDismiss')} onClick={handleDismissCatchUp}>×</button>
                </div>
            )}

            {/* No subtitles in the viewer's language: the picker's offer,
                brought to the picture. Same slot as the catch-up pill, which
                wins it (offerVisible). */}
            {isVideo && offerVisible({ offer, lingering: offerLingering, controlsVisible, cardOpen: offerCard, catchUp }) && (
                <div class="wt-catchup wt-offer" data-kind={offer.kind}
                     onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
                    <button type="button" class="wt-offer-main" onClick={handleOfferClick}
                        aria-expanded={offer.kind === 'upsell' ? String(offerCard) : undefined}>
                        <span class="wt-offer-mark" aria-hidden="true">✦ AI</span>
                        <span class="wt-catchup-text">{offer.label}</span>
                        {offer.kind === 'upsell' && (
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
                        )}
                    </button>
                    <button type="button" class="wt-catchup-close"
                        aria-label={t('player.subtitleCatchUpDismiss')} onClick={() => handleOfferDismiss('close')}>
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18"/></svg>
                    </button>
                </div>
            )}
            {isVideo && offer && offer.kind === 'upsell' && offerCard && !catchUp && (() => {
                const card = readUpsellCard(findSubtitlesModal(trackContainer));
                return (
                    <div class="wt-offer-card" role="dialog" aria-label={offer.label}
                         onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
                        <p class="wt-offer-card-text">{card.text}</p>
                        {card.href && (
                            <a class="wt-offer-card-cta" href={card.href} target="_blank" rel="noopener"
                               data-umami-event="donate-subtitle-translate" data-umami-event-tier={card.tier}
                               data-umami-event-source="player">{card.cta}</a>
                        )}
                        <div class="wt-offer-card-row">
                            <button type="button" class="wt-offer-card-link" onClick={() => handleOfferDismiss('later')}>{t('player.subtitleOfferLater')}</button>
                            <button type="button" class="wt-offer-card-link" onClick={() => handleOfferDismiss('never')}>{t('player.subtitleOfferNever')}</button>
                        </div>
                    </div>
                );
            })()}

            {/* Loading spinner (only when playing + buffering, or seeking) */}
            {showControls && isVideo && (sessionSeeking || preHolding || nextLoading || (awaitingStart && !state.playing) || (state.playing && state.loading)) && (
                <div class="wt-player-overlay wt-player-overlay--loading">
                    <LoadingSpinner />
                </div>
            )}

            {/* Big play button — shown when paused, regardless of loading state */}
            {showControls && isVideo && !state.playing && !sessionSeeking && !preHolding && !showResumePrompt && !awaitingStart && !nextLoading && (
                <div class="wt-player-overlay wt-player-overlay--play" onDblClick={(e) => e.stopPropagation()}>
                    <button type="button" class="wt-player-big-play" onClick={(e) => { e.stopPropagation(); togglePlay(); }} aria-label={t('player.play')}>
                        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-16 h-16">
                            <path fill-rule="evenodd" d="M4.5 5.653c0-1.427 1.529-2.33 2.779-1.643l11.54 6.347c1.295.712 1.295 2.573 0 3.286L7.28 19.99c-1.25.687-2.779-.217-2.779-1.643V5.653Z" clip-rule="evenodd" />
                        </svg>
                    </button>
                </div>
            )}

            {/* What comes after this file. 'soon': the last seconds, the next
                one starts by itself at the end unless cancelled. 'offer':
                autoplay is off, the file has ended. 'ask': several files went
                by with no sign of a viewer. */}
            {nextCardVisible && (
                <div ref={nextCardRef} popover={HAS_POPOVER ? 'manual' : undefined} class="wt-next-card" onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
                    <div class="wt-next-card-kicker">
                        {nextLoading ? t('player.nextLoading') : (nextCard === 'ask' ? t('player.stillWatching') : (nextCard === 'soon' && autoplayNext ? tf('player.nextIn', countdown({ currentTime: state.currentTime, duration: state.duration, creditsAt: creditsAtRef.current, shownAt: cardShownAtRef.current }).left) : t('player.nextUp')))}
                    </div>
                    <div class="wt-next-card-label" title={next.label}>{next.label}</div>
                    {/* What the wait is made of: the running step of the next
                        file's start, as the ordinary job log would say it. */}
                    {nextLoading && nextProgress && (
                        <div class="wt-next-card-progress" aria-live="off" title={nextProgress}>{nextProgress}</div>
                    )}
                    <div class="wt-next-card-actions">
                        <button type="button" class="wt-next-card-go" onClick={() => goNext('card')} disabled={nextLoading}>
                            {nextCard === 'ask' ? t('player.continueWatching') : t('player.playNow')}
                        </button>
                        {nextCard === 'soon' && (
                            <button type="button" class="wt-next-card-link" onClick={cancelNext}>{t('player.nextCancel')}</button>
                        )}
                    </div>
                    <label class="wt-next-card-auto">
                        <span>{t('player.autoplayNext')}</span>
                        <input type="checkbox" role="switch" class="toggle toggle-soft toggle-sm" checked={autoplayNext} onChange={toggleAutoplayNext} />
                    </label>
                </div>
            )}

            {/* What a key just changed (speed, subtitle delay): the keys
                have no other face. */}
            {toast && (
                <div key={toast.n} class="wt-player-toast" role="status">{toast.text}</div>
            )}

            {/* Double-tap seek feedback: which way, and how far the streak
                has gone. Keyed so each tap restarts the animation. */}
            {tapFx && (
                <div key={tapFx.n} class={`wt-tap-seek wt-tap-seek--${tapFx.dir > 0 ? 'right' : 'left'}`} aria-hidden="true">
                    {tapFx.dir > 0 ? '+' : '\u2212'}{tapFx.total}
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
                    rate={state.rate}
                    fullscreen={state.fullscreen}
                    buffered={state.buffered}
                    seeking={sessionSeeking}
                    onTogglePlay={togglePlay}
                    onSeek={handleSeek}
                    onVolumeChange={state.setVolume}
                    onRateChange={(r) => { if (r !== state.rate) track('player-speed', { rate: r, source: 'menu' }); state.setRate(r); }}
                    onToggleMute={state.toggleMute}
                    onToggleFullscreen={state.toggleFullscreen}
                    onCaptionsClick={handleCaptionsClick}
                    onEmbedClick={handleEmbedClick}
                    onNext={next ? () => goNext('button') : null}
                    nextBusy={nextLoading}
                    autoplayNext={autoplayNext}
                    onToggleAutoplayNext={next ? toggleAutoplayNext : null}
                    nextLabel={next ? next.label : ''}
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
        speed: true,
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
// The STAGE is the element fullscreen is requested on, and the one thing that
// outlives a player: moving to the next file (next-item-go.js) destroys this
// player and mounts another INTO THE SAME STAGE, and a fullscreen element
// stays fullscreen for as long as it stays in the document, whatever happens
// to its children. Requested on the player's own container, as it used to
// be, fullscreen ended with every transition -- and a browser will not
// re-enter it without a gesture. `opts.stage` is that existing stage; an
// ordinary start makes its own.
export async function initPlayer(target, opts = {}) {
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
    let stage = opts.stage || null;
    if (!stage) {
        stage = document.createElement('div');
        stage.className = 'wt-player-stage';
        videoEl.parentNode.insertBefore(stage, videoEl);
    }
    stage.appendChild(mountEl);

    // Build the player container with video inside
    const playerContainer = document.createElement('div');
    // The shape of the picture that was here a moment ago (next-item-go.js).
    // Until `canplay` reports the real one the container has only the
    // default 16:9-ish height, shorter or taller than the stage that is
    // holding the old height -- and the controls, pinned to the container's
    // bottom, jumped up and back (owner, 2026-09-20). Episodes of a series
    // share their shape; if this one does not, canplay corrects it.
    if (opts.aspectRatio) playerContainer.style.aspectRatio = opts.aspectRatio;
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
        <PlayerComponent videoEl={videoEl} settings={settings} containerEl={playerContainer} showControls={showControls} fixedSize={!!(fixedWidth || fixedHeight)} trackContainer={target} trackHooks={trackHooks} awaitStart={!!opts.awaitStart} />,
        playerContainer
    );

    _currentPlayer = { stage, mountEl, playerContainer, videoEl };
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
// currentStage: the stage of the player on screen, for a caller that is about
// to replace that player and wants the next one in the same place.
export function currentStage() {
    return _currentPlayer ? _currentPlayer.stage : null;
}

// currentAspectRatio: the CSS aspect-ratio of the player on screen ('' before
// its first canplay), to hand to the player that replaces it.
export function currentAspectRatio() {
    return _currentPlayer ? (_currentPlayer.playerContainer.style.aspectRatio || '') : '';
}

// `keepStage`: the player goes, its stage stays in the document for the next
// one (see initPlayer).
export function destroyPlayer({ keepStage = false } = {}) {
    // Before the early return: "this page's player is gone" is true whether
    // or not one was mounted, and it is what stands a queued PUT retry
    // down (persistTrackChoice). The same goes for the picker's 'async'
    // listener, which is wired by wireTrackHandlers rather than by the
    // mount and so outlives it.
    releaseTrackDialog();
    if (!_currentPlayer) return;
    const { stage, mountEl, playerContainer, videoEl } = _currentPlayer;

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
    if (stage && !keepStage) stage.remove();

    _currentPlayer = null;
}

// The dialog's functions live in track-dialog.js; they are re-exported here
// for the callers that have always imported them from the player module.
export {
    activateSubtitle,
    findSubtitleItem,
    firstTrackSrc,
    markTrack,
    PUT_RETRY_DELAY_MS,
    setSubtitlesOff,
    setUploadPanel,
    swapSubtitlesDialog,
    syncUploadMarks,
    wireTrackHandlers,
} from './track-dialog.js';
