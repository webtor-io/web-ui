// The AI subtitle translation as the player runs it: the progress poll and
// the <track> revisions it swaps in, the catch-up pill, the hold (after a
// seek, after a start, before a line the translation has not reached), the
// brake and the rewind, and the three handlers of the pill.
//
// Moved out of Player.jsx on 2026-09-19, unchanged in behaviour. It is one
// hook rather than several because its parts share a dozen refs that are the
// state machine; it is called where the code used to stand, so its effects
// keep their place in the component's effect order. `late` is a ref the
// component fills further down with what is declared after the call -- the
// hls.js instance and the on-screen offer's two operations -- and is only
// ever read when something happens, never during render.

import { useRef, useState, useEffect, useCallback } from 'preact/hooks';
import { applyCueOffset } from '../cue-offset';
import { applySubtitleSelection, readSelection, selectionHolds } from '../subtitle-apply.js';
import { reloadSubtitleTrack } from '../subtitle-track-reload.js';
import { readAllTracks, readTracks } from '../subtitle-telemetry.js';
import { pickDefaultSubtitle, translationAction, hasSavedDefault } from '../subtitle-rules.js';
import { pollProgress, progressText, withRev } from '../subtitle-progress.js';
import { catchUpTiming, caughtUp, needsReload, nothingCountedYet, resumeRewind, shouldBrake, trailing, viewerFrontier } from '../subtitle-catchup.js';
import {
    restoreSavedTranslation,
} from '../track-picker.js';
import {
    activateSubtitle,
    findSubtitleItem,
    findSubtitlesModal,
    itemData,
} from '../track-dialog.js';
import { tf } from '../i18n';

// TRACK_RELOAD_INTERVAL_MS throttles the <track> src swaps. Every swap
// refetches the whole partial VTT and leaves the track without cues
// while the browser reparses it, so reloading on each 3 s poll would
// blank the subtitles five times a minute. The percentage keeps updating
// on every poll; only the text catches up in steps.
const TRACK_RELOAD_INTERVAL_MS = 15000;

