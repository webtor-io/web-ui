// A player that never starts, said out loud (owner, 2026-09-29).
//
// For an hour and a half on 29.09 Safari on a Mac got a dead player on every
// route: hls.js on a ManagedMediaSource swapped in its own blob: <source>,
// the player took it for a new stream and killed itself (stream-url.js). And
// nothing said so: stream-start needs 5 s of playback, and the passthrough
// guard watches a decoder (the old route died the same way; a fallback would
// not have helped). The owner found it by hand.
//
// So: playback was asked for, the element has not played yet (no `playing`
// on a playing element, the clock has not moved), and for
// DEAD_AFTER_MS nothing moved towards playback (why: quiet). Then one
// `player-dead` event with the state it died in. Moving, or waiting on the
// server, is:
//   - a playlist or a fragment request pending: the server is being asked;
//     and an answer, or bytes. Not the asking itself, nor an hls.js error:
//     on a lost transcoder session (a rollout) every request gets a 404 and
//     hls.js asks again for ever, which is no progress at all;
//   - a fragment in flight before its headers or receiving bytes
//     (loader-restart.js loadProgress/loadWork: a 4K segment at the viewer's
//     cap may take minutes and raises no event while it arrives);
//   - until the first fragment is in, a playlist reload: hls.js is waiting
//     for the transcoder's first segment (median click to first frame is
//     ~58 s, a third of it before the player). After that a reload that
//     brings segments nobody loads is not progress -- hls.js polls a live
//     playlist whatever the element does;
//   - the element's buffered ranges, its readyState, and its `progress`
//     (bytes in: native HLS and a direct file have nothing else). Not its
//     `emptied`/`loadstart`/`durationchange`: an hls.js recovery fires them
//     every time it detaches and re-attaches, and does nothing else.
// Two deaths are busy rather than quiet, and hls.js keeps loading through
// them (measured by the AAC 5.1 session, 2026-09-29, headless Chrome 154 +
// hls.js 1.6.14):
//   - why: error -- the element holds a MediaError for DEAD_AFTER_MS on end.
//     A SourceBuffer append failure on the production master shape arrives
//     as a non-fatal bufferAppendError with the MediaSource still open;
//     hls.js does not recover it and the element sits on MediaError 4.
//     Any recovery resets the element (load(): `emptied`), and the clock;
//   - why: recovering -- RECOVERY_STORM re-attachments (`emptied`) since the
//     request and DEAD_AFTER_MS without a start: on a muxed TS shape hls.js
//     recovers ~1000 times a second, and the passthrough guard never gives up.
// Asked for is a `play` (the viewer, or autoplay) -- or, on a player with
// `autoplay` in its markup, the mount itself: autoplay fires `play` only
// once the element has data, together with `playing`, so a player that
// never gets any would never be watched, and a MediaError before any press
// makes play() reject without a `play`. That arming lets go as soon as the
// element has data and stays paused (autoplay refused, the resume prompt's
// hold): the viewer's Play arms it again.
// Not a fallback: a timer cannot tell a dead player from a slow one well
// enough to restart anything. Where it was only slow after all -- it plays
// later -- `player-revived` says so, and their count is this rule's error.
//
// Not watched: a paused player (the resume prompt, the grace hold, the
// viewer), a hidden tab (throttled, may load nothing: the quiet counts from
// when it is visible again), a restart a guard has begun (passthrough.js:
// counted as hevc-/audio-fallback), an element taken off the page, and
// everything after the first start (stall-watch.js, loader-restart.js). No
// timer outlives the report: the revival is heard from the element.

import { bufferedSig, loadProgress, loadWork } from './loader-restart.js';
import { startAudioClass } from './passthrough.js';

export const DEAD_EVENT = 'player-dead';
export const REVIVED_EVENT = 'player-revived';
export const DEAD_AFTER_MS = 30000;
export const CHECK_EVERY_MS = 2000;
// The clock this far past where the request found it: playing, `playing` seen
// or not (it may have gone by before the watch began).
export const STARTED_ADVANCE_S = 0.5;
// Re-attachments since the play request that make a storm. One is a load()
// or a recovery that may work; five without a start is a loop.
export const RECOVERY_STORM = 5;

