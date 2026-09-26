// Restarts hls.js's loader when playback is stalled and nothing is arriving
// -- nothing loading, or a request that stopped receiving bytes -- and only
// then.
//
// hls.js reports a stall (a non-fatal MEDIA_ERROR 'bufferStalledError', from
// its gap-controller) once per stall. The player used to answer every such
// report with setTimeout(() => hls.startLoad(), 5000). StreamController's
// startLoad() calls stopLoad() first, which aborts the fragment in flight
// and drops it from the fragment tracker. At the plan's cap a stall is
// exactly the moment the needed segment is still arriving -- so every stall
// threw that segment away 5 s in and fetched it again from byte 0 (recorded
// in Chrome 154 at a 5M cap, 2026-09-25/26: after a seek 48 aborts in one
// run; the owner's session 37 of 59 video segments requested 2-9 times and
// the film played at ~0.43x real time where the cap alone allows ~0.56x; in
// one run the aborted segment was skipped for good, a 4 s hole in the video
// buffer, and the player froze for the remaining 215 s).
//
// Nobody recorded why the restart was there. It came with the
// mediaelement.js player (f65afdea, 2024-03-31, next to a commented-out
// recoverMediaError()), which loaded hls.js 1.5.6 from a CDN with default
// retry settings; the Preact player carried it over (9f80e0ba). The only
// thing a startLoad() can fix is a loader that is not loading what the
// playhead needs -- stopped, or left in its ERROR state by a fatal error --
// and that still deserves a way out. hls-manager's fatal handler restarts
// loading after a network error; after a media error recoverMediaError()
// does it itself in 1.6 (hls.ts 661-671: startLoad at the playhead), which
// 1.5.6's did not (hls.ts 488-495). Everything else is hls.js's own: a
// fragment that errors or times out is retried by its load policy
// (fragLoadingMaxRetry in HLS_CONFIG; TTFB 10 s, 120 s a load), a playlist
// likewise, holes and nudges by the gap-controller.
//
// So the restart now needs RESTART_AFTER_MS of the element starving
// (readyState below HAVE_FUTURE_DATA), with no fragment on its way in any of
// hls.js's stream controllers (main, alternate audio, subtitles -- hls.js's
// own reading of "in flight", gap-controller inFlight(): any state but IDLE,
// STOPPED, ENDED and ERROR with a fragment, which counts a scheduled retry)
// and the buffered ranges unchanged, all the while. A segment that is
// arriving, however slowly, is never touched: with nothing in flight
// stopLoad() has nothing to abort.
//
// One kind of "in flight" is not arriving: a request whose body has stopped
// -- headers in, then no byte (a path that went dead, a response stuck
// upstream). hls.js cuts a request that gets no headers at 10 s (its TTFB,
// fragLoadPolicy maxTimeToFirstByteMs), but once headers are in only its
// 120 s load timeout is left (xhr-loader re-arms it at headers; progress
// never does), and gap-controller answers a stall with nothing. Reproduced in
// Chrome 154 (2026-09-26): a segment hung at 30% after headers froze the
// picture for 106-108 s, where the old 5 s startLoad() had it playing in 5.
// So a load in FRAG_LOADING past its headers counts as arriving only while
// its bytes grow: frag.stats IS the loader's live LoadStats
// (fragment-loader.ts: frag.stats = loader.stats; xhr-loader loadprogress
// sets stats.loaded; a retry is a new loader with new stats). With every
// in-flight load in that state and not one byte for NO_BYTES_MS -- still
// starving, the buffer still unchanged -- the loader is restarted, which
// aborts the dead request and asks again. Before its headers a load stays
// hls.js's (the TTFB above is its own recovery, and it fires first).
//
// NO_BYTES_MS is 10 s: hls.js's own bound for "no byte yet" applied to every
// later byte. It is far above the gaps of a segment arriving at a cap: thp's
// limiter (HybridBucket.Wait) paces each 32 KB write of the proxy and polls
// its Redis bucket every <= 100 ms, and one request on the session's bucket
// can take a second's worth of tokens ahead of another. Measured in Chrome
// at 5M against prod thp (2026-09-26, over-cap 1080p file, a session seek,
// HTTP/2, CDP Network.dataReceived): 4085 gaps between body chunks, p99
// 0.22 s, max 0.30 s. A segment still arriving, however slowly, keeps its
// stats.loaded growing and is never aborted. The same Chrome, a segment hung
// at 30% after its headers: playing again 10 s after the stall report.

export const RESTART_AFTER_MS = 5000;
export const NO_BYTES_MS = 10000;
export const CHECK_EVERY_MS = 1000;

// HTMLMediaElement.HAVE_FUTURE_DATA, spelled out for plain-object tests.
const HAVE_FUTURE_DATA = 3;

// hls.js's stream-controller states in which no fragment is on its way
// (base-stream-controller State; gap-controller inFlight()).
const NOT_IN_FLIGHT = new Set(['IDLE', 'STOPPED', 'ENDED', 'ERROR']);

