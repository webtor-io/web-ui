// What the page's players are doing, as far as the transfer status needs to
// know (lib/transferStatus.js present): at the plan's cap, a video that is
// buffering is sold the way out, one that plays smoothly is sold it only when
// its file needs more than the cap (overCap: it will stall once its buffer
// runs out), and bytes with no player on the page are a download.
//
// Not coupled to the player: media events do not bubble, but they do pass
// through the document on their way down, so one capture listener there
// hears every <video> and <audio> the job renders into the page, whoever
// built it (Player.jsx, the audio player, a future one).
//
//   'buffering' -- a player stalled for real (below) in the last
//                  STALL_WINDOW_MS, or is stalled right now. A minute, not
//                  the moment: the box would blink with every stall-and-
//                  resume otherwise.
//   'playing'   -- a player is playing, or was within ACTIVE_WINDOW_MS (a
//                  pause to fetch a drink does not turn the bytes it keeps
//                  buffering into a "download"), or is paused and still
//                  filling its buffer (hls.js buffers far ahead of a paused
//                  video, at the cap, for longer than that minute), or is
//                  held paused by the grace popup until the viewer answers
//                  it (viewerPaused: the page's pause, not theirs).
//   'none'      -- no player, an ended one, or one left alone for a minute.
//
// Apart from the verdict: streaming() -- a player is playing or waiting for
// data right now, or paused and still filling its buffer, or held by the
// grace popup. The transfer
// status keeps the viewer on its chain while it is (lib/transferStatus.js
// playing): HLS closes its request between two segments and the proxy then
// honestly counts none. No minute after a pause there: the minute is the
// plan box's question (what a cap means to them), not whether they are
// taking part. And offerAnswered() -- the viewer answered an offer about the
// cap (the grace popup, or "watch as is" on the slow-download modal before
// playback) and the player has not stalled for real since: the plan box's
// question again (they were just told of the cap; the next offer waits until
// they hit it).
//
// What is a stall. `waiting` is also what every (re)start of a source says:
// the first play() of a stream, and a session seek, which does not set
// `seeking` -- it reloads the source (player/session-seek.js: hls.js
// loadSource detaches and re-attaches the element, the native path sets src
// and calls load()), so the element is emptied and play() waits for its
// first data. None of that is the cap. So a `waiting` counts only once the
// element has played since its source (re)started, never while it is
// seeking, and only if it lasts STALL_MIN_MS: a hiccup the viewer never
// notices sells nothing. `stalled` counts only while the element is
// actually short of data -- under Media Source Extensions (hls.js) browsers
// fire it while a healthy buffer plays on.
//
// What ends a stall: `playing` (the element has data again), a seek, a
// pause, the end -- and a `timeupdate` only once currentTime has moved
// RESUME_MIN_S past where the element waited. Chrome's MSE element that runs
// dry fires `waiting` and then one more periodic `timeupdate` about 250 ms
// later with currentTime unchanged (readyState 2), and nothing after it until
// the data comes. Taken as playback, that tick closed every stall at ~255 ms,
// under STALL_MIN_MS, and a player frozen at the plan's cap read 'playing'
// through all of it: 30 real stalls of 2.5-215 s in two recorded runs
// (Chrome 154, transcoder HLS at a 5 Mbps cap, 2026-09-26), every one's
// `waiting` followed by that tick, not one read as buffering -- the owner's
// "no upsell while the video really stalls" (__fixtures__/
// player-stall-trace.json, playerActivity.trace.test.js).

export const STALL_WINDOW_MS = 60 * 1000;
export const ACTIVE_WINDOW_MS = 60 * 1000;
export const STALL_MIN_MS = 1500;
// A paused player whose buffer grew this recently is still streaming.
export const BUFFER_IDLE_MS = 10 * 1000;
// How far currentTime must move past a stall's position before a
// `timeupdate` counts as playback again. A played tick at 1x moves it by
// ~0.25 s (timeupdate fires every 250 ms or so); a tick that did not move it
// is the element reporting the stall, not leaving it.
export const RESUME_MIN_S = 0.1;

// HTMLMediaElement.HAVE_FUTURE_DATA, spelled out for plain-object tests.
const HAVE_FUTURE_DATA = 3;

// The expando on a player element whose answer a real stall has spent
// (offerAnswered). A string, not a Symbol: a second copy of this module
// (CLAUDE.md, shared JS state) must read the same one.
const GRACE_SPENT = '_txGraceCapHit';

