// What the player does after a network error from hls.js (docs/player.md,
// "Network errors and the stream restart").
//
// Until 2026-09-29 the answer to every one was hls.startLoad(), at once, with
// no limit (only levelParsingError waited 3 s). hls.js 1.6 does not retry a
// 4xx (utils/error-helper.ts retryForHttpStatus) and a fragment's 404 ends
// fatal once no other level is left to switch to (error-controller.ts
// getFragRetryOrSwitchAction -> getLevelSwitchAction -> onErrorOut): the
// same request again, answered the same way -- up to 66 requests a second
// from one tab, a spinner and no error. The transcoder keeps its sessions in
// the pod's memory and drops one after 10 min without a request and on every
// rollout (content-transcoder session_manager.go), after which
// /session/<id>/... answers 404 "session not found"; a day later the token
// expires and the playlists answer 403. 2.27M 404s and 594k 403s a day on
// ~hls in thp, 114 of 929 viewer sessions a day on a dead session.
//
// Now each kind of failure gets its own answer:
//   - the session is gone (404 with the transcoder's "session not found" /
//     "init not found", or with no body to read on a session URL) or the
//     token is (403): nothing hls.js can load again -- onSessionGone, and
//     the player restarts the stream (stream-restart.js);
//   - 429 (thp's limiter refusal): Retry-After (default 5 s), then
//     startLoad;
//   - another 4xx: no retry, onGiveUp -- the player's card;
//   - anything else (5xx, a timeout, status 0): startLoad with exponential
//     backoff, 1 s doubling to 30 s, reset by a fragment that loads; after
//     MAX_ATTEMPTS, onGiveUp.
// levelParsingError keeps its 3 s, as before.
//
// A dead session is known from its FIRST answer, fatal or not (observe). On
// the transcoder's variants -- #EXT-X-PLAYLIST-TYPE:EVENT, live to hls.js --
// most of a dead session's answers never become fatal: a fragment's 404 is
// skipped as a gap (base-stream-controller.ts onFragmentOrKeyLoadError ->
// treatAsGap: a media fragment of a live level with no alternate), and a
// playlist's 403/404 is asked again on hls.js's backoff until
// error-controller's playlistError reaches levelLoadingMaxRetry (100 in
// HLS_CONFIG) -- checkRetry in base-playlist-controller.ts retries a
// SendAlternateToPenaltyBox that carries a retryConfig. Measured in Chrome
// with hls.js 1.6.14 (2026-09-30): 3,000-5,600 404s over ~300 s before the
// first fatal one, or a spinner at the end of an episode; the fatal answer
// came at once only where a subtitle .vtt was loading. So a "gone" answer on
// any loader -- the video's, an audio rendition's, a subtitle's, the
// master's -- stops loading and is reported at once. Not the transcoder's
// "init not found" while hls.js still asks again: only a live session says
// it (sessionRouter answers "session not found" first), and one of its
// three causes is the 10 s wait for a slow run's init (passthrough_web.go
// passthroughInitWait), which hls.js's own retry rides out; it is the
// session's end only once hls.js gives it up (fatal).
//
// MAX_ATTEMPTS counts hls.js's own retries too, from the first fatal error
// on (observe). hls.js retries a 5xx and a timeout itself (HLS_CONFIG: up to
// 100 times, at most 10 s apart) before it makes the error fatal, and that
// patience stays its own. But the fatal error's stopLoad() zeroes the
// counters it keeps (level-controller.ts stopLoad: level.fragmentError and
// loadError; error-controller.ts stopLoad: playlistError), so each startLoad
// of ours used to buy all 100 again: a 503 for good (the transcoder's
// "restart limit reached") reached the card after 21 of hls.js's cycles,
// about 5 h. The master's retries happen inside hls.js's loader with no
// event (xhr-loader.ts retry), so they are not counted: a master answering
// 5xx for good still gets a whole cycle per attempt.
//
// Attempts are ROUNDS, not requests. A round opens with the first failure
// after our retry has gone (a fatal error stopped all of hls.js's loaders;
// our startLoad asks for each track again, and each gets its own answer) --
// or, with no retry of ours out, ROUND_MS after the last one opened -- and
// takes in everything that fails while our next retry waits and within
// ROUND_MS of its opening; for a 429, within its Retry-After: thp's limiter
// refuses the viewer's session, not a request, so the video's, the audio's
// and the subtitles' playlists and fragments all come back refused together
// (5-6 per round, measured). Counted per request, four rounds of the
// limiter in 30 s spent all twenty attempts and put up the card.
//
// A fragment of the film that loads after onGiveUp or onSessionGone (a
// stall's startLoad, Play from the keyboard, a session seek on a live
// session) means the stream goes on: this watches again, and the player is
// told (onFilmLoaded), so the card it put up comes down.
//
// onGiveUp does not stop loading itself: the player's card does, when it
// shows -- and it waits while the film still plays from its buffer
// (stream-restart.js). A fatal error has already stopped hls.js; after the
// last of hls.js's own retries counted here (observe) hls.js may go on with
// its paced ones, and one that loads a fragment takes the card back.
// After onSessionGone every further "gone" answer stops loading again,
// quietly: something else may start it (loader-restart's startLoad on a
// stall), and hls.js would walk the dead session's playlist again.
//
// A failed MASTER playlist is loaded again with loadSource, not startLoad:
// hls.js loads the master only on MANIFEST_LOADING (playlist-loader.ts: its
// startLoad is empty), so after a fatal manifest error startLoad() loads
// nothing at all -- the old handler's answer to one was a spinner forever.
//
// Where hls.js 1.6.14 puts what this reads (read off its source):
//   - the status: `data.response.code` on an HTTP error of a fragment
//     (loader/fragment-loader.ts onError: response {url, data: undefined,
//     code, text}) or of a playlist -- manifest, level, audio, subtitle
//     (loader/playlist-loader.ts handleNetworkError: the same shape); none on
//     a timeout;
//   - the body: `data.networkDetails`, the loader's XMLHttpRequest
//     (utils/xhr-loader.ts passes itself to onError). The loader does not
//     copy the body of an error (response.data is undefined), but the xhr
//     still holds it: `response` as an ArrayBuffer for a fragment
//     (responseType 'arraybuffer'), `responseText` for a playlist ('text');
//   - Retry-After: the same xhr's headers, readable only where CORS exposes
//     them (thp does not send Access-Control-Expose-Headers today, so the
//     5 s default is what applies cross-origin -- thp's own value is 5).
// The fatal error has already stopped loading when this runs: the
// error-controller's onErrorOut is registered in the Hls constructor, before
// any listener of ours, and calls hls.stopLoad() on data.fatal.

