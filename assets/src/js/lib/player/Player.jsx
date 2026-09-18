import { render } from 'preact';
import { useRef, useState, useEffect, useCallback } from 'preact/hooks';
import { usePlayerState } from './hooks/usePlayerState';
import { useHls } from './hooks/useHls';
import { useWatchHistory } from './hooks/useWatchHistory';
import { createSessionSeeker } from './session-seek';
import { Hls } from './hls-manager';
import { applyCueOffset } from './cue-offset';
import { applySubtitleSelection, isEmbedded, readSelection, selectionFor, selectionHolds } from './subtitle-apply.js';
import { reloadSubtitleTrack, dropDeletedTracks } from './subtitle-track-reload.js';
import { readAllTracks, readTracks, resolveSubtitleLevel, selectEventData } from './subtitle-telemetry.js';
import { pickDefaultSubtitle, translationAction, hasSavedDefault } from './subtitle-rules.js';
import { pollProgress, progressText, withRev } from './subtitle-progress.js';
import { catchUpTiming, caughtUp, needsReload, nothingCountedYet, remaining, trailing, viewerFrontier } from './subtitle-catchup.js';
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
    readChips,
} from './track-picker.js';
import { offerTiming, offerVisible, pickOffer, suppressUpsell, upsellSuppressed } from './subtitle-offer.js';
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