// answered: the viewer has answered an offer about the cap for this player
// element -- the grace popup ("continue at N Mbps" or its close,
// data-grace-cta-answered, set by Player.jsx) or the slow-download modal's
// "watch as is" before playback (data-offer-answered, rendered by the stream
// job for a force-slow run, StreamContent.StatusAnswered).
const answered = (m) => !!m && !!m.dataset && ('graceCtaAnswered' in m.dataset || 'offerAnswered' in m.dataset);

const isMedia = (el) => !!el && (el.tagName === 'VIDEO' || el.tagName === 'AUDIO');

// Paused by the viewer. A player the page itself holds paused -- the grace
// popup, until the viewer answers it (player/grace-hold.js marks the element
// data-grace-cta-hold) -- is not: the viewer is reading the popup, hls.js
// keeps filling the buffer at the cap, and the film goes on with the answer.
// Read as playing, as it read while the film played on under the popup:
// the viewer stays on the chain (streaming), the verdict keeps no minute.
const viewerPaused = (m) => m.paused && !(m.dataset && 'graceCtaHold' in m.dataset);

// playerState is the verdict from the players on the page and the marks the
// listener keeps. Pure, for the tests.
//   lastStallAt   a stall that lasted STALL_MIN_MS ended (or was seen) then
//   stallingSince a stall under way since then, null for none
//   lastActiveAt  a player last played then
//   lastBufferAt  a paused player's buffer last grew then
export function playerState(media, {
    lastStallAt = -Infinity, lastActiveAt = -Infinity, stallingSince = null, lastBufferAt = -Infinity,
} = {}, now = Date.now()) {
    const live = Array.from(media || []).filter((m) => !m.ended);
    if (!live.length) return 'none';
    const playing = live.some((m) => !viewerPaused(m));
    if (!playing && now - lastActiveAt >= ACTIVE_WINDOW_MS && now - lastBufferAt >= BUFFER_IDLE_MS) return 'none';
    const stalling = stallingSince !== null && now - stallingSince >= STALL_MIN_MS;
    if (stalling || now - lastStallAt < STALL_WINDOW_MS) return 'buffering';
    return 'playing';
}

// playerStreaming: a player on the page is streaming now -- not paused by the
// viewer (so playing, waiting for data, or held by the grace popup) and not
// ended, or paused and its buffer grew within BUFFER_IDLE_MS (hls.js fetching
// ahead). Pure, for the tests.
export function playerStreaming(media, { lastBufferAt = -Infinity } = {}, now = Date.now()) {
    const live = Array.from(media || []).filter((m) => !m.ended);
    if (live.some((m) => !viewerPaused(m))) return true;
    return live.length > 0 && now - lastBufferAt < BUFFER_IDLE_MS;
}

function bufferedEnd(m) {
    try {
        const b = m.buffered;
        return b && b.length ? b.end(b.length - 1) : 0;
    } catch (e) {
        return 0;
    }
}