// HTMLMediaElement.NETWORK_LOADING and HAVE_METADATA, spelled out for
// plain-object tests.
const NETWORK_LOADING = 2;
const HAVE_METADATA = 1;

const ELEMENT_ACTIVITY = ['progress'];

const safe = (fn, dflt) => {
    try { return fn(); } catch (e) { return dflt; }
};

// sourceKind: what the element is playing from, without the URL itself --
// hls.js ('hlsjs'), a blob: URL hls.js is not attached to ('blob': the
// 29.09 death), an m3u8 the browser plays itself ('native'), a file
// ('direct'), nothing ('none').
export function sourceKind(video, hlsOn) {
    if (hlsOn) return 'hlsjs';
    const src = safe(() => video.currentSrc || video.getAttribute('src') || '', '');
    if (!src) return 'none';
    if (src.startsWith('blob:')) return 'blob';
    return src.includes('.m3u8') || src.includes('mpegurl') ? 'native' : 'direct';
}

// createDeadPlayerWatch watches one player element until its first start.
// getHls returns the hls.js instance the player holds (or null); Hls is the
// hls.js class, for its event names; handled() is true once a guard has
// begun a restart. Returns { dispose }.
export function createDeadPlayerWatch({ video, getHls = () => null, Hls = null, handled = () => false,
    track, doc = document, now = () => Date.now(),
    setInterval: startTimer = (fn, ms) => setInterval(fn, ms),
    clearInterval: stopTimer = (id) => clearInterval(id),
    deadAfterMs = DEAD_AFTER_MS, checkEveryMs = CHECK_EVERY_MS }) {
    const E = (Hls && Hls.Events) || {};
    let armedAt = null;
    // What armed the watch: 'play', or 'autoplay' (the markup, at mount).
    let armedBy = null;
    let startAt = 0;
    let quietSince = 0;
    let timer = null;
    let reportedAt = null;
    // It played after the report: the rule's error (player-revived).
    let revived = false;
    let stopped = false;
    let sig = '';
    let rs = -1;
    let loads = new Map();
    let bound = null;
    // Requests hls.js has out: the manifest, the video and the audio playlist.
    const waiting = { manifest: false, level: false, audio: false };
    let fragIn = false;
    // The element's MediaError since (a check saw it; a reset ends it), and
    // the re-attachments since the play request.
    let errorSince = null;
    let recoveries = 0;

    const touch = () => { quietSince = now(); };
    // A playlist request or answer is progress until the first fragment is
    // in; after that only for the manifest (a new start: a session seek).
    const counts = (k) => k === 'manifest' || !fragIn;
    const ask = (k) => () => {
        if (k === 'manifest') fragIn = false;
        waiting[k] = true;
    };
    const answer = (k) => () => {
        waiting[k] = false;
        if (counts(k)) touch();
    };
    // A failed playlist request has had its answer: hls.js retries (a new
    // request, which counts), or gives up. Which one, by the error's details
    // (manifestLoadError, levelLoadTimeOut, audioTrackLoadError, ...).
    const onError = (ev, data) => {
        const d = String((data && data.details) || '');
        if (d.startsWith('manifest')) waiting.manifest = false;
        else if (d.startsWith('level')) waiting.level = false;
        else if (d.startsWith('audioTrack')) waiting.audio = false;
    };
    const onFrag = () => { fragIn = true; touch(); };
    const onServer = () => waiting.manifest || ((waiting.level || waiting.audio) && !fragIn);
    const listeners = [
        [E.MANIFEST_LOADING, ask('manifest')], [E.MANIFEST_LOADED, answer('manifest')],
        [E.LEVEL_LOADING, ask('level')], [E.LEVEL_LOADED, answer('level')],
        [E.AUDIO_TRACK_LOADING, ask('audio')], [E.AUDIO_TRACK_LOADED, answer('audio')],
        [E.FRAG_LOADED, onFrag], [E.FRAG_BUFFERED, onFrag],
        [E.ERROR, onError],
    ].filter(([ev]) => ev);
    const bind = (hls) => {
        if (hls === bound) return;
        if (bound && typeof bound.off === 'function') for (const [ev, fn] of listeners) bound.off(ev, fn);
        bound = hls || null;
        waiting.manifest = false;
        waiting.level = false;
        waiting.audio = false;
        fragIn = false;
        loads = new Map();
        if (bound && typeof bound.on === 'function') for (const [ev, fn] of listeners) bound.on(ev, fn);
        touch();
    };

    const disarm = () => {
        if (timer !== null) { stopTimer(timer); timer = null; }
        armedAt = null;
        armedBy = null;
    };
    const dispose = () => {
        if (stopped) return;
        stopped = true;
        disarm();
        bind(null);
        video.removeEventListener('play', onPlay);
        video.removeEventListener('pause', onPause);
        video.removeEventListener('playing', onPlaying);
        video.removeEventListener('seeking', onSeeking);
        video.removeEventListener('timeupdate', onTime);
        video.removeEventListener('emptied', onEmptied);
        for (const ev of ELEMENT_ACTIVITY) video.removeEventListener(ev, touch);
        doc.removeEventListener('visibilitychange', touch);
    };

    const state = (t, why) => {
        const hls = safe(getHls, null) || null;
        const on = !!(hls && hls.media === video);
        const err = safe(() => (video.error ? video.error.code : 0), 0);
        return {
            why,
            // What armed the watch, and whether the element was paused: an
            // autoplay the browser refused is not a dead player.
            by: armedBy || '',
            paused: safe(() => video.paused, false) === true,
            path: sourceKind(video, on),
            hls: on ? 'on' : hls ? 'off' : 'none',
            route: safe(() => (video.dataset && video.dataset.videoRoute) || '', ''),
            // The start's audio class (passthrough.js startAudioClass: none,
            // aac51, dolby), as on stream-start and both fallbacks. A
            // multichannel start that dies with no error and no fallback --
            // native HLS on iOS refused a PQ variant that way -- has no
            // other event to be counted per class by.
            audio: safe(() => startAudioClass(video), 'none'),
            err,
            rs: safe(() => video.readyState, -1),
            ns: safe(() => video.networkState, -1),
            inflight: on ? loads.size : 0,
            got: fragIn ? 'frags' : bound ? 'playlist' : 'none',
            recoveries,
            waited_s: Math.round((t - (armedAt === null ? t : armedAt)) / 1000),
        };
    };

    const advanced = () => safe(() => video.currentTime - startAt, 0) > STARTED_ADVANCE_S;
    const started = () => {
        if (reportedAt !== null) {
            revived = true;
            const t = now();
            const s = state(t);
            track(REVIVED_EVENT, { path: s.path, route: s.route, audio: s.audio, waited_s: s.waited_s, recoveries });
        }
        dispose();
    };

    const check = () => {
        // Reported: the timer is stopped below, but not every timer honours
        // a clearInterval from inside its own callback (Node's mocked ones
        // run it again) -- one report per player whatever the timer does.
        if (stopped || armedAt === null || reportedAt !== null) return;
        // Off the page: the player is gone (a test's page replaced).
        if (safe(handled, false) || video.isConnected === false) { dispose(); return; }
        const t = now();
        if (advanced()) { started(); return; }
        if (doc.hidden) { quietSince = t; return; }
        // Autoplay's arming: data in (metadata and on) and still paused is
        // autoplay refused or held, not a dead player. From metadata, not
        // from enough to play: an iPhone refuses autoplay with sound, and its
        // ManagedMediaSource then stops streaming at HAVE_METADATA (30 of 49
        // player-dead of 2026-09-30's first 12 h were iOS, most paused at
        // readyState 1). A Play arms it again.
        if (armedBy === 'autoplay' && safe(() => video.paused, true)
            && safe(() => video.readyState, 0) >= HAVE_METADATA) {
            disarm();
            return;
        }
        if (safe(() => !!video.error, false)) {
            if (errorSince === null) errorSince = t;
        } else {
            errorSince = null;
        }
        const hls = safe(getHls, null) || null;
        bind(hls);
        const on = !!(hls && hls.media === video);
        const s = bufferedSig(video);
        const r = safe(() => video.readyState, -1);
        const cur = on ? loadProgress(safe(() => hls.inFlightFragments, {})) : new Map();
        const work = on ? loadWork(loads, cur) : 'none';
        loads = cur;
        // Without hls.js the element is all there is: loading, and no error.
        const elementLoading = !on && safe(() => video.networkState === NETWORK_LOADING && !video.error, false);
        if (s !== sig || r !== rs || work === 'busy' || (on && onServer()) || elementLoading) {
            sig = s;
            rs = r;
            quietSince = t;
        }
        let why = null;
        if (errorSince !== null && t - errorSince >= deadAfterMs) why = 'error';
        else if (recoveries >= RECOVERY_STORM && t - armedAt >= deadAfterMs) why = 'recovering';
        else if (t - quietSince >= deadAfterMs) why = 'quiet';
        if (why === null) return;
        reportedAt = t;
        // No timer from here: a `playing`, or the clock moving (onTime),
        // after this is the rule's error, counted.
        if (timer !== null) { stopTimer(timer); timer = null; }
        track(DEAD_EVENT, state(t, why));
    };

    const arm = (by) => {
        armedBy = by;
        armedAt = now();
        startAt = safe(() => video.currentTime, 0) || 0;
        sig = bufferedSig(video);
        rs = safe(() => video.readyState, -1);
        quietSince = armedAt;
        // hls.js attaching at mount resets the element too: not a recovery.
        recoveries = 0;
        timer = startTimer(check, checkEveryMs);
    };
    function onPlay() {
        if (stopped || armedAt !== null || reportedAt !== null) return;
        arm('play');
    }
    function onPause() {
        // Not a pause the element takes with an error: Chrome, an append
        // failure before metadata, sets the error and pauses in the same
        // moment (the AAC 5.1 session's benches, 4 of 4) -- nobody chose it,
        // and the player is dead.
        if (reportedAt === null && !safe(() => !!video.error, false)) disarm();
    }
    function onPlaying() {
        // Autoplay fires `play` and `playing` together; a hold that pauses
        // on `play` (the resume prompt) leaves a `playing` on a paused
        // element that has not started anything.
        if (safe(() => video.paused, false)) return;
        started();
    }
    function onTime() {
        if (armedAt !== null && advanced()) started();
    }
    // A reset: the element's error, if any, is gone (load()), and one more
    // re-attachment -- counted from the play request (onPlay).
    function onEmptied() {
        errorSince = null;
        recoveries++;
    }
    // A seek before the start moves the clock without playing: measured
    // from where it landed.
    function onSeeking() {
        startAt = safe(() => video.currentTime, startAt) || 0;
        touch();
    }

    video.addEventListener('play', onPlay);
    video.addEventListener('pause', onPause);
    video.addEventListener('playing', onPlaying);
    video.addEventListener('seeking', onSeeking);
    video.addEventListener('timeupdate', onTime);
    video.addEventListener('emptied', onEmptied);
    for (const ev of ELEMENT_ACTIVITY) video.addEventListener(ev, touch);
    doc.addEventListener('visibilitychange', touch);
    // Autoplay may have asked before the player mounted; or it is in the
    // markup and will ask once there is data.
    if (!safe(() => video.paused, true)) onPlay();
    else if (safe(() => video.autoplay, false) === true) arm('autoplay');

    // dead: reported and not revived -- the player is this watch's verdict
    // (the stream restart, stream-restart.js, stands back while it is).
    return {
        dispose,
        get reported() { return reportedAt !== null; },
        get dead() { return reportedAt !== null && !revived; },
    };
}