export const BACKOFF_FIRST_MS = 1000;
export const BACKOFF_MAX_MS = 30000;
export const MAX_ATTEMPTS = 20;
export const RETRY_AFTER_DEFAULT_MS = 5000;
// A Retry-After above this is not waited out in a player: the viewer would
// be looking at a frozen picture for longer than any retry is worth.
export const RETRY_AFTER_MAX_MS = 60000;
export const LEVEL_PARSING_RETRY_MS = 3000;
// Failures this close to the one that opened a round are the same round:
// requests asked for together (our startLoad asks for every track) come back
// within thp's 2 s hold of a refusal (refuseLimited).
export const ROUND_MS = 2000;

const LEVEL_PARSING_ERROR = 'levelParsingError';
// hls.js errors.ts: the master playlist's (playlist-loader.ts, fatal).
const MANIFEST_ERRORS = new Set(['manifestLoadError', 'manifestLoadTimeOut', 'manifestParsingError']);
// hls.js errors.ts: a subtitle playlist's (playlist-loader.ts
// handleNetworkError). A subtitle segment's is a fragment error whose
// frag.type is 'subtitle'.
const SUBTITLE_PLAYLIST_ERRORS = new Set(['subtitleTrackLoadError', 'subtitleTrackLoadTimeOut']);

// ofSubtitles: an error of a text track -- nothing the film waits for.
function ofSubtitles(data) {
    return SUBTITLE_PLAYLIST_ERRORS.has(data.details) || !!(data.frag && data.frag.type === 'subtitle');
}