// createPlayerActivity listens on the document. onChange(state) is called
// when the verdict or streaming() changes on an event; one that changes with
// time alone (a stall reaching STALL_MIN_MS, the minute running out, a paused
// buffer that stopped growing) is read by whoever asks -- the status view
// asks every second while a plan box, or the view its player keeps, can be
// up.
export function createPlayerActivity(doc = document, { now = () => Date.now(), onChange = null } = {}) {
    const marks = { lastStallAt: -Infinity, lastActiveAt: -Infinity, stallingSince: null, lastBufferAt: -Infinity };
    // Per element: `played` -- it has played since its source last
    // (re)started; `buf` -- its buffered end when last sampled; `stallAt` --
    // its currentTime when it last waited.
    const els = new WeakMap();
    const el = (m) => {
        let st = els.get(m);
        if (!st) {
            st = { played: false, buf: null, stallAt: 0 };
            els.set(m, st);
        }
        return st;
    };
    let stallEl = null;
    const media = () => doc.querySelectorAll('video, audio');
    // A paused player filling its buffer: sampled whenever the verdict is
    // asked for, since nothing else says so. While the element plays, its
    // buffer is only the baseline, followed on every sample (each media
    // event and each redraw), so a pause is compared with the buffer as it
    // was when it paused. Baselined only while paused, it compared with the
    // buffer before the element ever played -- or at its previous pause --
    // and every pause read as "still buffering" for BUFFER_IDLE_MS, the
    // viewer kept on the chain ten seconds after a pause with a full buffer.
    const sampleBuffers = (t) => {
        for (const m of media()) {
            if (m.ended) continue;
            const end = bufferedEnd(m);
            const st = el(m);
            if (m.paused && st.buf !== null && end > st.buf) marks.lastBufferAt = t;
            st.buf = end;
        }
    };
    // A stall whose element has left the document is over, counted as far
    // as it was seen (lastStallAt, set by the last state() while it lasted).
    // The events that end a stall never come for it: a removed element's
    // `pause` and `emptied` fire on a detached node and do not pass through
    // the document. Picking another file mid-stall (#content swapped, the
    // <video> gone before the player's teardown runs) and "Next" on a
    // prewarmed card (pause() and remove() in one task) both leave it so --
    // and the next file, playing smoothly, read 'buffering' for good: its
    // `playing` and `timeupdate` are not the stalled element's.
    const forgetGone = () => {
        if (!stallEl || stallEl.isConnected) return;
        marks.stallingSince = null;
        stallEl = null;
    };
    // A real stall counts (it lasted STALL_MIN_MS, at its end or while it
    // lasts). On an element whose grace popup the viewer has answered, it is
    // them hitting the cap they were told of -- a stall under way when they
    // answer included, from its next count: the video frozen at the cap then
    // is the limit, now. One that ended before the answer is not: it was the
    // popup's to speak of. Kept on the element (GRACE_SPENT), next to the
    // answer: not in this listener, which the status view makes anew when it
    // re-inits (a refused stream, app/resource/status.js renew) -- the
    // answer spent an hour ago would come back, and a file over the cap
    // would go quiet until its next stall. A session seek restarts the
    // source, not the answer: it stays spent.
    const counted = (t) => {
        marks.lastStallAt = t;
        if (answered(stallEl)) stallEl[GRACE_SPENT] = true;
    };
    // A stall that has lasted long enough counts from now on, and keeps
    // counting while it lasts.
    const countOngoing = (t) => {
        if (marks.stallingSince !== null && t - marks.stallingSince >= STALL_MIN_MS) counted(t);
    };
    const state = () => {
        const t = now();
        sampleBuffers(t);
        forgetGone();
        countOngoing(t);
        return playerState(media(), marks, t);
    };
    const streaming = () => {
        const t = now();
        sampleBuffers(t);
        return playerStreaming(media(), marks, t);
    };
    let last = state();
    let lastStreaming = streaming();
    const settle = () => {
        const s = state();
        const st = streaming();
        if (s === last && st === lastStreaming) return;
        last = s;
        lastStreaming = st;
        if (onChange) onChange(s);
    };
    // The stall under way ends: it counts if it lasted.
    const endStall = (t) => {
        if (marks.stallingSince === null) return;
        if (t - marks.stallingSince >= STALL_MIN_MS) counted(t);
        marks.stallingSince = null;
    };
    const onWaiting = (e) => {
        const m = e.target;
        const st = isMedia(m) ? el(m) : null;
        if (!st || m.seeking || !st.played) return;
        const t = now();
        if (marks.stallingSince === null) marks.stallingSince = t;
        st.stallAt = m.currentTime || 0;
        marks.lastActiveAt = t;
        stallEl = m;
        settle();
    };
    const onStalled = (e) => {
        const m = e.target;
        if (!isMedia(m) || m.seeking || m.paused || m.readyState >= HAVE_FUTURE_DATA) return;
        onWaiting(e);
    };
    // Playback: `playing`, or a `timeupdate` whose clock moved past the
    // stall (RESUME_MIN_S) -- the one Chrome sends right after `waiting`,
    // with the clock where it stopped, is not.
    const onActive = (e) => {
        const m = e.target;
        if (!isMedia(m) || m.paused) return;
        const t = now();
        const st = el(m);
        if (e.type === 'playing') st.played = true;
        const resumed = e.type === 'playing' || (m.currentTime || 0) - st.stallAt >= RESUME_MIN_S;
        if (m === stallEl && resumed) endStall(t);
        marks.lastActiveAt = t;
        settle();
    };
    // A new source (a session seek, the next episode): its first `waiting`
    // is the start, not a stall.
    const onRestart = (e) => {
        const m = e.target;
        if (!isMedia(m)) return;
        const st = el(m);
        st.played = false;
        st.buf = null;
        if (m === stallEl) marks.stallingSince = null;
        settle();
    };
    const onIdle = (e) => {
        const m = e.target;
        if (!isMedia(m)) return;
        // Paused or sought mid-stall: not a stall any more, and not one
        // that lasted unless it had.
        if (m === stallEl) endStall(now());
        settle();
    };
    const listeners = [
        ['waiting', onWaiting],
        ['stalled', onStalled],
        ['playing', onActive],
        ['timeupdate', onActive],
        ['emptied', onRestart],
        ['loadstart', onRestart],
        ['seeking', onIdle],
        ['pause', onIdle],
        ['ended', onIdle],
    ];
    for (const [name, fn] of listeners) doc.addEventListener(name, fn, true);

    const withData = (name) => {
        const own = stallEl && stallEl.isConnected && stallEl.dataset ? stallEl : null;
        if (own && name in own.dataset) return own;
        for (const m of media()) {
            if (m.dataset && name in m.dataset) return m;
        }
        return null;
    };
    // A player's free grace window (data-grace-duration-sec, set only where
    // grace applies), in seconds of film; 0 for none.
    const graceOf = (m) => {
        const g = m.dataset ? parseFloat(m.dataset.graceDurationSec) : NaN;
        return g > 0 && !m.ended ? g : 0;
    };
    // Its movie time: currentTime plus the offset its transcoder session
    // started at (data-run-offset, Player.jsx) -- after a session seek
    // currentTime counts from the seek point.
    const movieTime = (m) => (m.currentTime || 0) + (parseFloat(m.dataset.runOffset) || 0);

    return {
        state,
        streaming,
        // The stream job's line for the player that stalled (or any player
        // that has one): "…, and this file needs 8 Mbps".
        stallSub() {
            const m = withData('statusStallSub');
            return (m && m.dataset.statusStallSub) || '';
        },
        // The stream job marked the file as needing no more than the cap,
        // with a margin (data-status-fits-cap): it plays smoothly at the
        // cap, so nothing is said while it plays. Not a verdict on its
        // stalls -- one while the limiter binds is still the cap's.
        fitsCap() {
            return !!withData('statusFitsCap');
        },
        // The stream job marked the file as needing more than the cap
        // (data-status-over-cap: its bitrate is known and above it): at the
        // cap it will stall, however smoothly it plays from its buffer now.
        overCap() {
            return !!withData('statusOverCap');
        },
        // A player is inside its free grace window (data-grace-duration-sec,
        // set only where grace applies) by its movie time: currentTime plus
        // the offset its transcoder session started at (data-run-offset,
        // Player.jsx) -- after a session seek currentTime counts from the
        // seek point, and a player 24 minutes in read 6 s. The grace window is
        // not the plan's cap: nothing is sold inside it (transferStatus.js
        // present), though the proxy's verdict can be on there -- hls.js
        // fetches the segments after the window ahead of the playhead, at
        // the cap.
        inGrace() {
            for (const m of media()) {
                const g = graceOf(m);
                if (g && movieTime(m) < g) return true;
            }
            return false;
        },
        // A player has left its grace window by movie time and its grace
        // popup (the page's [data-upsell-surface="grace"]) is still to come:
        // Player.jsx puts it up from a render effect, a frame or more after
        // the element's clock crosses -- none at all while the tab is
        // hidden, where no frame is drawn -- and marks the element
        // (data-grace-cta-shown) when it does. Until then the offer is on its
        // way, and the status counts it as on screen (upsellElsewhere): a
        // render in between drew the plan box, and the popup folded it into
        // a line a moment later -- up to a second of box by the playing
        // video, or all the time the tab was away.
        graceOfferDue() {
            if (!doc.querySelector('[data-upsell-surface="grace"]')) return false;
            for (const m of media()) {
                const g = graceOf(m);
                if (g && !('graceCtaShown' in m.dataset) && movieTime(m) >= g) return true;
            }
            return false;
        },
        // The viewer has answered an offer about the cap for a player --
        // the grace popup's "continue at N Mbps" or its close, or the
        // slow-download modal's "watch as is" (answered) -- and that player
        // has not stalled for real since: they have just been
        // told the cap is coming, and the next offer is for when they hit
        // it (transferStatus.js present). A stall that counts on the
        // answered element ends it for that element; the next file (a new
        // element), a reload or another grace window starts without the
        // mark.
        offerAnswered() {
            forgetGone();
            countOngoing(now());
            for (const m of media()) {
                if (m.ended || !answered(m)) continue;
                if (!m[GRACE_SPENT]) return true;
            }
            return false;
        },
        stop() {
            for (const [name, fn] of listeners) doc.removeEventListener(name, fn, true);
        },
    };
}