// receivingBody returns the live stats of a stream controller's in-flight
// entry when it is a request past its headers -- the only kind whose
// progress this module judges -- and null otherwise. Parsing, appending, a
// scheduled retry, a load still waiting for its headers (hls.js's TTFB timer
// runs) and a fragment without stats are hls.js at work, never judged.
export function receivingBody(d) {
    if (!d || !d.frag || d.state !== 'FRAG_LOADING') return null;
    const st = d.frag.stats;
    if (!st || !st.loading || !(st.loading.first > 0)) return null;
    return st;
}

// loadProgress snapshots every stream controller with a fragment in flight
// -- loading, parsing, appending or waiting for its retry. `inFlight` is
// hls.inFlightFragments: { main: { frag, state }, audio?: …, subtitle?: … }.
// Each maps to { stats, loaded } for a request receiving its body, to null
// for any other in-flight state; an empty map is nothing in flight.
export function loadProgress(inFlight) {
    const out = new Map();
    for (const [k, d] of Object.entries(inFlight || {})) {
        if (!d || !d.frag || NOT_IN_FLIGHT.has(d.state)) continue;
        const st = receivingBody(d);
        out.set(k, st ? { stats: st, loaded: st.loaded } : null);
    }
    return out;
}

// loadWork compares two snapshots: 'busy' when anything in flight moved or
// is not judged (a byte came, a new request started, a state hls.js owns),
// 'dead' when every load in flight is past its headers with the same request
// and the same byte count as before, 'none' with nothing in flight.
export function loadWork(prev, cur) {
    if (cur.size === 0) return 'none';
    for (const [k, l] of cur) {
        const p = prev.get(k);
        if (!l || !p || p.stats !== l.stats || p.loaded !== l.loaded) return 'busy';
    }
    return 'dead';
}

// bufferedSig is the element's buffered ranges as one string: any append or
// eviction changes it.
export function bufferedSig(media) {
    try {
        const b = media.buffered;
        let s = '';
        for (let i = 0; i < b.length; i++) s += `${b.start(i).toFixed(3)}-${b.end(i).toFixed(3)};`;
        return s;
    } catch (e) {
        return '';
    }
}

// createLoaderRestart watches one hls.js instance for its life. `Hls` is the
// hls.js class (its Events / ErrorTypes / ErrorDetails); the clock and the
// interval are injectable for the tests.
export function createLoaderRestart(hls, Hls, {
    now = () => Date.now(),
    setInterval: startTimer = (fn, ms) => setInterval(fn, ms),
    clearInterval: stopTimer = (id) => clearInterval(id),
    kickMs = RESTART_AFTER_MS,
    noBytesMs = NO_BYTES_MS,
    log = (...a) => console.warn(...a),
} = {}) {
    let timer = null;
    let quietSince = 0;
    let sig = '';
    let loads = new Map();
    let kicks = 0;

    const disarm = () => {
        if (timer === null) return;
        stopTimer(timer);
        timer = null;
    };
    const check = () => {
        const m = hls.media;
        // Paused or finished: not a stall any more. hls.js reports the next
        // one afresh.
        if (!m || m.paused || m.ended) {
            disarm();
            return;
        }
        const t = now();
        const s = bufferedSig(m);
        const starving = !m.seeking && (m.readyState || 0) < HAVE_FUTURE_DATA;
        const cur = loadProgress(hls.inFlightFragments);
        const work = loadWork(loads, cur);
        loads = cur;
        if (s !== sig || !starving || work === 'busy') {
            sig = s;
            quietSince = t;
            return;
        }
        // Nothing in flight: RESTART_AFTER_MS. Requests in flight that stopped
        // receiving bytes: NO_BYTES_MS without a byte.
        const wait = work === 'dead' ? noBytesMs : kickMs;
        if (t - quietSince < wait) return;
        kicks++;
        quietSince = t;
        log(work === 'dead'
            ? `HLS stall: no bytes for ${Math.round(wait / 1000)} s on the request in flight, restarting the loader`
            : `HLS stall: nothing loading for ${Math.round(wait / 1000)} s, restarting the loader`);
        hls.startLoad();
    };
    const arm = () => {
        if (timer !== null) return;
        sig = hls.media ? bufferedSig(hls.media) : '';
        loads = loadProgress(hls.inFlightFragments);
        quietSince = now();
        timer = startTimer(check, CHECK_EVERY_MS);
    };
    const onError = (event, data) => {
        if (!data || data.fatal) return;
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR && data.details === Hls.ErrorDetails.BUFFER_STALLED_ERROR) arm();
    };
    const listeners = [
        [Hls.Events.ERROR, onError],
        // Played, sought or ended after the report (gap-controller).
        [Hls.Events.STALL_RESOLVED, disarm],
        // A session seek reloads the source (session-seek.js): a new start.
        [Hls.Events.MANIFEST_LOADING, disarm],
        [Hls.Events.MEDIA_DETACHING, disarm],
    ];
    const stop = () => {
        disarm();
        for (const [ev, fn] of listeners) hls.off(ev, fn);
        hls.off(Hls.Events.DESTROYING, stop);
    };
    for (const [ev, fn] of listeners) hls.on(ev, fn);
    hls.on(Hls.Events.DESTROYING, stop);

    return {
        stop,
        get armed() {
            return timer !== null;
        },
        get kicks() {
            return kicks;
        },
    };
}