// A transcoder session URL: .../~hls/session/<id>/<file>.
const SESSION_PATH = /\/session\/[^/?#]+\//;
// The transcoder's own words for a session it no longer has, and for the
// init of a run it no longer has (content-transcoder web.go sessionRouter,
// passthrough_web.go sessionInitHandler): no request of this player's can
// be answered again.
const GONE_BODY = /session not found|init not found/i;
const INIT_BODY = /init not found/i;
// ...and for a segment whose run was released under the request
// (web.go serveSegment): the session lives, and the next request restarts
// the run (RestartForSegment). Asked again, with the backoff.
const RUN_RACE_BODY = /segment not found/i;

export function httpStatus(data) {
    if (!data) return undefined;
    const r = data.response;
    if (r && typeof r.code === 'number') return r.code;
    const nd = data.networkDetails;
    try {
        if (nd && typeof nd.status === 'number') return nd.status;
    } catch (e) { /* no status */ }
    return undefined;
}

// responseText is the start of the error's body, '' where there is none to
// read (a timeout, an opaque answer, a body CORS keeps from us).
export function responseText(data) {
    const nd = data && data.networkDetails;
    if (!nd) return '';
    try {
        const type = nd.responseType;
        if ((type === '' || type === 'text') && typeof nd.responseText === 'string') return nd.responseText.slice(0, 256);
    } catch (e) { /* responseText throws for other types */ }
    try {
        const b = nd.response;
        if (typeof b === 'string') return b.slice(0, 256);
        if (b && (b instanceof ArrayBuffer || ArrayBuffer.isView(b)) && typeof TextDecoder === 'function') {
            const bytes = b instanceof ArrayBuffer ? new Uint8Array(b, 0, Math.min(256, b.byteLength))
                : new Uint8Array(b.buffer, b.byteOffset, Math.min(256, b.byteLength));
            return new TextDecoder().decode(bytes);
        }
    } catch (e) { /* no body */ }
    return '';
}

export function requestURL(data) {
    if (!data) return '';
    const nd = data.networkDetails;
    return String((data.response && data.response.url) || data.url || (data.frag && data.frag.url)
        || (data.context && data.context.url) || (nd && nd.responseURL) || '');
}

// retryAfterMs reads Retry-After (seconds, or an HTTP date) where the
// browser lets us: getAllResponseHeaders lists only the headers CORS
// exposes, and asking getResponseHeader for any other makes Chrome log an
// error -- so it is looked for in the list, as hls.js's own
// xhr-loader.getResponseHeader does.
export function retryAfterMs(data, now = Date.now()) {
    const nd = data && data.networkDetails;
    let v = null;
    try {
        if (nd && typeof nd.getAllResponseHeaders === 'function') {
            const m = /^retry-after:\s*(.+?)\s*$/im.exec(nd.getAllResponseHeaders() || '');
            if (m) v = m[1];
        } else if (nd && nd.headers && typeof nd.headers.get === 'function') {
            v = nd.headers.get('Retry-After');
        }
    } catch (e) { v = null; }
    if (v) {
        let ms = NaN;
        if (/^\d+$/.test(v)) ms = parseInt(v, 10) * 1000;
        else {
            const at = Date.parse(v);
            if (!Number.isNaN(at)) ms = at - now;
        }
        if (Number.isFinite(ms) && ms >= 0) return Math.min(ms, RETRY_AFTER_MAX_MS);
    }
    return RETRY_AFTER_DEFAULT_MS;
}

// loaderOf: which of hls.js's loaders asked -- 'video' (the main level's
// playlist or fragment), 'audio', 'subtitle', 'master' -- '' where the error
// does not say.
export function loaderOf(data) {
    if (!data) return '';
    const type = data.frag && data.frag.type;
    if (type) return type === 'main' ? 'video' : String(type);
    const d = String(data.details || '');
    if (d.startsWith('manifest')) return 'master';
    if (d.startsWith('level')) return 'video';
    if (d.startsWith('audioTrack')) return 'audio';
    if (d.startsWith('subtitle')) return 'subtitle';
    return '';
}

// classifyNetworkError: { kind, status, reason?, init? }
//   kind 'gone'      -- the session or its token is gone (reason '404' |
//                       '403'): a restart of the stream, never a reload;
//                       init: the words were "init not found" (see the top
//                       of the module: gone only once hls.js gives it up);
//   kind 'limited'   -- 429: Retry-After;
//   kind 'client'    -- another 4xx: no retry;
//   kind 'transient' -- 5xx, a timeout, status 0 or none: the backoff.
export function classifyNetworkError(data) {
    const status = httpStatus(data);
    if (status === 404) {
        const body = responseText(data);
        const onSession = SESSION_PATH.test(requestURL(data));
        if (GONE_BODY.test(body)) return { kind: 'gone', reason: '404', status, init: INIT_BODY.test(body) };
        if (onSession && RUN_RACE_BODY.test(body)) return { kind: 'transient', status };
        if (onSession && !body.trim()) return { kind: 'gone', reason: '404', status };
        return { kind: 'client', status };
    }
    if (status === 403) return { kind: 'gone', reason: '403', status };
    if (status === 429) return { kind: 'limited', status };
    if (typeof status === 'number' && status >= 400 && status < 500) return { kind: 'client', status };
    return { kind: 'transient', status: typeof status === 'number' ? status : 0 };
}

export function backoffMs(attempt) {
    return Math.min(BACKOFF_FIRST_MS * Math.pow(2, attempt), BACKOFF_MAX_MS);
}

// createNetworkRecovery watches one hls.js instance for its life. `Hls` is
// the hls.js class (its Events). handle(data) is called by hls-manager with
// every fatal NETWORK_ERROR the guard did not take, observe(data) with every
// other one. onSessionGone(reason, status, { via, loader }) and
// onGiveUp(status) are the player's (Player.jsx: restart or the card); via is
// 'load' for a fatal error, 'error-nonfatal' for one hls.js would have asked
// again. After either, nothing here starts loading again until a new source
// loads (MANIFEST_LOADING: a session seek's reload is a new start) or a
// fragment of the film does (something else started loading, and it works).
// onFilmLoaded() is told of every fragment of the film. Timers are cleared on
// DESTROYING.
export function createNetworkRecovery(hls, Hls, {
    onSessionGone = () => {},
    onGiveUp = () => {},
    onFilmLoaded = () => {},
    setTimer = (fn, ms) => setTimeout(fn, ms),
    clearTimer = (id) => clearTimeout(id),
    now = () => Date.now(),
    maxAttempts = MAX_ATTEMPTS,
    roundMs = ROUND_MS,
    log = (...a) => console.warn(...a),
} = {}) {
    let timer = null;
    let attempts = 0;
    let stopped = false;
    // Stopped because the session is gone (not a give-up): a later "gone"
    // answer -- someone else started loading -- is stopped again.
    let dead = false;
    // Our own loadSource of the master (below): not a new start.
    let reloading = false;
    // The round the last counted attempt opened lasts until then.
    let roundUntil = -Infinity;
    const delays = [];

    const clear = () => {
        if (timer === null) return;
        clearTimer(timer);
        timer = null;
    };
    // One pending retry at a time: a second fatal report while one waits is
    // the same failure (hls.js has stopped loading; nothing else is asked).
    const schedule = (ms, master = false) => {
        if (timer !== null) return;
        delays.push(ms);
        timer = setTimer(() => {
            timer = null;
            // Our retry asks for every track again: what fails from here is
            // the next round.
            roundUntil = -Infinity;
            if (stopped) return;
            const url = master ? hls.url : null;
            if (!url) {
                hls.startLoad();
                return;
            }
            reloading = true;
            try {
                hls.loadSource(url);
            } finally {
                reloading = false;
            }
        }, ms);
    };
    const stopLoading = () => {
        try { hls.stopLoad(); } catch (e) { /* already stopped */ }
    };
    // A new round: no retry of ours waiting (what fails before it goes is
    // the round it answers), and past the window of the last one. Opens it.
    const newRound = (c, data) => {
        const t = now();
        if (timer !== null || t < roundUntil) return false;
        roundUntil = t + Math.max(roundMs, c.kind === 'limited' ? retryAfterMs(data, t) : 0);
        return true;
    };
    // Loading stays stopped -- after a give-up too (a card waiting for the
    // buffer to run out: hls.js's own retries meet the dead session), and
    // again on every later answer of it -- and the player is told, once: it
    // restarts the stream, or its card stays.
    const gone = (c, data, via) => {
        stopLoading();
        if (dead) return;
        log(`HLS: ${c.status} on ${requestURL(data) || 'a request'}${via === 'load' ? '' : ' (not fatal)'}: the session is gone, not retrying`);
        stopped = true;
        dead = true;
        clear();
        try { onSessionGone(c.reason, c.status, { via, loader: loaderOf(data) }); } catch (e) { /* the page goes on */ }
    };
    // No retry of ours from here. Loading is not stopped: a fatal error has
    // stopped it already, and after hls.js's own counted retries the card
    // stops it when it shows (it waits while the film plays from its buffer).
    const giveUp = (status) => {
        stopped = true;
        clear();
        try { onGiveUp(status); } catch (e) { /* the page goes on */ }
    };

    function handle(data) {
        const c = data && data.details === LEVEL_PARSING_ERROR ? null : classifyNetworkError(data);
        if (c && c.kind === 'gone') {
            gone(c, data, 'load');
            return;
        }
        if (stopped) return;
        if (!c) {
            schedule(LEVEL_PARSING_RETRY_MS);
            return;
        }
        switch (c.kind) {
            case 'client':
                log(`HLS: ${c.status} on ${requestURL(data) || 'a request'}: not retrying`);
                giveUp(c.status);
                return;
            case 'limited':
            default: {
                if (timer !== null) return;
                const fresh = newRound(c, data);
                if (fresh && attempts >= maxAttempts) {
                    log(`HLS: ${attempts} rounds without a fragment, giving up`);
                    giveUp(c.status);
                    return;
                }
                schedule(c.kind === 'limited' ? retryAfterMs(data, now()) : backoffMs(Math.max(0, attempts - (fresh ? 0 : 1))),
                    !!data && MANIFEST_ERRORS.has(data.details));
                if (fresh) attempts++;
            }
        }
    }

    // observe: an error hls.js answers itself (not fatal: it asks again,
    // switches level, or skips a live fragment as a gap).
    //   - The session gone (see the top of the module): stopped and
    //     reported now, whichever loader asked -- subtitles too: the session
    //     is one for all of them. Not "init not found" (hls.js rides it
    //     out; fatal, handle() takes it).
    //   - Otherwise, before the first fatal error it is hls.js's patience
    //     and none of ours. After it, a failed round of the film is an
    //     attempt, whichever of the two layers asked (see the top of the
    //     module). Only what the backoff would have asked again counts (5xx,
    //     a timeout, status 0, 429): hls.js never retries another 4xx, so
    //     here it is switching level, and if nothing is left to switch to it
    //     ends fatal -- handle() then says what it is. Subtitles are not the
    //     film.
    function observe(data) {
        if (!data || data.fatal) return;
        const c = classifyNetworkError(data);
        if (c.kind === 'gone' && !c.init) {
            gone(c, data, 'error-nonfatal');
            return;
        }
        if (stopped || attempts === 0) return;
        if (ofSubtitles(data)) return;
        if (c.kind !== 'transient' && c.kind !== 'limited') return;
        if (!newRound(c, data)) return;
        if (attempts >= maxAttempts) {
            log(`HLS: ${attempts} rounds without a fragment (hls.js's own retries counted), giving up`);
            giveUp(c.status);
            return;
        }
        attempts++;
    }

    // A fragment of the film arrived: whatever failed before is over -- the
    // backoff starts from its first step again, and after a give-up or a
    // dead session this watches again: something started loading (a stall's
    // startLoad, Play from the keyboard, a session seek on a live session),
    // and it works. Not a subtitle's: a text track that loads says nothing
    // of the picture, and would otherwise keep a failing video at the first,
    // 1 s, step forever.
    const onFragLoaded = (event, data) => {
        const type = data && data.frag && data.frag.type;
        if (type === 'subtitle') return;
        attempts = 0;
        roundUntil = -Infinity;
        stopped = false;
        dead = false;
        try { onFilmLoaded(); } catch (e) { /* the page goes on */ }
    };
    // A new source (a session seek's loadSource): a new start. Not our own
    // reload of a master that failed: that is the backoff going on.
    const onManifestLoading = () => {
        if (reloading) return;
        clear();
        attempts = 0;
        roundUntil = -Infinity;
        stopped = false;
        dead = false;
    };
    const listeners = [
        [Hls.Events.FRAG_LOADED, onFragLoaded],
        [Hls.Events.MANIFEST_LOADING, onManifestLoading],
    ];
    const dispose = () => {
        clear();
        stopped = true;
        for (const [ev, fn] of listeners) hls.off(ev, fn);
        hls.off(Hls.Events.DESTROYING, dispose);
    };
    for (const [ev, fn] of listeners) hls.on(ev, fn);
    hls.on(Hls.Events.DESTROYING, dispose);

    return {
        handle,
        observe,
        dispose,
        get pending() { return timer !== null; },
        get attempts() { return attempts; },
        get stopped() { return stopped; },
        // The delays scheduled so far, for the tests.
        get delays() { return delays.slice(); },
    };
}