// How often a DIRECT seek may kick the translation poll (a session seek is
// rationed by its POST). A held arrow key seeks once per key-repeat.
const DIRECT_SEEK_KICK_MS = 500;


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

    // --- the catching-up banner --------------------------------------
    //
    // A live (embedded-track) translation runs alongside the transcode and
    // can fall behind the playhead: the film plays on and the cues for
    // what is on screen have not been written yet. The service says where
    // its frontier is (X-Subtitle-Pending-From, movie time); this block
    // compares it with where the viewer is and offers to wait.
    //
    // No tier gate, and none wanted: a viewer without the entitlement
    // never has a translation running at all — the AI chip is rendered
    // locked upstream (data-locked, no data-src), so there is nothing here
    // for them to be gated out of.
    //
    // `null` or `{ remaining, waiting }`. `waiting` is the viewer having
    // pressed Wait: the film is paused on purpose and the poll is kept
    // awake, which is the one thing a pause normally switches off.
    const [catchUp, setCatchUp] = useState(null);
    // The same value as a ref, so a tick three seconds apart can tell
    // "nothing changed" from "changed back to the same shape" without
    // re-rendering the player every 3 s to find out.
    const catchUpRef = useRef(null);
    const waitingRef = useRef(false);
    // When Wait was pressed, for the seconds field of wait-done.
    const waitedSinceRef = useRef(0);
    // The × is per run and per stretch of film: it comes back on its own
    // once the translation catches up (below) and on a session seek
    // (kickTranslationPoll), because both mean the thing that was
    // dismissed is over.
    const dismissedRef = useRef(false);
    // The previous trailing answer, which is what the hysteresis band
    // between the two margins keeps.
    const trailingRef = useRef(false);
    // The running item's language and the last frontier the service
    // reported, for the two events. The handlers are rendered buttons, so
    // they are outside startTranslationProgress's closure where both are
    // in scope.
    const catchUpLangRef = useRef('');
    const pendingFromRef = useRef(null);
    // Whether the running source is live (X-Subtitle-Live), off the last
    // answer: the banner's cue count and the hold cap differ by it.
    const liveRunRef = useRef(false);
    // Movie time is video.currentTime + the session offset (see
    // applyCueOffset in cue-offset.js for the same arithmetic on cues).
    // Mirrored into a ref because the poll callbacks are built once and
    // would otherwise read the offset the run started with.
    const seekOffsetRef = useRef(0);
    seekOffsetRef.current = seekOffset;
    // Whether the current wait was started by a seek rather than by the
    // Wait button, and the timer that bounds it (catchUpTiming).
    const autoWaitRef = useRef(false);
    const holdTimerRef = useRef(null);
    // The last direct-seek kick, so holding an arrow key (one seek per
    // key-repeat, ~30/s) costs one immediate HEAD per DIRECT_SEEK_KICK_MS
    // rather than one per repeat. The hold window is re-opened every time
    // either way — it is the kick that is rationed, not the decision.
    const directKickAtRef = useRef(0);
    // A seek happened while a live run was playing: for a short window after
    // the seek settles (catchUpTiming.seekWatchMs) every answer is asked
    // whether to hold playback for the new position's subtitles. A window
    // and not the first answer: that one can describe the run before the
    // seek, or a document with none of the new run's cues in it yet, and
    // both read as "nothing pending".
    const seekHoldPendingRef = useRef(false);
    // The viewer has just started this run (owner, 2026-09-18): until it
    // counts its first cue, "nothing counted" is read as "behind the
    // playhead" -- see nothingCountedYet. Starting a translation over a
    // playing film is the same event as a seek landing on untranslated
    // film, so it opens the same hold window and is bounded by the same cap.
    const startHoldRef = useRef(false);
    // The silent hold (owner, 2026-09-18): a seek's hold was decided by an
    // answer that arrives after the seek has settled, so the film played
    // for a moment and was paused again. Now the film is paused the instant
    // the seek settles, with nothing on screen, and the first answer about
    // the new run either turns that into the ordinary hold (banner, cap) or
    // lets the film go. Bounded by catchUpTiming.seekPreHoldMaxMs.
    const preHoldRef = useRef(false);
    const preHoldTimerRef = useRef(null);
    // The same fact for the render: while it lasts the picture is a seek
    // that has not finished (spinner), not a paused film (big play button).
    const [preHolding, setPreHolding] = useState(false);
    // The keyboard handler is declared before the toggle wrapper it must
    // call (see togglePlay below), so it goes through this.
    const togglePlayRef = useRef(() => {});
    const seekSettledAtRef = useRef(0);
    // The timer that asks the service again while the window lasts.
    const holdWatchRef = useRef(null);
    // A seek's hold that ended while the tab was hidden: the film is played
    // when the viewer comes back, not in the background.
    const resumeOnVisibleRef = useRef(false);
    // Consecutive answers about another run, outside a seek's window. Past
    // catchUpTiming.runMismatchLimit the player stops naming its run and
    // takes answers at their word again: a mismatch that does not go away
    // (two sessions on one key, a failed offset read at mount) must not
    // silence the banner and strand a pressed Wait for the rest of the film.
    const runMismatchRef = useRef(0);

    // setCatchUp behind a value comparison: a tick arrives every 3 s and
    // almost all of them say exactly what the last one did. Re-rendering
    // the player on each would be the banner's whole cost.
    const caughtUpTimerRef = useRef(null);
    const showCatchUp = useCallback((next) => {
        const cur = catchUpRef.current;
        if (cur === next) return;
        // The "caught up" flash owns the slot for its few seconds: the tick
        // that follows it says "nothing to show", and must not cut it short.
        // Anything that has something to say does.
        if (cur && cur.caughtUp && !next) return;
        if (cur && next && cur.remaining === next.remaining && cur.waiting === next.waiting
            && !!cur.caughtUp === !!next.caughtUp) return;
        if (caughtUpTimerRef.current) {
            clearTimeout(caughtUpTimerRef.current);
            caughtUpTimerRef.current = null;
        }
        catchUpRef.current = next;
        setCatchUp(next);
    }, []);

    // flashCaughtUp: the pill does not just vanish when the subtitles are
    // back (owner, 2026-09-18) -- a viewer who was told "behind" is told
    // "caught up", and then the pill goes. Never over a dismissed banner.
    const flashCaughtUp = useCallback(() => {
        if (dismissedRef.current) {
            showCatchUp(null);
            return;
        }
        showCatchUp({ remaining: null, waiting: false, caughtUp: true });
        caughtUpTimerRef.current = setTimeout(() => {
            caughtUpTimerRef.current = null;
            if (catchUpRef.current && catchUpRef.current.caughtUp) {
                catchUpRef.current = null;
                setCatchUp(null);
            }
        }, catchUpTiming.caughtUpFlashMs);
    }, [showCatchUp]);

    // play() is not a promise everywhere (and is not implemented at all
    // under jsdom), so the rejection guard has to check before it chains.
    const resumePlayback = useCallback(() => {
        const video = videoRef.current;
        if (!video || typeof video.play !== 'function') return;
        const r = video.play();
        if (r && typeof r.catch === 'function') r.catch(() => {});
    }, []);

    // bannerRemaining is the number the banner may honestly say. On a live
    // source total is the document read so far, so total - done is the real
    // backlog; on a file source total is the whole film and the difference
    // is off by orders of magnitude exactly where the viewer decides
    // whether to wait — so no number at all (the copy drops its tail).
    // No count on a run that has counted nothing: "~0 cues to go" over a
    // translation that has not begun says the opposite of what is true.
    const bannerRemaining = (p) => (p.live && !nothingCountedYet(p) ? remaining(p) : null);

    // resumeAfterHold plays the film a seek's hold paused — unless the tab
    // is hidden: then the poll goes to sleep (nothing would put it there
    // otherwise, and an awake poll keeps the transcode and the translation
    // running for nobody) and the film plays when the viewer is back.
    const resumeAfterHold = useCallback(() => {
        if (typeof document !== 'undefined' && document.hidden) {
            resumeOnVisibleRef.current = true;
            const poll = pollStopRef.current;
            if (poll && poll.suspend) poll.suspend();
            return;
        }
        resumePlayback();
    }, [resumePlayback]);

    // clearWait drops a wait without deciding anything about playback:
    // every path that ends one (a run that stopped, play pressed, ×) goes
    // through it, so the seek hold's timer can never outlive its wait.
    //
    // It reports whether the wait it dropped was a seek's hold. That pause
    // is one the viewer never made, so a caller ending it for a reason that
    // is not the viewer's (the run died, another track, ×) plays the film
    // again; the Wait button's pause is the viewer's and stays.
    const clearWait = useCallback(() => {
        const wasAuto = waitingRef.current && autoWaitRef.current;
        waitingRef.current = false;
        autoWaitRef.current = false;
        if (holdTimerRef.current) clearTimeout(holdTimerRef.current);
        holdTimerRef.current = null;
        return wasAuto;
    }, []);

    // endPreHold drops the silent hold and reports whether there was one.
    // `play` is for the endings that are not a wait taking over.
    const endPreHold = useCallback((play) => {
        if (!preHoldRef.current) return false;
        preHoldRef.current = false;
        setPreHolding(false);
        if (preHoldTimerRef.current) clearTimeout(preHoldTimerRef.current);
        preHoldTimerRef.current = null;
        if (play) resumeAfterHold();
        return true;
    }, [resumeAfterHold]);

    // beginPreHold: only for a seek that may yet be held (the film was
    // playing, a translation is being polled), in a tab somebody is looking
    // at. pause() here reaches sleep(), which leaves the poll awake while
    // preHoldRef is set -- the answer is what this is waiting for.
    const beginPreHold = useCallback(() => {
        const video = videoRef.current;
        if (!video || video.paused || preHoldRef.current) return;
        if (!seekHoldPendingRef.current || !pollStopRef.current) return;
        if (typeof document !== 'undefined' && document.hidden) return;
        preHoldRef.current = true;
        setPreHolding(true);
        if (typeof video.pause === 'function') video.pause();
        preHoldTimerRef.current = setTimeout(() => {
            preHoldTimerRef.current = null;
            endPreHold(true);
        }, catchUpTiming.seekPreHoldMaxMs);
    }, [endPreHold]);

    // finishWait ends a wait on its own terms and plays the film: the run
    // got ahead (capped false), or a seek's hold ran out of time (capped
    // true) — then the run is still behind, so the banner stays and offers
    // Wait again.
    const finishWait = useCallback((capped) => {
        const auto = autoWaitRef.current;
        const seconds = Math.round((Date.now() - waitedSinceRef.current) / 100) / 10;
        clearWait();
        trailingRef.current = capped;
        if (capped) {
            showCatchUp({ remaining: catchUpRef.current ? catchUpRef.current.remaining : 0, waiting: false });
        } else {
            showCatchUp(null);
        }
        if (window.umami) window.umami.track('subtitle-translate-wait-done', {
            lang: catchUpLangRef.current,
            seconds,
            auto,
            capped,
        });
        // A hold ending in a tab nobody is looking at does not start the
        // film there (see resumeAfterHold).
        if (auto) resumeAfterHold();
        else resumePlayback();
    }, [clearWait, showCatchUp, resumePlayback, resumeAfterHold]);

    // beginWait pauses the film but keeps the poll awake. Both halves are
    // needed — the pause is the wait, and the HEAD every 3 s is what the
    // viewer waits on (for a live source it also keeps the transcoder
    // session and the translation reading it alive). `auto` is a wait a
    // seek started: bounded by catchUpTiming.seekHoldMaxMs.
    const beginWait = useCallback((auto, left) => {
        const video = videoRef.current;
        const playhead = ((video && video.currentTime) || 0) + seekOffsetRef.current;
        const pendingFrom = pendingFromRef.current;
        clearWait();
        // Set before pause(), because the `pause` event is what reaches
        // sleep() and sleep() reads this to decide not to suspend.
        waitingRef.current = true;
        autoWaitRef.current = auto;
        waitedSinceRef.current = Date.now();
        dismissedRef.current = false;
        if (auto) {
            // A file job retargets only at a batch boundary and then owes a
            // whole upstream call, so its floor is higher than a live run's
            // (which the transcoder restart already hid part of).
            const cap = liveRunRef.current ? catchUpTiming.seekHoldMaxMs : catchUpTiming.seekHoldMaxMsFile;
            holdTimerRef.current = setTimeout(() => {
                holdTimerRef.current = null;
                if (waitingRef.current && autoWaitRef.current) finishWait(true);
            }, cap);
        }
        if (video && typeof video.pause === 'function') video.pause();
        const poll = pollStopRef.current;
        // A no-op unless the run is suspended, which is exactly the case
        // it is here for: a viewer who paused first and pressed Wait
        // afterwards has a sleeping poll to wake.
        if (poll && poll.resume) poll.resume();
        showCatchUp({ remaining: left, waiting: true });
        if (window.umami) window.umami.track('subtitle-translate-wait', {
            lang: catchUpLangRef.current,
            behind: pendingFrom === null || pendingFrom === undefined ? 0 : Math.round(playhead - pendingFrom),
            auto,
        });
    }, [clearWait, finishWait, showCatchUp]);

    // `resumeHold: false` is for the player going away: nothing is played
    // on an element that is being torn down.
    const stopTranslationProgress = useCallback(({ resumeHold = true } = {}) => {
        pollingIdRef.current = '';
        // The banner belongs to the run: no run, nothing to catch up to.
        // No play() for the Wait button's pause — a run that died while the
        // viewer waited leaves the film paused with the big play button, and
        // the chip is what explains why. A seek's hold is different: the
        // viewer never paused, so the film goes back on.
        const waitHeld = clearWait();
        const preHeld = endPreHold(false);
        const heldBySeek = waitHeld || preHeld;
        seekHoldPendingRef.current = false;
        if (holdWatchRef.current) clearTimeout(holdWatchRef.current);
        holdWatchRef.current = null;
        if (!resumeHold) resumeOnVisibleRef.current = false;
        trailingRef.current = false;
        // The dismissal was about this run. The next one is a fresh
        // decision the viewer just made by picking a track.
        dismissedRef.current = false;
        showCatchUp(null);
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
        if (heldBySeek && resumeHold) resumeAfterHold();
    }, [showCatchUp, clearWait, endPreHold, resumeAfterHold]);

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
        // The banner's buttons are rendered outside this closure and both
        // of its events name the language.
        catchUpLangRef.current = lang;
        pendingFromRef.current = null;
        // Over a film that is playing, in a tab somebody is looking at. A
        // paused film is not running into anything, and a wait the viewer
        // is already in is theirs. While a session seek is in flight (the
        // resume prompt's answer, then the pill) the window opens when the
        // seek settles -- onSessionSeekingChange stamps it.
        const startVideo = videoRef.current;
        startHoldRef.current = !!startVideo && !startVideo.paused && !waitingRef.current
            && !(typeof document !== 'undefined' && document.hidden);
        if (startHoldRef.current) {
            dismissedRef.current = false;
            seekHoldPendingRef.current = true;
            seekSettledAtRef.current = sessionSeekingRef.current ? 0 : Date.now();
        }
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
            // A run that is gone cannot catch up with anything. No play():
            // a viewer who was waiting on it is left with the film paused
            // and the big play button, and the chip says what happened.
            // Unless it was a seek's hold, which the viewer never asked for.
            if (clearWait()) resumeAfterHold();
            trailingRef.current = false;
            showCatchUp(null);
            // The run is over, so every event it could still cause belongs
            // to nobody: bumping the counter is what makes onTrackError's
            // identity check fail. A <track> whose src this run set keeps
            // loading after an HTTP error or a timeout and fails a moment
            // later, and without this that late event landed as a second
            // subtitle-translate-error for the same run (trackErrorReported
            // only guards repeats of the track error itself).
            runSeqRef.current++;
            if (progressSpanRef.current === span) progressSpanRef.current = null;
            if (progressSpinnerRef.current === spinner) progressSpinnerRef.current = null;
            // 'stopped' is the service saying this run ended incomplete
            // (source_gone, too_large) -- the only failure where the count
            // on the chip is worth keeping: those cues are on screen and
            // are all there will ever be. Everything else clears the chip,
            // because a frozen percentage reads as a translation still
            // going. The spinner goes either way: it is the part that
            // claims work is happening.
            if (span) {
                if (code === 'stopped') {
                    span.title = tf('player.subtitleTranslationStopped');
                } else {
                    span.hidden = true;
                }
            }
            // Terminal, like 'done': the service ended this run, so
            // re-selecting the chip must not start another poll that gets
            // the same answer and reports it again. The title says "reload
            // to retry", and a reload is a fresh status map.
            if (code === 'stopped') translationStatusRef.current.set(id, 'stopped');
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
        // What the last swap brought into the <track>: the service's count
        // and frontier at that moment (see viewerFrontier).
        let loaded = null;
        // Where the cues actually in the track end, in movie time: the
        // stand-in for a frontier when the swap happened with nothing
        // pending. Cues are in time order; a shifted cue keeps its movie
        // time in __absEnd (cue-offset.js), an unshifted one is in movie
        // time already.
        const coverageEnd = () => {
            const video = videoRef.current;
            const el = video ? Array.from(video.querySelectorAll('track')).find((t) => t.id === id) : null;
            const cues = el && el.track ? el.track.cues : null;
            if (!cues || !cues.length) return null;
            const last = cues[cues.length - 1];
            if (!last) return null;
            return last.__absEnd !== undefined ? last.__absEnd : (last.endTime || 0) + seekOffsetRef.current;
        };
        const reload = (done, force, frontier = null) => {
            const now = Date.now();
            if (!force && now - lastReloadAt < TRACK_RELOAD_INTERVAL_MS) return;
            // Only a reload that actually swapped the src spends the
            // throttle window: stamping on a no-op (same revision, or no
            // <track> element yet) would hold off the next real one for
            // another 15 s.
            if (reloadSubtitleTrack(videoRef.current, id, withRev(src, done), onTrackError)) {
                lastReloadAt = now;
                loaded = { done, frontier: frontier === undefined ? null : frontier };
            }
        };
        if (!resume && window.umami) window.umami.track('subtitle-translate-start', {
            lang,
            // Which human track the machine works from: a translation of
            // an OpenSubtitles imdb match is a weaker claim than one of
            // the viewer's own upload.
            source: el.getAttribute('data-source-badge') || '',
        });
        // The pause/visibility listeners below see transitions, not state,
        // and two entry states fire no event at all: a video that has never
        // played (autoplay blocked, or the mount-time restore of a saved
        // track) and a tab that was already in the background. Left
        // unsuspended those runs never sleep, and since a live run's cap is
        // an inactivity cap they would hold a transcoder session for the
        // length of the film with nobody watching.
        //
        // But only a LIVE source costs that. The check therefore waits for
        // the first response, which is what reveals X-Subtitle-Live: a
        // cached OpenSubtitles job is seconds of work with no session
        // behind it, and pausing the film to open the picker and press
        // "Translate to Portuguese" is exactly when people start one. Doing
        // it before the first HEAD left that viewer looking at `· 0%` and a
        // spinner that never moved until they pressed play.
        let initialStateChecked = false;
        const suspendIfNobodyIsWatching = (p) => {
            if (initialStateChecked) return;
            initialStateChecked = true;
            if (!p.live) return;
            if (!document.hidden && !(videoRef.current && videoRef.current.paused)) return;
            if (pollStopRef.current) pollStopRef.current.suspend();
        };
        pollStopRef.current = pollProgress(src, {
            // The run a transcoder-session player is watching, so the
            // service answers about it (see pollProgress).
            sessionOffset: () => (videoRef.current && videoRef.current.dataset.sessionId
                && runMismatchRef.current < catchUpTiming.runMismatchLimit ? seekOffsetRef.current : null),
            // The viewer's playhead in movie time, on every poll: a
            // file-source job orders its batches by it, the way a live one
            // follows the playlist offset, and answers the frontier
            // against it.
            position: () => {
                // Mid session-seek the two halves disagree: the offset is
                // already the new run's, currentTime still the old run's.
                // No position beats a wrong one — the stored one stands,
                // and the settle kick sends the right value moments later.
                if (sessionSeekingRef.current) return null;
                const v = videoRef.current;
                return v ? (v.currentTime || 0) + seekOffsetRef.current : null;
            },
            onProgress: (p) => {
                cues = p.total;
                if (span) {
                    // Three states (queued, live, counting) in one place,
                    // in subtitle-progress.js where they can be tested
                    // without a player: see progressText.
                    const chip = progressText(p);
                    span.textContent = chip.text;
                    span.title = tf(chip.key, ...chip.args);
                }
                // total === 0 means the job has not counted the cues yet:
                // the file on the other end is still empty, so a reload
                // would only replace subtitles with nothing.
                // p.forceReload is subtitle-progress.js's kick() marking
                // the first change after a session seek: bypass the 15 s
                // throttle for this one swap, so the new position's cues
                // do not wait out both the service's own lag and this
                // reload throttle on top of it.
                // done === 0 is the same argument one step later: counted,
                // nothing translated. The body carries no cue the viewer
                // can read, and the swap would spend the 15 s throttle
                // window on it -- so the first real batch, seconds away,
                // would sit unloaded behind that window.
                if (p.total > 0 && p.done > 0) reload(p.done, p.forceReload === true, p.pendingFrom);
                // Last, so the chip has already been painted with the
                // opening count before the run goes to sleep on it.
                suspendIfNobodyIsWatching(p);
            },
            // Every 200, changed or not: the question this answers is
            // "where is the viewer relative to the translation", and the
            // viewer keeps moving while the counts stand still.
            onTick: (p) => {
                const video = videoRef.current;
                if (!video) return;
                // Presence is the gate: the service sends the frontier
                // only when it can stand behind one — a live run against
                // its playlist offset, a file job against the playhead
                // this poll itself carried (`pos`). An old service sends
                // neither and nothing here fires.
                liveRunRef.current = p.live === true;
                const playhead = (video.currentTime || 0) + seekOffsetRef.current;
                // The frontier everything below is decided by is the
                // VIEWER's: the service may be ahead of what the throttled
                // <track> has loaded, and then the honest answer to "are
                // there subtitles where I am" is the track's edge.
                pendingFromRef.current = viewerFrontier({
                    serviceFrontier: p.pendingFrom,
                    serviceDone: p.done,
                    loaded,
                    coverageEnd: coverageEnd(),
                });
                // ...and the cure for that state is a swap now, not when
                // the throttle allows: the viewer is about to run out of
                // loaded cues and the service has the next ones.
                // Not for a film the viewer paused -- nobody is running out
                // of anything -- but a WAIT is a pause too, and it is exactly
                // the state this has to end: without the swap the track's
                // edge never moves and the wait never finishes.
                if ((!video.paused || waitingRef.current) && needsReload({ serviceDone: p.done, loaded, frontier: pendingFromRef.current, playhead })) {
                    reload(p.done, true, p.pendingFrom);
                    pendingFromRef.current = p.pendingFrom;
                }
                // Which run this answer describes. A service that does not
                // say is taken at its word, as before it said, and so is one
                // that has disagreed for too long (runMismatchRef).
                const saysRun = p.sessionOffset !== null && p.sessionOffset !== undefined;
                const matches = saysRun && Math.abs(p.sessionOffset - seekOffsetRef.current) < 1;
                if (saysRun && !seekHoldPendingRef.current) {
                    runMismatchRef.current = matches ? 0 : runMismatchRef.current + 1;
                }
                const aboutThisRun = !saysRun || matches
                    || runMismatchRef.current >= catchUpTiming.runMismatchLimit;
                // A run the viewer just started and that has counted nothing
                // is behind by definition; the first counted answer ends the
                // special case and the frontier speaks for itself.
                if (!nothingCountedYet(p)) startHoldRef.current = false;
                const behind = !caughtUp(pendingFromRef.current, playhead)
                    || (startHoldRef.current && nothingCountedYet(p));
                // The window after a seek settled: hold playback for the new
                // position's subtitles as soon as an answer about the new run
                // shows the translation is not comfortably ahead of it. An
                // answer that shows nothing pending does not end the window:
                // it may predate the new run's cues. Once per seek, only for
                // a film that was playing when the viewer seeked, and never
                // in a hidden tab.
                if (seekHoldPendingRef.current && !sessionSeekingRef.current) {
                    const inWindow = Date.now() - seekSettledAtRef.current <= catchUpTiming.seekWatchMs;
                    // The silent hold's own pause is not the viewer's.
                    const viewerPaused = video.paused && !preHoldRef.current;
                    if (!inWindow || document.hidden || viewerPaused || waitingRef.current) {
                        seekHoldPendingRef.current = false;
                        endPreHold(true);
                    } else if (aboutThisRun && behind) {
                        seekHoldPendingRef.current = false;
                        // The wait takes the pause over: no play in between.
                        endPreHold(false);
                        beginWait(true, bannerRemaining(p));
                        return;
                    } else {
                        // "Not behind", said about this run: the silent hold
                        // has its answer and the film goes on. (An answer
                        // about another run decides nothing; the hold waits
                        // for the next one under its own cap.) A live run
                        // that names no frontier may simply not have read
                        // the new run's cues yet -- its subtitle segments
                        // close seconds later -- and holding blind for that
                        // would stall every seek on a translation that is
                        // keeping up. So on a live source a hold can still
                        // arrive after a moment of playback; the window
                        // keeps watching for it.
                        if (aboutThisRun) endPreHold(true);
                        if (!holdWatchRef.current) {
                            // Not decided yet: ask again in a moment, rather
                            // than on the next 3 s tick.
                            holdWatchRef.current = setTimeout(() => {
                                holdWatchRef.current = null;
                                const poll = pollStopRef.current;
                                if (seekHoldPendingRef.current && poll && poll.kick) poll.kick();
                            }, catchUpTiming.seekWatchEveryMs);
                        }
                    }
                }
                // Nothing below acts on an answer about another run: not the
                // end of a wait, not the banner.
                if (!aboutThisRun) return;
                if (waitingRef.current) {
                    if (behind) {
                        showCatchUp({ remaining: bannerRemaining(p), waiting: true });
                        return;
                    }
                    // The wait is over on its own terms: the translation is
                    // comfortably ahead again, so the film goes back on --
                    // with those subtitles loaded. The wait's promise is
                    // "there will be subtitles when it plays", and the
                    // service being ahead is only half of it: the <track>
                    // still holds whatever revision the throttle last let
                    // in. (Found by the owner, 2026-09-18: subtitles showed
                    // up only after pause -> play, whose kick bypasses the
                    // throttle.)
                    if (p.done > 0) reload(p.done, true, p.pendingFrom);
                    finishWait(false);
                    flashCaughtUp();
                    return;
                }
                // A paused film is not running into anything. The one
                // tick that reaches here paused is a run started before
                // the first play (suspendIfNobodyIsWatching sleeps it on
                // this same answer): at t=0 against a fresh run's
                // frontier the comparison would say "behind" over a film
                // that has not started. Nothing else is touched — the
                // answer will be recomputed by the first tick after play.
                if (video.paused) {
                    showCatchUp(null);
                    return;
                }
                const wasTrailing = trailingRef.current;
                const isTrailing = trailing(trailingRef.current, pendingFromRef.current, playhead)
                    || (startHoldRef.current && nothingCountedYet(p));
                trailingRef.current = isTrailing;
                if (isTrailing && !dismissedRef.current) {
                    showCatchUp({ remaining: bannerRemaining(p), waiting: false });
                    return;
                }
                // The same promise without a wait: the banner going away
                // says "the translation is ahead of you now", which is only
                // true on screen once the track has the cues that made it so.
                if (wasTrailing && !isTrailing) {
                    if (p.done > 0) reload(p.done, true, p.pendingFrom);
                    flashCaughtUp();
                } else {
                    showCatchUp(null);
                }
                // A dismissal is about a stretch of film that the
                // translation was behind on. Once it is no longer behind,
                // that stretch is over and the next one gets its own say.
                if (!isTrailing) dismissedRef.current = false;
            },
            onDone: (p) => {
                pollStopRef.current = null;
                pollingIdRef.current = '';
                if (clearWait()) resumeAfterHold();
                trailingRef.current = false;
                showCatchUp(null);
                if (progressSpanRef.current === span) progressSpanRef.current = null;
                if (progressSpinnerRef.current === spinner) progressSpinnerRef.current = null;
                // Final: a later re-selection must neither poll nor
                // report this translation again.
                translationStatusRef.current.set(id, 'done');
                cues = p.total || cues;
                reload(p.done, true, null);
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
    }, [stopTranslationProgress, showCatchUp, clearWait, beginWait, finishWait, resumeAfterHold]);

    // translationActionFor answers translationAction for a list element,
    // with one addition the pure rule cannot know: the item whose poll is
    // running right now needs nothing done to it at all.
    const translationActionFor = useCallback((el) => {
        const data = itemData(el);
        if (data.id && data.id === pollingIdRef.current) return 'none';
        return translationAction(data, translationStatusRef.current);
    }, []);

    // A running poll sleeps with the video. The HEAD every 3 s is what
    // tells the translate service somebody is still watching, and for a
    // live (embedded-track) source it is also what holds the transcoder
    // session and its FFmpeg run open: the job keeps pulling the subtitle
    // playlist for as long as we keep asking. A viewer who paused or
    // tabbed away is not watching, and a feature film's worth of transcode
    // and translation for nobody is real money.
    //
    // Suspending is not stopping: no onError, no telemetry, the chip keeps
    // its count and its spinner, and the same run continues afterwards.
    // Resuming costs nothing either — the service holds partial progress
    // for 24 h and re-aligns by cue identity.
    useEffect(() => {
        const video = videoRef.current;
        if (!video) return;
        const sleep = () => {
            // A viewer who pauses (or leaves the tab) outside a seek has
            // answered the hold question for this seek: no hold later, on
            // whatever play comes next. Without this the flag outlived a
            // pause that dropped the answer it was waiting for, and the next
            // play paused the film again.
            // A hidden tab counts even mid-seek: the decision would otherwise
            // be made in the background, or minutes later on the way back.
            if ((!sessionSeekingRef.current || document.hidden) && !waitingRef.current && !preHoldRef.current) seekHoldPendingRef.current = false;
            // Wait is the exception, and it is the whole of the feature:
            // the viewer paused *so that* the translation can catch up,
            // and the HEAD every 3 s is what it catches up against.
            // Suspending here would pause the film against a run that is
            // no longer being asked for anything.
            // The silent hold likewise: it paused in order to be answered.
            if (waitingRef.current || preHoldRef.current) return;
            const poll = pollStopRef.current;
            if (poll && poll.suspend) poll.suspend();
        };
        const wake = () => {
            const poll = pollStopRef.current;
            if (poll && poll.resume) poll.resume();
        };
        // A viewer who presses play has answered the banner's question
        // themselves: the wait is over, and the next tick renders the
        // ordinary trailing banner again if the run is still behind.
        const onPlay = () => {
            resumeOnVisibleRef.current = false;
            clearWait();
            endPreHold(false);
            // Play IS "Keep watching": whichever of the two the viewer
            // presses, the banner must stop saying the film is paused for
            // them now, not on the next 3 s tick (owner, 2026-09-18).
            const banner = catchUpRef.current;
            if (banner && banner.waiting) showCatchUp({ remaining: banner.remaining, waiting: false });
            wake();
        };
        // The `play` event is the authority on playback — `paused` is not
        // yet false in every engine when it fires — so it wakes the poll
        // without a second opinion. Coming back to the tab is not: a
        // visible tab showing a paused film is still nobody watching.
        const onVisibility = () => {
            if (document.hidden) {
                sleep();
                return;
            }
            // A seek's hold ended while the tab was hidden: its play was put
            // off until now (resumeAfterHold). The play event wakes the poll.
            if (resumeOnVisibleRef.current) {
                resumeOnVisibleRef.current = false;
                if (typeof video.play === 'function') {
                    const r = video.play();
                    if (r && typeof r.catch === 'function') r.catch(() => {});
                }
                return;
            }
            if (!video.paused) wake();
        };
        video.addEventListener('pause', sleep);
        video.addEventListener('play', onPlay);
        document.addEventListener('visibilitychange', onVisibility);
        return () => {
            video.removeEventListener('pause', sleep);
            video.removeEventListener('play', onPlay);
            document.removeEventListener('visibilitychange', onVisibility);
        };
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
            // The viewer has dealt with subtitles themselves -- in the
            // picker, or through the pill, which lands here too.
            setOffer(null);
            setOfferCard(false);
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
        // nothing was chosen here that was not already chosen.
        //
        // This is the one automatic start left after the engagement gate
        // went away, and it is no longer free: a saved translation of an
        // embedded track is a live job, so the mount starts a transcode and
        // holds a job slot rather than replaying a cached artifact. It
        // still runs, because the viewer asked for this track and expects
        // it back; what bounds it is that the poll sleeps with the video
        // (see the pause/visibility effect above), so a page opened and
        // left alone stops paying as soon as it is hidden.
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
            stopTranslationProgress({ resumeHold: false });
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

    // Watch history hook (position tracking + resume).
    // `paused` prevents overwriting saved position while resume prompt is open.
    const { resumePosition, resumeReady, forceSendPosition } = useWatchHistory(videoRef, {
        resourceID, path,
        currentTime: state.currentTime,
        duration: state.duration,
        playing: state.playing,
        paused: showResumePrompt,
    });

    // A session seek moves the video to a spot the running translation has
    // not caught up to yet. Left alone, the first cues for that position
    // wait out whatever is left of the poll's 3 s interval on top of the
    // service's own lag, and then the reload throttle
    // (TRACK_RELOAD_INTERVAL_MS) on top of that. kick() (a no-op when no
    // poll is running) takes one HEAD tick right away and marks the next
    // changed report to bypass the reload throttle once — see reload()'s
    // p.forceReload above and pollProgress's kick() in subtitle-progress.js.
    //
    // On the DIRECT seek path this is additionally rationed by
    // DIRECT_SEEK_KICK_MS (see handleSeek): a session seek is naturally
    // rationed by its POST, a direct one is not.
    const kickTranslationPoll = useCallback(() => {
        // A seek is a new stretch of film, so a banner the viewer
        // dismissed for the old one is no longer being answered.
        dismissedRef.current = false;
        const poll = pollStopRef.current;
        const video = videoRef.current;
        // A seek during a seek's hold: that hold was for the old position,
        // and a paused element would keep the new one from ever playing
        // (the hls.js seek path waits for `playing` and never calls play()
        // itself). Not covered by the wiring tests: under jsdom there is no
        // hls.js, and the native path the harness takes plays on its own.
        // The silent hold counts the same: both are pauses the viewer never
        // made, over a film they were watching.
        const waitHeld = clearWait();
        const preHeld = endPreHold(false);
        const heldBySeek = waitHeld || preHeld;
        if (heldBySeek) resumePlayback();
        resumeOnVisibleRef.current = false;
        // Decided when the seek settles (see onTick), recorded now: by then
        // the seek itself may have started playback, and what matters is
        // whether the viewer was watching when they seeked.
        seekHoldPendingRef.current = !!poll && !!video
            && (heldBySeek || (!video.paused && !waitingRef.current));
        seekSettledAtRef.current = 0;
        // A new run: whatever disagreement there was belongs to the old one.
        runMismatchRef.current = 0;
        if (holdWatchRef.current) clearTimeout(holdWatchRef.current);
        holdWatchRef.current = null;
        if (poll && poll.kick) poll.kick();
    }, [clearWait, endPreHold, resumePlayback]);

    // The seek has settled (the new run is playing): ask the service where
    // the translation is now, rather than on the next 3 s tick, so a hold
    // for the new position's subtitles starts within one request.
    const onSessionSeekingChange = useCallback((val) => {
        setSessionSeekingWithRef(val);
        if (val || !seekHoldPendingRef.current) return;
        // The hold window starts here, with the new run playing -- and the
        // film stands still until the first answer about it (beginPreHold),
        // so a hold never has to interrupt a film that has just started.
        seekSettledAtRef.current = Date.now();
        beginPreHold();
        const poll = pollStopRef.current;
        if (poll && poll.kick) poll.kick();
    }, [setSessionSeekingWithRef, beginPreHold]);

    // Wait: pause the film but keep the poll awake. Both halves are
    // needed — the pause is what the viewer asked for, and the poll is
    // what the HEAD every 3 s keeps alive on the service (and, for a live
    // source, the transcoder session the translation is reading).
    const handleWait = useCallback(() => {
        beginWait(false, catchUpRef.current ? catchUpRef.current.remaining : 0);
    }, [beginWait]);

    // Keep watching: the viewer overrules the wait. The banner is not
    // rewritten here — what it should say next is the next tick's answer,
    // and the run may well still be behind.
    const handleKeepWatching = useCallback(() => {
        clearWait();
        resumePlayback();
    }, [clearWait, resumePlayback]);

    // ×: stop saying it. The wait goes with it (a dismissed banner that
    // still pauses the film and restarts it three seconds later would be
    // the opposite of dismissed). After the Wait button nothing is played —
    // the film stays where the viewer left it; after a seek's hold, which
    // the viewer never made, the film plays.
    const handleDismissCatchUp = useCallback(() => {
        dismissedRef.current = true;
        if (clearWait()) resumePlayback();
        showCatchUp(null);
    }, [clearWait, showCatchUp, resumePlayback]);

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
    const offerTimerRef = useRef(null);

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
    }, [state.playing, isVideo, trackContainer, trackOffer, resumeReady, resumePosition, resumeAnswered]);

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

    // Seek handler (session or direct)
    const handleSeek = useCallback((time, { play = false } = {}) => {
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
                    onSeekOffsetChange: (offset) => {
                        setSeekOffset(offset);
                        // Now, not on the next render: the kick below polls
                        // with it, and the answer is judged against it.
                        seekOffsetRef.current = offset;
                        kickTranslationPoll();
                    },
                    onSeekingChange: onSessionSeekingChange,
                    trackContainer,
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
                const now = Date.now();
                if (now - directKickAtRef.current >= DIRECT_SEEK_KICK_MS) {
                    directKickAtRef.current = now;
                    kickTranslationPoll();
                }
                seekSettledAtRef.current = now;
                beginPreHold();
            }
        }
    }, [isSession, sessionSeekUrl, sourceUrl, kickTranslationPoll, onSessionSeekingChange, beginPreHold]);

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

    // A film with a saved position does not start by itself (owner,
    // 2026-09-18): <video autoplay> would play it from the beginning behind
    // the prompt, sound included, while the viewer is still choosing. Held
    // from the moment the saved position is known until the prompt is
    // answered; 'play' is listened for because autoplay may fire after the
    // prompt is already up (canplay arrives when the stream is ready, not
    // when the page is). Either answer starts playback.
    useEffect(() => {
        if (!resumeReady || !(resumePosition > 0) || resumeAnswered) return;
        const video = videoRef.current;
        if (!video) return;
        const hold = () => { if (!resumeAnsweredRef.current && !video.paused) video.pause(); };
        hold();
        video.addEventListener('play', hold);
        return () => video.removeEventListener('play', hold);
    }, [resumeReady, resumePosition, resumeAnswered]);

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
            // The seeker starts the new run itself.
            handleSeek(resumePosition, { play: true });
        } else {
            video.currentTime = resumePosition;
            playAfterPrompt();
        }
        // Save resumed position immediately
        const dur = duration > 0 ? duration : (video.duration || 0);
        if (dur > 0) forceSendPosition(resumePosition, dur);
    }, [resumePosition, isSession, sessionSeekUrl, handleSeek, duration, forceSendPosition, playAfterPrompt]);

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

    // One toggle for every control (click, space, the big button, the
    // bar). During the silent hold the element is paused by the player
    // while the viewer is looking at a seek that has not finished: their
    // toggle means "pause", not "play". It ends the hold without playing
    // and withdraws the hold question for this seek.
    const togglePlay = useCallback(() => {
        if (preHoldRef.current) {
            endPreHold(false);
            seekHoldPendingRef.current = false;
            return;
        }
        state.togglePlay();
    }, [endPreHold, state.togglePlay]);
    togglePlayRef.current = togglePlay;

    // Click on video to toggle play (video only).
    // Use ref for showResumePrompt to avoid re-registering native DOM listeners.
    const showResumePromptRef = useRef(false);
    showResumePromptRef.current = showResumePrompt;

    const handleVideoClick = useCallback((e) => {
        if (!isVideo || sessionSeekingRef.current || showResumePromptRef.current) return;
        if (e.target.closest('.wt-player-controls')) return;
        if (e.target.closest('.wt-resume-prompt')) return;
        if (e.target.closest('.wt-catchup')) return;
        if (e.target.closest('.wt-offer-card')) return;
        togglePlayRef.current();
        resetHideTimer();
    }, [isVideo]);

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
                <div class="wt-catchup" role="status" data-remaining={catchUp.remaining}
                     onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
                    <LoadingSpinner />
                    <span class={`wt-catchup-text${catchUp.remaining === null ? '' : ' wt-catchup-text--long'}`}>
                        {catchUp.remaining === null
                            ? t(catchUp.waiting ? 'player.subtitleCatchUpWaitingShort' : 'player.subtitleCatchUpShort')
                            : tf(catchUp.waiting ? 'player.subtitleCatchUpWaiting' : 'player.subtitleCatchUp', catchUp.remaining)}
                    </span>
                    {/* A phone has no room for the cue count: three lines of
                        pill over the picture (owner, 2026-09-18). The short
                        copy is a sibling, not a second child of the text
                        span, and CSS shows exactly one of the two. */}
                    {catchUp.remaining !== null && (
                        <span class="wt-catchup-text-short">
                            {t(catchUp.waiting ? 'player.subtitleCatchUpWaitingShort' : 'player.subtitleCatchUpShort')}
                        </span>
                    )}
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
            {showControls && isVideo && (sessionSeeking || preHolding || (state.playing && state.loading)) && (
                <div class="wt-player-overlay wt-player-overlay--loading">
                    <LoadingSpinner />
                </div>
            )}

            {/* Big play button — shown when paused, regardless of loading state */}
            {showControls && isVideo && !state.playing && !sessionSeeking && !preHolding && !showResumePrompt && (
                <div class="wt-player-overlay wt-player-overlay--play" onDblClick={(e) => e.stopPropagation()}>
                    <button type="button" class="wt-player-big-play" onClick={(e) => { e.stopPropagation(); togglePlay(); }} aria-label={t('player.play')}>
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
                    onTogglePlay={togglePlay}
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
export function findSubtitleItem(modal, id) {
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
export function setSubtitlesOff(container, modal, off) {
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

// offerStorage: merely touching window.localStorage throws in a sandboxed
// frame or with site data blocked; subtitle-offer.js treats null as "no
// memory".
function offerStorage() {
    try { return window.localStorage; } catch (e) { return null; }
}

// readUpsellCard takes the upsell's words from the picker's own card
// (#translate-cta): one sentence, one CTA, one /donate link with the
// language prefix, all rendered by the server -- so the pill's card and the
// picker's can never disagree, and no locale carries the sentence twice.
function readUpsellCard(modal) {
    const box = modal ? modal.querySelector('#translate-cta') : null;
    const a = box ? box.querySelector('a[href]') : null;
    const text = box ? box.querySelector('div') : null;
    return {
        text: text ? text.textContent.trim() : '',
        cta: a ? a.textContent.trim() : '',
        href: a ? a.getAttribute('href') : '',
        tier: a ? a.getAttribute('data-umami-event-tier') || '' : '',
    };
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
export function activateSubtitle(container, target, { persist = true } = {}) {
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
    // Both halves of "what is on screen" — the hls.js selection and the
    // element modes — are written by applySubtitleSelection and nowhere
    // else, so a chip and the player cannot disagree about which of the two
    // renderers is drawing. See subtitle-apply.js for why hls.js-managed
    // tracks go to 'disabled' rather than 'hidden'.
    //
    // window.hlsPlayer is undefined until useHls creates the instance, and
    // a mount-time activation (a translation saved in an earlier session
    // coming back) routinely runs before that: then only the element modes
    // land here, and initDefaultTracks applies the same selection to hls.js
    // on the first canplay.
    const player = container.querySelector('video.player, audio.player');
    applySubtitleSelection(player, window.hlsPlayer || null, selectionFor(target));
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
export function setUploadPanel(modal, open) {
    const panel = modal.querySelector('#my-uploads-panel');
    if (!panel) return;
    panel.hidden = !open;
    const toggle = modal.querySelector('#my-uploads-toggle');
    if (toggle) toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    const wrap = modal.querySelector('#my-subtitles');
    if (wrap) wrap.setAttribute('data-upload-open', open ? 'true' : 'false');
}

// isUpload is what "my uploads" means now that the MY chips live in the
// radiogroup with everything else (adoptUploadChips moves in the ones an
// async reload delivers). Containment in #my-subtitles used to be the test
// and no longer can be: that wrapper holds the disclosure and the panel,
// and — between the swap and the adoption — a fresh chip for one instant.
// The provider is the honest question anyway: these are the chips the
// uploads partial re-renders, and therefore the ones whose markers the
// client owns.
function isUpload(el) {
    return el.getAttribute('data-provider') === 'UserSubtitle';
}

function setUploadMark(el, on) {
    setChipActive(el, on);
    if (on) el.setAttribute('data-default', 'true');
    else el.removeAttribute('data-default');
}

// syncUploadMarks re-derives the uploads' active marker from what is
// actually playing. The <track default> in <video> drives playback
// correctly on reload, but the uploads' chips are re-rendered by the
// partial and never go through the click path, so they lose the marker;
// this runs at wiring time and after every async swap.
//
// Module scope rather than a closure inside wireTrackHandlers: it is the
// least-covered piece of this file (the stage checklist carried it as a
// by-hand item), and a function the tests can call is one they can drive
// through states a real player takes minutes to reach.
export function syncUploadMarks(container, subtitlesModal) {
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
        // while nothing else claims it. An embedded (MediaProbe) track is
        // driven by hls.js and has no <track> element at all, so "no
        // showing textTrack" does not mean "nothing is playing" — and a
        // stale default left on a side-loaded track would then hand the
        // marker to an upload as well, leaving two chips with data-default
        // and two check marks.
        for (const el of scope.querySelectorAll('.subtitle[data-default="true"]')) {
            if (!isUpload(el)) return;
        }
        const dt = video.querySelector('track[default]');
        if (dt && dt.id) activeID = dt.id;
    }
    if (!activeID) return;
    const active = mine.find((el) => el.getAttribute('data-id') === activeID) || null;
    for (const el of mine) setUploadMark(el, el === active);
    if (!active) return;
    // Exactly one marker per group, as after a click: markTrack's clearing
    // loop without the PUT — nothing was chosen here, the DOM is only
    // catching up with what is already playing.
    for (const el of scope.querySelectorAll('.subtitle')) {
        if (el === active || isUpload(el)) continue;
        setUploadMark(el, false);
    }
}

// The 'async' listener the picker installs on `window`, kept at module
// scope so wireTrackHandlers can replace it and destroyPlayer can take it
// off. See the comment at the addEventListener call below.
let asyncSwapListener = null;

// wireTrackHandlers binds the picker modals. It stays module-scope and
// ref-free: `hooks` is the object the mounted component fills with
// onSubtitleSelect/onAudioSelect (see the trackHooks effect), so the
// session-scoped state those need lives in the component, not here.
export function wireTrackHandlers(container, hooks = {}) {
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

    syncUploadMarks(container, subtitlesModal);
    // loadAsyncView dispatches an 'async' CustomEvent after swapping a
    // target's innerHTML; re-sync when #my-subtitles content is replaced.
    const mySubsContainer = container.querySelector('#my-subtitles');
    // Taken off again before a new one goes on, and by destroyPlayer.
    // wireTrackHandlers runs once per async navigation and the listener
    // closes over that page's #my-subtitles, so one left behind accumulates
    // and pins a detached node. Identity-guarded, so the old ones were
    // inert rather than wrong -- which is exactly why nobody noticed.
    if (asyncSwapListener) {
        window.removeEventListener('async', asyncSwapListener);
        asyncSwapListener = null;
    }
    if (mySubsContainer) {
        asyncSwapListener = (e) => {
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
                else syncUploadMarks(container, subtitlesModal);
            }
            // The set of chips itself changed, so this is the full pass and
            // not refreshMarks: an upload can bring a language the server
            // never rendered a chip for, and a delete can empty the expanded
            // one. `current` keeps the viewer where they were whenever that
            // language survived the swap.
            if (subtitlesModal) refresh(subtitlesModal, { current: expandedLang(subtitlesModal) });
        };
        window.addEventListener('async', asyncSwapListener);
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
// chose this". When it does persist, the write in flight is returned (see
// persistTrackChoice); no call site in the player awaits it.
export function markTrack(container, el, type, persist = true) {
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
    return persistTrackChoice(type, {
        id: el.getAttribute('data-id'),
        resourceID: s.getAttribute('data-resource-id'),
        itemID: s.getAttribute('data-item-id'),
    });
}

// PUT_RETRY_DELAY_MS is how long the one retry below waits. Long enough for
// an edge that is refusing connections to finish failing over, short enough
// that a viewer who closes the tab straight after clicking still has a
// decent chance of the write landing.
export const PUT_RETRY_DELAY_MS = 1000;

// persistTrackChoice writes the viewer's pick back to the session, and
// retries it once.
//
// Why: on stage two of these came back 503 from the edge without ever
// reaching the pod, and the choice was silently lost — the request is
// fire-and-forget, so nothing noticed and nothing told the viewer. A single
// retry covers the failure this actually is (an edge blip, a dropped
// connection) without turning a busy backend into a stampede.
//
// What is retried and what is not. A rejected fetch is a network error: the
// request may not have been made at all. A 5xx is the server saying it
// could not handle it. A 4xx is an answer — a stale CSRF token, a session
// that ended, an id the server refuses — and repeating it would get the
// same answer, so it is left alone. Nothing is retried twice: a second
// failure is not a blip.
//
// Still fire-and-forget: no UI, no error surface, and every failure ends
// swallowed. The returned promise is the write in flight, which is what
// makes the retry testable without a sleep in the test.
//
// A retry only ever resends the LATEST choice of its kind. The body is read
// eagerly at markTrack time, so a second later it may name a track the
// viewer has already moved on from: click A (503), click B (200), and A's
// retry would land last and make the next page load restore A. That is
// worse than the failure it exists to fix — before the retry a lost PUT
// only lost that choice; it could not overwrite a newer one. Each call
// takes the next number for its kind, and the retry stands down when it is
// no longer holding it. Audio and subtitle count separately: they are two
// independent choices and one must not cancel the other's retry.
const putSeq = new Map();

// And a retry does not outlive the player that queued it. destroyPlayer
// bumps this, so a write scheduled by a page the viewer has navigated away
// from cannot land — possibly into the next file's session.
let playerGeneration = 0;

function persistTrackChoice(type, body) {
    const seq = (putSeq.get(type) || 0) + 1;
    putSeq.set(type, seq);
    const generation = playerGeneration;
    const stillCurrent = () => putSeq.get(type) === seq && playerGeneration === generation;
    const send = () => fetch(`/stream-video/${type}`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-TOKEN': window._CSRF,
        },
        body: JSON.stringify(body),
    });
    const retry = () => new Promise((resolve) => setTimeout(resolve, PUT_RETRY_DELAY_MS))
        .then(() => (stillCurrent() ? send() : undefined));
    return send().then(
        (res) => (res && res.status >= 500 ? retry() : res),
        retry,
    ).catch(() => {});
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
    // Before the early return: "this page's player is gone" is true whether
    // or not one was mounted, and it is what stands a queued PUT retry
    // down (persistTrackChoice). The same goes for the picker's 'async'
    // listener, which is wired by wireTrackHandlers rather than by the
    // mount and so outlives it.
    playerGeneration++;
    if (asyncSwapListener) {
        window.removeEventListener('async', asyncSwapListener);
        asyncSwapListener = null;
    }
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