// How often a DIRECT seek may kick the translation poll (a session seek is
// rationed by its POST). A held arrow key seeks once per key-repeat.
const DIRECT_SEEK_KICK_MS = 500;
export function useSubtitleTranslation({ videoRef, videoEl, trackContainer, trackHooks, seekOffset, seekOffsetRef, sessionSeekingRef, late }) {
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
    // `null`, `{ waiting }` or `{ waiting: false, caughtUp: true }`. `waiting` is the viewer having
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
    // Whether the current wait was started by the player (a seek, a start,
    // a line the translation has not reached) rather than by the Wait button.
    const autoWaitRef = useRef(false);
    // The frontier when the current wait began, in movie time: the first
    // line the viewer had no subtitle for. finishWait rewinds to it.
    const waitFromRef = useRef(null);
    // The viewer ended a wait themselves (Keep watching, the play button):
    // for the rest of this stretch of untranslated film the player does not
    // pause again -- a hold that fights the viewer is worse than missing
    // subtitles they chose to miss. Cleared with the stretch, like
    // dismissedRef.
    const overruledRef = useRef(false);
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
    // film, so it opens the same hold window.
    const startHoldRef = useRef(false);
    // The silent hold (owner, 2026-09-18): a seek's hold was decided by an
    // answer that arrives after the seek has settled, so the film played
    // for a moment and was paused again. Now the film is paused the instant
    // the seek settles, with nothing on screen, and the first answer about
    // the new run either turns that into the ordinary hold (banner) or
    // lets the film go. Bounded by catchUpTiming.seekPreHoldMaxMs.
    const preHoldRef = useRef(false);
    const preHoldTimerRef = useRef(null);
    // The same fact for the render: while it lasts the picture is a seek
    // that has not finished (spinner), not a paused film (big play button).
    const [preHolding, setPreHolding] = useState(false);
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
    const showCatchUp = useCallback((next, hard) => {
        const cur = catchUpRef.current;
        if (cur === next) return;
        // The "caught up" flash owns the slot for its few seconds: the tick
        // that follows it says "nothing to show", and must not cut it short.
        // Anything that has something to say does -- and so does a clear
        // that is not a tick's (`hard`: the track was switched off, the run
        // is gone, the viewer closed the pill): "caught up" over a
        // translation that is no longer there is not true.
        if (cur && cur.caughtUp && !next && !hard) return;
        if (cur && next && cur.waiting === next.waiting
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
        showCatchUp({ waiting: false, caughtUp: true });
        // A second flash inside the first one's few seconds is "no change"
        // to showCatchUp, which then leaves the first timer alone -- and it
        // would take the pill down early.
        if (caughtUpTimerRef.current) clearTimeout(caughtUpTimerRef.current);
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

    // The banner says no number, on any source. On a file source total is
    // the whole film, so total - done is off by orders of magnitude exactly
    // where the viewer decides whether to wait. On a live one (measured
    // 2026-09-18 on a cold seek) the transcoder's subtitle playlist grows by
    // 20-25 s of film per second of wall time, so within ten seconds `total`
    // is hundreds of cues that lie minutes ahead of the viewer: "~300 cues
    // to go" is true of the document and read as "this will never catch up"
    // (owner). The service reports no count of cues between the viewer and
    // the frontier, so none is shown.

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
    // through it.
    //
    // It reports whether the wait it dropped was a seek's hold. That pause
    // is one the viewer never made, so a caller ending it for a reason that
    // is not the viewer's (the run died, another track, ×) plays the film
    // again; the Wait button's pause is the viewer's and stays.
    const clearWait = useCallback(() => {
        const wasAuto = waitingRef.current && autoWaitRef.current;
        waitingRef.current = false;
        autoWaitRef.current = false;
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
    // got ahead. It is the only such ending -- a seek's hold used to give
    // up after 20 s and play the film without subtitles, which is the one
    // outcome the hold exists to prevent (owner, 2026-09-18). A viewer who
    // would rather watch has Keep watching, x and the play button; a run
    // that dies ends the wait through its own error path.
    const finishWait = useCallback(() => {
        const auto = autoWaitRef.current;
        const seconds = Math.round((Date.now() - waitedSinceRef.current) / 100) / 10;
        // Back to the first line the viewer had no subtitle for (nothing,
        // if the wait began before it -- see resumeRewind). Inside the
        // playlist that is playing: currentTime, not a session seek, which
        // would restart the transcoder for a few seconds of film.
        const video = videoRef.current;
        const rewind = video ? resumeRewind(waitFromRef.current, (video.currentTime || 0) + seekOffsetRef.current) : 0;
        if (rewind > 0) video.currentTime = Math.max(0, (video.currentTime || 0) - rewind);
        clearWait();
        trailingRef.current = false;
        showCatchUp(null);
        if (window.umami) window.umami.track('subtitle-translate-wait-done', {
            lang: catchUpLangRef.current,
            seconds,
            auto,
            rewind: Math.round(rewind * 10) / 10,
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
    // seek started, told apart because its pause is not the viewer's.
    const beginWait = useCallback((auto) => {
        const video = videoRef.current;
        const playhead = ((video && video.currentTime) || 0) + seekOffsetRef.current;
        const pendingFrom = pendingFromRef.current;
        clearWait();
        // Set before pause(), because the `pause` event is what reaches
        // sleep() and sleep() reads this to decide not to suspend.
        waitingRef.current = true;
        autoWaitRef.current = auto;
        waitedSinceRef.current = Date.now();
        waitFromRef.current = pendingFrom === null || pendingFrom === undefined ? null : pendingFrom;
        dismissedRef.current = false;
        if (video && typeof video.pause === 'function') video.pause();
        const poll = pollStopRef.current;
        // A no-op unless the run is suspended, which is exactly the case
        // it is here for: a viewer who paused first and pressed Wait
        // afterwards has a sleeping poll to wake.
        if (poll && poll.resume) poll.resume();
        showCatchUp({ waiting: true });
        if (window.umami) window.umami.track('subtitle-translate-wait', {
            lang: catchUpLangRef.current,
            behind: pendingFrom === null || pendingFrom === undefined ? 0 : Math.round(playhead - pendingFrom),
            auto,
        });
    }, [clearWait, showCatchUp]);

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
        overruledRef.current = false;
        showCatchUp(null, true);
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
            showCatchUp(null, true);
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
            return last.__absEnd !== undefined ? last.__absEnd : (last.endTime || 0);
        };
        // Once a revision has settled, the picker's current answer is
        // written again: the module no longer restores a mode of its own
        // (see its header), and a <track> load wakes none of the hls.js
        // events the re-assertion effect listens to.
        const reassertSelection = () => {
            if (runID !== runSeqRef.current) return;
            // Cues put back by the reload (an empty or failed revision
            // restores the old objects) carry the shift of the run they were
            // loaded in. After a seek that is another run's offset.
            if (videoRef.current && videoRef.current.dataset.sessionId) {
                const el = Array.from(videoRef.current.querySelectorAll('track')).find((t) => t.id === id);
                if (el && el.track) applyCueOffset(el.track, seekOffsetRef.current);
            }
            const selection = readSelection(trackContainer || document);
            if (!selection) return;
            const video = videoRef.current;
            const hls = (late.current.hlsRef && late.current.hlsRef.current) || window.hlsPlayer || null;
            if (selectionHolds(video, hls, selection)) return;
            applySubtitleSelection(video, hls, selection);
        };
        const reload = (done, force, frontier = null) => {
            const now = Date.now();
            if (!force && now - lastReloadAt < TRACK_RELOAD_INTERVAL_MS) return;
            // Only a reload that actually swapped the src spends the
            // throttle window: stamping on a no-op (same revision, or no
            // <track> element yet) would hold off the next real one for
            // another 15 s.
            if (reloadSubtitleTrack(videoRef.current, id, withRev(src, done), onTrackError, reassertSelection)) {
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
                // edge never moves and the wait never finishes. So is the
                // silent pre-hold after a seek: skipped here, the stale edge
                // reads as "behind", the hold becomes a visible wait, and only
                // the next tick swaps and ends it -- a banner and a "caught
                // up" flash over cues the service already had.
                if ((!video.paused || waitingRef.current || preHoldRef.current) && needsReload({ serviceDone: p.done, loaded, frontier: pendingFromRef.current, playhead })) {
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
                        beginWait(true);
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
                        showCatchUp({ waiting: true });
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
                    finishWait();
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
                    // Behind the viewer, mid-film: the same hold a seek gets
                    // (owner, 2026-09-18) -- unless the viewer has already
                    // said they would rather watch, and then the pill only
                    // offers.
                    // Nor mid-seek: the run being left is not worth a hold,
                    // and the one being started gets its own when it settles.
                    if (overruledRef.current || sessionSeekingRef.current) showCatchUp({ waiting: false });
                    else beginWait(true);
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
                if (!isTrailing) {
                    dismissedRef.current = false;
                    overruledRef.current = false;
                }
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
            // Play pressed over a wait is the viewer overruling it.
            if (waitingRef.current) overruledRef.current = true;
            clearWait();
            endPreHold(false);
            // Play IS "Keep watching": whichever of the two the viewer
            // presses, the banner must stop saying the film is paused for
            // them now, not on the next 3 s tick (owner, 2026-09-18).
            const banner = catchUpRef.current;
            if (banner && banner.waiting) showCatchUp({ waiting: false });
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
        // The brake: stop a second before the first line the translation
        // has not reached, rather than on the tick after it (shouldBrake).
        // The kick is what keeps a stale frontier cheap -- an answer that is
        // ahead ends the wait one HEAD later.
        const brake = () => {
            if (!pollStopRef.current || video.paused || document.hidden) return;
            if (waitingRef.current || preHoldRef.current || sessionSeekingRef.current) return;
            if (dismissedRef.current || overruledRef.current) return;
            const playhead = (video.currentTime || 0) + seekOffsetRef.current;
            if (!shouldBrake(pendingFromRef.current, playhead)) return;
            beginWait(true);
            const poll = pollStopRef.current;
            if (poll && poll.kick) poll.kick();
        };
        video.addEventListener('pause', sleep);
        video.addEventListener('play', onPlay);
        video.addEventListener('timeupdate', brake);
        document.addEventListener('visibilitychange', onVisibility);
        return () => {
            video.removeEventListener('pause', sleep);
            video.removeEventListener('play', onPlay);
            video.removeEventListener('timeupdate', brake);
            document.removeEventListener('visibilitychange', onVisibility);
            // Every one of these ends in play() or a setState, and none of
            // them has anything left to act on once the player is gone.
            for (const ref of [preHoldTimerRef, caughtUpTimerRef, holdWatchRef]) {
                if (ref.current) clearTimeout(ref.current);
                ref.current = null;
            }
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
            late.current.withdrawOffer();
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
        // The dialog was re-rendered for another preferred language
        // (swapSubtitlesDialog). What plays now is the server's default, not
        // a press of the viewer's: the translation poll follows it, "manual"
        // is whatever the new render says was saved, and the on-screen offer
        // is decided again -- for the language the viewer has just asked
        // for, which is the moment it is most likely wanted.
        trackHooks.onDialogSwapped = (el) => {
            const modal = findSubtitlesModal(trackContainer);
            manualSubtitleRef.current = !!modal && hasSavedDefault(readAllTracks(modal));
            late.current.withdrawOffer();
            const action = translationActionFor(el);
            if (itemData(el).id !== pollingIdRef.current) stopTranslationProgress();
            if (action !== 'none') startTranslationProgress(el, action === 'resume');
            late.current.redecideOffer();
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
            trackHooks.onDialogSwapped = null;
            stopTranslationProgress({ resumeHold: false });
        };
    }, [trackHooks, trackContainer, startTranslationProgress, stopTranslationProgress, translationActionFor]);

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


    // onSeekSettled: a session seek has landed (the new run is `playing`).
    // The component calls it from its seeking callback.
    const onSeekSettled = useCallback(() => {
        if (!seekHoldPendingRef.current) return;
        // The hold window starts here, with the new run playing -- and the
        // film stands still until the first answer about it (beginPreHold),
        // so a hold never has to interrupt a film that has just started.
        seekSettledAtRef.current = Date.now();
        beginPreHold();
        const poll = pollStopRef.current;
        if (poll && poll.kick) poll.kick();
    }, [beginPreHold]);

    // onDirectSeek: the same seek treatment a session gets, minus the
    // transcoder round-trip: the poll is kicked with the new position and the
    // hold window opens now -- a direct seek has no `playing` settle to wait
    // for. Unlike a session seek, nothing rations this path (no POST, no
    // sessionSeeking guard), so the kick itself is: a held arrow key repeats
    // the seek ~30 times a second, and each kick is an immediate HEAD.
    const onDirectSeek = useCallback(() => {
        const now = Date.now();
        if (now - directKickAtRef.current >= DIRECT_SEEK_KICK_MS) {
            directKickAtRef.current = now;
            kickTranslationPoll();
        }
        seekSettledAtRef.current = now;
        beginPreHold();
    }, [kickTranslationPoll, beginPreHold]);

    // cancelPreHold: the viewer's toggle during the silent hold means
    // "pause", not "play". It ends the hold without playing, withdraws the
    // hold question for this seek, and reports that it did.
    const cancelPreHold = useCallback(() => {
        if (!preHoldRef.current) return false;
        endPreHold(false);
        seekHoldPendingRef.current = false;
        return true;
    }, [endPreHold]);

    // Wait: pause the film but keep the poll awake. Both halves are
    // needed — the pause is what the viewer asked for, and the poll is
    // what the HEAD every 3 s keeps alive on the service (and, for a live
    // source, the transcoder session the translation is reading).
    const handleWait = useCallback(() => {
        beginWait(false);
    }, [beginWait]);

    // Keep watching: the viewer overrules the wait. The banner is not
    // rewritten here — what it should say next is the next tick's answer,
    // and the run may well still be behind.
    const handleKeepWatching = useCallback(() => {
        overruledRef.current = true;
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
        showCatchUp(null, true);
    }, [clearWait, showCatchUp, resumePlayback]);

    return {
        catchUp, preHolding, manualSubtitleRef,
        kickTranslationPoll, onSeekSettled, onDirectSeek, cancelPreHold,
        handleWait, handleKeepWatching, handleDismissCatchUp,
    };
}
