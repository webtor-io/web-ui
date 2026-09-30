import { test } from 'node:test';
import assert from 'node:assert/strict';
import Hls from 'hls.js';
import {
    classifyNetworkError, retryAfterMs, responseText, backoffMs, createNetworkRecovery, loaderOf,
    BACKOFF_FIRST_MS, BACKOFF_MAX_MS, MAX_ATTEMPTS, RETRY_AFTER_DEFAULT_MS, LEVEL_PARSING_RETRY_MS, ROUND_MS,
} from './network-recovery.js';
import { setupHlsEvents } from './hls-manager.js';

// ---- what hls.js 1.6.14 hands a fatal network error ---------------------
//
// The shapes below are hls.js's own (read off its source, see the module):
// `response` {url, data: undefined, code, text} from fragment-loader.ts
// onError / playlist-loader.ts handleNetworkError, and `networkDetails` the
// loader's XMLHttpRequest, which still holds the body the loader did not
// copy -- an ArrayBuffer for a fragment, text for a playlist.

const SESSION = 'https://api.webtor.io/abc/file.mkv~hls/session/f1097e820673061866306a7496758a2d';
const enc = (s) => new TextEncoder().encode(s).buffer;

// xhr is an XMLHttpRequest as xhr-loader leaves it at readyState 4.
function xhr({ status, body = '', type = 'arraybuffer', headers = '' } = {}) {
    return {
        status,
        responseType: type,
        get responseText() {
            if (type !== '' && type !== 'text') throw new Error('InvalidStateError');
            return body;
        },
        response: type === 'arraybuffer' ? enc(body) : body,
        responseURL: '',
        getAllResponseHeaders: () => headers,
        // Asking for a header CORS does not expose logs an error in Chrome:
        // the module must never do it.
        getResponseHeader: () => { throw new Error('getResponseHeader must not be called'); },
    };
}

// fragError: a fatal fragment error (fragment-loader.ts onError, escalated by
// the error-controller's onErrorOut).
function fragError(status, body, { url = `${SESSION}/v0-1080-34.ts`, headers = '' } = {}) {
    return {
        type: Hls.ErrorTypes.NETWORK_ERROR,
        details: Hls.ErrorDetails.FRAG_LOAD_ERROR,
        fatal: true,
        frag: { url, type: 'main' },
        response: { url, data: undefined, code: status, text: '' },
        networkDetails: xhr({ status, body, headers }),
    };
}

// playlistError: a playlist's (playlist-loader.ts handleNetworkError), text.
function playlistError(status, body, { url = `${SESSION}/v0-1080.m3u8`, details = Hls.ErrorDetails.LEVEL_LOAD_ERROR } = {}) {
    return {
        type: Hls.ErrorTypes.NETWORK_ERROR,
        details,
        fatal: true,
        url,
        context: { url },
        response: { url, data: undefined, code: status, text: '' },
        networkDetails: xhr({ status, body, type: 'text' }),
    };
}

const timeoutError = () => ({
    type: Hls.ErrorTypes.NETWORK_ERROR, details: Hls.ErrorDetails.FRAG_LOAD_TIMEOUT, fatal: true,
    frag: { url: `${SESSION}/v0-1080-3.ts`, type: 'main' }, networkDetails: xhr({ status: 0 }),
});

test('the body is read from the xhr hls.js passes: an ArrayBuffer for a fragment, text for a playlist', () => {
    assert.equal(responseText(fragError(404, 'session not found\n')), 'session not found\n');
    assert.equal(responseText(playlistError(404, 'init not found\n')), 'init not found\n');
    assert.equal(responseText({ networkDetails: null }), '');
    assert.equal(responseText({}), '');
});

test('classify: the session gone, its token gone, the limiter, other 4xx, the rest', () => {
    const k = (d) => { const c = classifyNetworkError(d); return c.kind + (c.reason ? `:${c.reason}` : ''); };
    assert.equal(k(fragError(404, 'session not found\n')), 'gone:404', 'the transcoder\'s words (18 bytes, prod 2026-09-29)');
    assert.equal(k(playlistError(404, 'session not found\n')), 'gone:404', 'a level playlist');
    assert.equal(k(playlistError(404, 'session not found\n', { details: Hls.ErrorDetails.MANIFEST_LOAD_ERROR, url: `${SESSION}/index.m3u8` })), 'gone:404', 'the master');
    assert.equal(k(playlistError(404, 'session not found\n', { details: Hls.ErrorDetails.AUDIO_TRACK_LOAD_ERROR, url: `${SESSION}/a0.m3u8` })), 'gone:404', 'an audio playlist');
    assert.equal(k(playlistError(404, 'session not found\n', { details: Hls.ErrorDetails.SUBTITLE_LOAD_ERROR, url: `${SESSION}/s0.m3u8` })), 'gone:404', 'a subtitle playlist');
    assert.equal(k(fragError(404, 'init not found\n', { url: `${SESSION}/v0-init-2.mp4` })), 'gone:404', 'a passthrough init of a run no longer there');
    assert.equal(k(fragError(404, '')), 'gone:404', 'no body to read, on a session URL');
    assert.equal(k(fragError(404, 'segment not found\n')), 'transient', 'the run released under the request: the session lives, asked again');
    assert.equal(k(fragError(404, '', { url: 'https://api.webtor.io/abc/file.mp4' })), 'client', 'a 404 elsewhere is not a session');
    assert.equal(k(fragError(403, '')), 'gone:403', 'the token (thp answers 403 with no body)');
    assert.equal(k(fragError(429, '')), 'limited');
    assert.equal(k(fragError(410, '')), 'client');
    assert.equal(k(fragError(500, 'x')), 'transient');
    assert.equal(k(fragError(503, 'transcoder restart limit reached')), 'transient');
    assert.equal(k(timeoutError()), 'transient');
    assert.equal(k({ type: Hls.ErrorTypes.NETWORK_ERROR, fatal: true }), 'transient', 'no status at all');
});

test('Retry-After: where CORS exposes it, seconds or a date, capped; else 5 s', () => {
    const withHeaders = (h) => ({ networkDetails: xhr({ status: 429, headers: h }) });
    assert.equal(retryAfterMs(withHeaders('content-type: text/plain\r\nretry-after: 7\r\n')), 7000);
    assert.equal(retryAfterMs(withHeaders('Retry-After: 0\r\n')), 0);
    assert.equal(retryAfterMs(withHeaders('Retry-After: 3600\r\n')), 60000, 'an hour is not waited out in a player');
    const now = Date.parse('2026-09-29T10:00:00Z');
    assert.equal(retryAfterMs(withHeaders('Retry-After: Tue, 29 Sep 2026 10:00:09 GMT\r\n'), now), 9000);
    assert.equal(retryAfterMs(withHeaders('content-type: text/plain\r\n')), RETRY_AFTER_DEFAULT_MS, 'not exposed (thp today, cross-origin)');
    assert.equal(retryAfterMs(withHeaders('Retry-After: soon\r\n')), RETRY_AFTER_DEFAULT_MS);
    assert.equal(retryAfterMs({}), RETRY_AFTER_DEFAULT_MS);
    assert.equal(RETRY_AFTER_DEFAULT_MS, 5000);
});

// ---- the recovery, on a fake instance and a hand-turned clock -----------

function bus() {
    const handlers = new Map();
    return {
        startLoads: 0,
        stopLoads: 0,
        on(ev, fn) { handlers.set(ev, [...(handlers.get(ev) || []), fn]); },
        off(ev, fn) { handlers.set(ev, (handlers.get(ev) || []).filter((f) => f !== fn)); },
        trigger(ev, data) { for (const fn of [...(handlers.get(ev) || [])]) fn(ev, data); },
        count(ev) { return (handlers.get(ev) || []).length; },
        startLoad() { this.startLoads++; },
        stopLoad() { this.stopLoads++; },
    };
}

function clock() {
    let t = 0;
    let id = 0;
    const timers = new Map();
    return {
        now: () => t,
        setTimer: (fn, ms) => { timers.set(++id, { fn, at: t + ms }); return id; },
        clearTimer: (i) => timers.delete(i),
        advance(ms) {
            const end = t + ms;
            for (;;) {
                let due = null;
                for (const [i, tm] of timers) if (tm.at <= end && (!due || tm.at < due[1].at)) due = [i, tm];
                if (!due) break;
                timers.delete(due[0]);
                t = due[1].at;
                due[1].fn();
            }
            t = end;
        },
        get pending() { return timers.size; },
    };
}

function setup(opts = {}) {
    const hls = bus();
    const c = clock();
    const gone = [];
    const goneInfo = [];
    const gaveUp = [];
    const r = createNetworkRecovery(hls, Hls, {
        onSessionGone: (reason, status, info) => { gone.push([reason, status]); goneInfo.push(info); },
        onGiveUp: (status) => gaveUp.push(status),
        setTimer: c.setTimer, clearTimer: c.clearTimer, now: c.now, log: () => {},
        ...opts,
    });
    return { hls, c, r, gone, goneInfo, gaveUp };
}

test('a dead session answered again and again: no startLoad, one onSessionGone', () => {
    const { hls, c, r, gone } = setup();
    // The loop as recorded: every startLoad met by the same 404, 66 a second.
    for (let i = 0; i < 200; i++) {
        r.handle(fragError(404, 'session not found\n'));
        c.advance(15);
    }
    c.advance(60 * 60 * 1000);
    assert.equal(hls.startLoads, 0);
    assert.deepEqual(gone, [['404', 404]]);
    assert.ok(hls.stopLoads >= 1, 'loading stays stopped');
    assert.equal(c.pending, 0);
});

test('403 (the token): the same, reason 403', () => {
    const { hls, c, r, gone } = setup();
    for (let i = 0; i < 10; i++) r.handle(playlistError(403, ''));
    c.advance(60000);
    assert.equal(hls.startLoads, 0);
    assert.deepEqual(gone, [['403', 403]]);
});

test('another 4xx: no retry, the visible error', () => {
    const { hls, c, r, gone, gaveUp } = setup();
    r.handle(fragError(410, ''));
    r.handle(fragError(410, ''));
    c.advance(60000);
    assert.equal(hls.startLoads, 0);
    assert.deepEqual(gaveUp, [410]);
    assert.deepEqual(gone, []);
});

test('429: Retry-After, then startLoad; the default 5 s where it cannot be read', () => {
    const { hls, c, r } = setup();
    r.handle(fragError(429, '', { headers: 'Retry-After: 7\r\n' }));
    c.advance(6999);
    assert.equal(hls.startLoads, 0);
    c.advance(1);
    assert.equal(hls.startLoads, 1);
    r.handle(fragError(429, ''));
    c.advance(RETRY_AFTER_DEFAULT_MS - 1);
    assert.equal(hls.startLoads, 1);
    c.advance(1);
    assert.equal(hls.startLoads, 2);
});

test('5xx, timeouts, status 0: 1 s doubling to 30 s, reset by a fragment of the film; given up after MAX_ATTEMPTS', () => {
    const { hls, c, r, gaveUp } = setup();
    const fail = (i) => r.handle(i % 3 === 0 ? fragError(502, '') : i % 3 === 1 ? timeoutError() : fragError(0, ''));
    for (let i = 0; i < 7; i++) {
        fail(i);
        c.advance(BACKOFF_MAX_MS);
    }
    assert.deepEqual(r.delays, [1000, 2000, 4000, 8000, 16000, 30000, 30000]);
    assert.equal(hls.startLoads, 7);
    // A subtitle segment is no sign the film loads again.
    hls.trigger(Hls.Events.FRAG_LOADED, { frag: { type: 'subtitle' } });
    fail(7);
    c.advance(BACKOFF_MAX_MS);
    assert.equal(r.delays.at(-1), 30000);
    // A video segment is.
    hls.trigger(Hls.Events.FRAG_LOADED, { frag: { type: 'main' } });
    fail(8);
    c.advance(BACKOFF_MAX_MS);
    assert.equal(r.delays.at(-1), BACKOFF_FIRST_MS, 'back to the first step');
    // Now the run to the end: MAX_ATTEMPTS in a row, then the card.
    hls.trigger(Hls.Events.FRAG_LOADED, { frag: { type: 'audio' } });
    const before = hls.startLoads;
    for (let i = 0; i < MAX_ATTEMPTS; i++) {
        fail(i);
        c.advance(BACKOFF_MAX_MS);
    }
    assert.equal(hls.startLoads - before, MAX_ATTEMPTS);
    assert.deepEqual(gaveUp, []);
    fail(0);
    c.advance(BACKOFF_MAX_MS * 10);
    assert.deepEqual(gaveUp, [502]);
    assert.equal(hls.startLoads - before, MAX_ATTEMPTS, 'nothing after giving up');
    assert.equal(backoffMs(0), 1000);
    assert.equal(backoffMs(40), BACKOFF_MAX_MS);
});

test('a second report while a retry waits is the same failure', () => {
    const { hls, c, r } = setup();
    r.handle(fragError(502, ''));
    r.handle(fragError(502, ''));
    r.handle(timeoutError());
    assert.equal(r.attempts, 1, 'one attempt, not three');
    c.advance(BACKOFF_FIRST_MS);
    assert.equal(hls.startLoads, 1);
    assert.deepEqual(r.delays, [1000]);
});

test('levelParsingError keeps its 3 s', () => {
    const { hls, c, r } = setup();
    r.handle({ type: Hls.ErrorTypes.NETWORK_ERROR, details: 'levelParsingError', fatal: true });
    r.handle({ type: Hls.ErrorTypes.NETWORK_ERROR, details: 'levelParsingError', fatal: true });
    assert.equal(c.pending, 1, 'one retry waits, not two');
    c.advance(LEVEL_PARSING_RETRY_MS - 1);
    assert.equal(hls.startLoads, 0);
    c.advance(1);
    assert.equal(hls.startLoads, 1);
    assert.equal(r.attempts, 0, 'not the backoff\'s');
});

test('destroy clears the timers and the listeners', () => {
    const { hls, c, r } = setup();
    r.handle(fragError(502, ''));
    assert.equal(c.pending, 1);
    hls.trigger(Hls.Events.DESTROYING);
    assert.equal(c.pending, 0);
    c.advance(60000);
    assert.equal(hls.startLoads, 0);
    assert.equal(hls.count(Hls.Events.FRAG_LOADED), 0);
    assert.equal(hls.count(Hls.Events.MANIFEST_LOADING), 0);
    assert.equal(hls.count(Hls.Events.DESTROYING), 0);
});

// After a fatal error of the MASTER playlist, startLoad() loads nothing:
// hls.js asks for the master only on MANIFEST_LOADING (playlist-loader.ts
// startLoad is empty). So the master is loaded again, and that reload is
// the backoff going on, not a new start.
test('a failed master is loaded again with loadSource, on the backoff', () => {
    const { hls, c, r, gaveUp } = setup();
    hls.url = 'https://api.webtor.io/x/session/abc/index.m3u8';
    hls.loads = [];
    hls.loadSource = function (u) { this.loads.push(u); this.trigger(Hls.Events.MANIFEST_LOADING, { url: u }); };
    const master = (code) => playlistError(code, 'bad gateway', { details: Hls.ErrorDetails.MANIFEST_LOAD_ERROR, url: hls.url });
    for (let i = 0; i < MAX_ATTEMPTS; i++) {
        r.handle(i % 2 ? master(502) : { ...timeoutError(), details: Hls.ErrorDetails.MANIFEST_LOAD_TIMEOUT });
        c.advance(BACKOFF_MAX_MS);
    }
    assert.equal(hls.startLoads, 0, 'startLoad would load nothing');
    assert.equal(hls.loads.length, MAX_ATTEMPTS);
    assert.deepEqual(r.delays.slice(0, 6), [1000, 2000, 4000, 8000, 16000, 30000], 'our own reload does not reset the backoff');
    r.handle(master(502));
    assert.deepEqual(gaveUp, [502]);
});

test('a new source (a session seek\'s reload) is a new start', () => {
    const { hls, c, r, gaveUp } = setup();
    r.handle(fragError(410, ''));
    assert.deepEqual(gaveUp, [410]);
    hls.trigger(Hls.Events.MANIFEST_LOADING, { url: 'x' });
    r.handle(fragError(502, ''));
    c.advance(BACKOFF_FIRST_MS);
    assert.equal(hls.startLoads, 1);
});

// ---- hls.js's own retries, and a stream that loads again ----------------
//
// hls.js retries a 5xx and a timeout itself before it makes it fatal (up to
// 100 times in HLS_CONFIG), and each fatal error's stopLoad() zeroes its
// counters: every startLoad of ours bought all 100 again. From the first
// fatal error on, its retries (ERROR with fatal: false) are attempts too.

// retried: what hls.js hands its listeners for a request it will ask again
// (the error-controller leaves fatal false).
const retried = (e) => ({ ...e, fatal: false });

test('from the first fatal error on, hls.js\'s own retries count toward the give-up, a round each', () => {
    const { hls, c, r, gaveUp } = setup();
    r.handle(fragError(503, 'transcoder restart limit reached\n'));
    assert.equal(r.attempts, 1);
    c.advance(BACKOFF_FIRST_MS);
    assert.equal(hls.startLoads, 1, 'our retry');
    // hls.js's counters start from zero again: it asks again and again,
    // 1, 2, 4, 8, then 10 s apart -- past ROUND_MS, a round each.
    for (let i = 1; i < MAX_ATTEMPTS; i++) {
        r.observe(retried(fragError(503, 'transcoder restart limit reached\n')));
        c.advance(ROUND_MS);
    }
    assert.equal(r.attempts, MAX_ATTEMPTS);
    assert.deepEqual(gaveUp, []);
    r.observe(retried(playlistError(503, 'transcoder restart limit reached\n')));
    assert.deepEqual(gaveUp, [503], 'MAX_ATTEMPTS rounds after the first fatal error, not MAX_ATTEMPTS of hls.js\'s cycles');
    assert.equal(hls.stopLoads, 0, 'not stopped here: the card stops it when it shows, and it waits for the buffer');
    assert.equal(c.pending, 0);
    c.advance(ROUND_MS);
    r.observe(retried(fragError(503, '')));
    r.handle(fragError(503, ''));
    c.advance(BACKOFF_MAX_MS);
    assert.deepEqual(gaveUp, [503], 'once');
    assert.equal(hls.startLoads, 1, 'nothing of ours after it');
});

test('before the first fatal error, hls.js\'s retries are its own patience', () => {
    const { r, gaveUp } = setup();
    for (let i = 0; i < 150; i++) r.observe(retried(i % 2 ? fragError(503, '') : timeoutError()));
    assert.equal(r.attempts, 0);
    assert.deepEqual(gaveUp, []);
});

test('a subtitle\'s failed requests are not the film\'s', () => {
    const { r, gaveUp } = setup();
    r.handle(fragError(502, ''));
    for (let i = 0; i < 3 * MAX_ATTEMPTS; i++) {
        r.observe(retried({ ...fragError(502, ''), frag: { url: `${SESSION}/s0-3.vtt`, type: 'subtitle' } }));
        r.observe(retried(playlistError(502, '', { url: `${SESSION}/s0.m3u8`, details: Hls.ErrorDetails.SUBTITLE_LOAD_ERROR })));
        r.observe(retried(playlistError(0, '', { url: `${SESSION}/s0.m3u8`, details: Hls.ErrorDetails.SUBTITLE_TRACK_LOAD_TIMEOUT })));
    }
    assert.equal(r.attempts, 1);
    assert.deepEqual(gaveUp, []);
});

// A 4xx is never retried by hls.js: fatal: false there is a level switch,
// and it ends fatal when nothing is left -- which handle() reads. Not
// counted meanwhile: a 410 is no round of the backoff's.
test('hls.js switching level on another 4xx is no attempt', () => {
    const { c, r, gaveUp } = setup();
    r.handle(fragError(502, ''));
    c.advance(BACKOFF_FIRST_MS);
    for (let i = 0; i < 3 * MAX_ATTEMPTS; i++) {
        r.observe(retried(fragError(410, '')));
        c.advance(ROUND_MS);
    }
    assert.equal(r.attempts, 1);
    assert.deepEqual(gaveUp, []);
    r.handle(fragError(410, ''));
    assert.deepEqual(gaveUp, [410]);
});

// Under the card loading can start again: a stall's startLoad, Play from
// the keyboard. When the film loads, the stream goes on, and so does this.
test('a fragment of the film after a give-up: the player is told, and the next failure is handled', () => {
    const loaded = [];
    const { hls, c, r, gaveUp, gone } = setup({ onFilmLoaded: () => loaded.push(true) });
    r.handle(fragError(410, ''));
    assert.deepEqual(gaveUp, [410]);
    // A subtitle segment is no sign the film loads.
    hls.trigger(Hls.Events.FRAG_LOADED, { frag: { type: 'subtitle' } });
    assert.equal(loaded.length, 0);
    r.handle(fragError(502, ''));
    assert.equal(r.pending, false, 'still given up');
    hls.trigger(Hls.Events.FRAG_LOADED, { frag: { type: 'main' } });
    assert.equal(loaded.length, 1);
    assert.equal(r.stopped, false);
    r.handle(fragError(502, ''));
    c.advance(BACKOFF_FIRST_MS);
    assert.equal(hls.startLoads, 1, 'the backoff again');
    r.handle(fragError(404, 'session not found\n'));
    assert.deepEqual(gone, [['404', 404]], 'and a dead session is reported again');
});

// ---- a dead session's answers hls.js does not make fatal ------------------
//
// The transcoder's variants are EVENT playlists, live to hls.js: a
// fragment's 404 is skipped as a gap (base-stream-controller.ts
// treatAsGap) and a playlist's 403/404 is asked again on hls.js's backoff
// (base-playlist-controller.ts checkRetry) -- fatal only after 100 of them.
// Chrome, 2026-09-30: 3,000-5,600 404s over ~300 s before the first fatal
// one. The first such answer, on any loader, is the session gone.

const onSession = (f) => `${SESSION}/${f}`;
const deadAnswers = [
    ['a video fragment skipped as a gap', retried(fragError(404, 'session not found\n')), '404', 'video'],
    ['an audio fragment', retried({ ...fragError(404, 'session not found\n', { url: onSession('a0-12.ts') }), frag: { url: onSession('a0-12.ts'), type: 'audio' } }), '404', 'audio'],
    ['a subtitle segment', retried({ ...fragError(404, 'session not found\n', { url: onSession('s0-3.vtt') }), frag: { url: onSession('s0-3.vtt'), type: 'subtitle' } }), '404', 'subtitle'],
    ['the video playlist asked again', retried(playlistError(404, 'session not found\n')), '404', 'video'],
    ['an audio playlist', retried(playlistError(404, 'session not found\n', { url: onSession('a0.m3u8'), details: Hls.ErrorDetails.AUDIO_TRACK_LOAD_ERROR })), '404', 'audio'],
    ['a subtitle playlist', retried(playlistError(404, 'session not found\n', { url: onSession('s0.m3u8'), details: Hls.ErrorDetails.SUBTITLE_LOAD_ERROR })), '404', 'subtitle'],
    ['a 404 with no body to read, on a session URL', retried(fragError(404, '')), '404', 'video'],
    ['the token refused (403, no body)', retried(playlistError(403, '')), '403', 'video'],
];

for (const [name, answer, reason, loader] of deadAnswers) {
    test(`a dead session's answer hls.js would ask again is the session gone at the first one: ${name}`, () => {
        const { hls, c, r, gone, goneInfo, gaveUp } = setup();
        r.observe(answer);
        assert.deepEqual(gone, [[reason, Number(reason)]]);
        assert.deepEqual(goneInfo, [{ via: 'error-nonfatal', loader }]);
        assert.equal(hls.stopLoads, 1, 'loading stopped at once');
        // hls.js walked on to the next fragment, or its playlist timer was
        // cleared: whatever still answers is stopped again, not reported.
        for (let i = 0; i < 200; i++) {
            r.observe(answer);
            c.advance(50);
        }
        assert.equal(gone.length, 1, 'reported once');
        assert.equal(hls.stopLoads, 201, 'every later answer stops loading again');
        c.advance(60 * 60 * 1000);
        assert.equal(hls.startLoads, 0);
        assert.deepEqual(gaveUp, []);
    });
}

test('loaderOf: which loader asked', () => {
    assert.equal(loaderOf(fragError(404, '')), 'video');
    assert.equal(loaderOf({ frag: { type: 'audio' } }), 'audio');
    assert.equal(loaderOf({ frag: { type: 'subtitle' } }), 'subtitle');
    assert.equal(loaderOf(playlistError(404, '')), 'video');
    assert.equal(loaderOf(playlistError(404, '', { details: Hls.ErrorDetails.AUDIO_TRACK_LOAD_ERROR })), 'audio');
    assert.equal(loaderOf(playlistError(404, '', { details: Hls.ErrorDetails.SUBTITLE_LOAD_ERROR })), 'subtitle');
    assert.equal(loaderOf(playlistError(404, '', { details: Hls.ErrorDetails.MANIFEST_LOAD_ERROR })), 'master');
    assert.equal(loaderOf({}), '');
    assert.equal(loaderOf(null), '');
});

// Only a live session says "init not found" (sessionRouter answers "session
// not found" first); one of its causes is the 10 s wait for a slow run's
// init (passthrough_web.go). hls.js asks again: its to ride out. Once it
// gives up (fatal), the run is not coming: the session's end, as before.
test('"init not found" while hls.js asks again is hls.js\'s; fatal, it is the session gone', () => {
    const { hls, r, gone } = setup();
    const init = (fatal) => ({ ...fragError(404, 'init not found\n', { url: onSession('v0-init-2.mp4') }), fatal });
    for (let i = 0; i < 10; i++) r.observe(init(false));
    assert.deepEqual(gone, []);
    assert.equal(hls.stopLoads, 0);
    r.handle(init(true));
    assert.deepEqual(gone, [['404', 404]]);
});

test('"segment not found" (the run released under the request) is no dead session, fatal or not', () => {
    const { r, gone } = setup();
    r.observe(retried(fragError(404, 'segment not found\n')));
    r.handle(fragError(404, 'segment not found\n'));
    assert.deepEqual(gone, []);
    assert.equal(r.pending, true, 'the backoff');
});

// A card waiting for the buffer to run out (stream-restart.js): hls.js's own
// retries go on under it, and may meet the session gone meanwhile.
test('the session gone after a give-up: loading stopped, and reported', () => {
    const { hls, r, gone, gaveUp } = setup();
    r.handle(fragError(410, ''));
    assert.deepEqual(gaveUp, [410]);
    r.observe(retried(fragError(404, 'session not found\n')));
    assert.deepEqual(gone, [['404', 404]]);
    assert.equal(hls.stopLoads, 1);
});

// ---- rounds, not requests ------------------------------------------------

// thp's limiter refuses the viewer's session, not a request: the video's,
// the audio's and the subtitles' playlists and fragments all come back 429
// together -- 5-6 a round, a round every ~7 s (the 5 s default wait, thp's
// 2 s hold). Chrome, 2026-09-30: counted per request, 4 rounds in 30 s spent
// all twenty attempts and put up the card on a film still playing.
test('429: every track refused in one round is one attempt', () => {
    const { hls, c, r, gaveUp } = setup();
    const at = (u, details, type) => (type
        ? { ...fragError(429, '', { url: onSession(u) }), frag: { url: onSession(u), type } }
        : playlistError(429, '', { url: onSession(u), details }));
    const round = () => {
        c.advance(2000); // thp holds the refusal
        r.observe(retried(at('v0-720.m3u8', Hls.ErrorDetails.LEVEL_LOAD_ERROR)));
        c.advance(300);
        r.observe(retried(at('a0.m3u8', Hls.ErrorDetails.AUDIO_TRACK_LOAD_ERROR)));
        c.advance(300);
        r.handle({ ...at('s0-3.vtt', null, 'subtitle'), fatal: true });
        c.advance(300);
        r.observe(retried(at('v0-720-40.ts', null, 'main')));
        r.observe(retried(at('a0-40.ts', null, 'audio')));
        c.advance(RETRY_AFTER_DEFAULT_MS);
    };
    for (let i = 0; i < 4; i++) round();
    assert.equal(r.attempts, 4, 'four rounds, four attempts -- not twenty');
    assert.deepEqual(gaveUp, []);
    assert.equal(hls.startLoads, 4, 'one retry a round, 5 s after its refusal');
    assert.ok(r.delays.every((d) => d === RETRY_AFTER_DEFAULT_MS), String(r.delays));
    for (let i = 4; i < MAX_ATTEMPTS; i++) round();
    assert.deepEqual(gaveUp, []);
    round();
    assert.deepEqual(gaveUp, [429], 'the card after MAX_ATTEMPTS rounds');
});

// A round with no fatal refusal (the subtitle's went through this time):
// hls.js asks its playlists again itself, 1 and 2 s on, and thp holds each
// refusal 2 s -- they come back 3 and 5 s after the round opened. Within the
// round's Retry-After: the same refusal of the session.
test('429: hls.js\'s own asking-again within the refusal\'s Retry-After is the same round', () => {
    const { c, r } = setup();
    const lvl = () => retried(playlistError(429, '', { url: onSession('v0-720.m3u8') }));
    r.handle({ ...fragError(429, '', { url: onSession('s0-3.vtt') }), frag: { url: onSession('s0-3.vtt'), type: 'subtitle' } });
    c.advance(RETRY_AFTER_DEFAULT_MS);
    assert.equal(r.attempts, 1);
    c.advance(2000);
    r.observe(lvl());
    assert.equal(r.attempts, 2, 'the round after our retry');
    c.advance(3000);
    r.observe(lvl());
    c.advance(1900);
    r.observe(lvl());
    assert.equal(r.attempts, 2, 'the same refusal, not two more rounds');
    c.advance(200);
    r.observe(lvl());
    assert.equal(r.attempts, 3, 'past it: the next round');
});

// Our retry asks for every track again: what fails after it is the next
// round, however soon -- the backoff keeps its steps.
test('5xx: the tracks failing after our retry are one round; the backoff keeps its steps', () => {
    const { c, r } = setup();
    r.handle(fragError(502, ''));
    c.advance(BACKOFF_FIRST_MS);
    r.observe(retried({ ...fragError(502, '', { url: onSession('a0-3.ts') }), frag: { url: onSession('a0-3.ts'), type: 'audio' } }));
    c.advance(100);
    r.handle(fragError(502, ''));
    assert.equal(r.attempts, 2, 'the audio\'s failure opened the round, the video\'s is in it');
    c.advance(2000);
    r.handle(fragError(502, ''));
    assert.equal(r.attempts, 3);
    assert.deepEqual(r.delays, [1000, 2000, 4000]);
});

// ---- through hls-manager's own wiring -----------------------------------

function managerBus() {
    return { ...bus(), levels: [], media: null, inFlightFragments: {}, recoverMediaError() {}, destroy() {} };
}

test('hls-manager: a fatal 404 of a dead session is never answered with startLoad', () => {
    const hls = managerBus();
    const c = clock();
    const gone = [];
    setupHlsEvents(hls, { now: () => 0, setInterval: () => 0, clearInterval: () => {} }, null,
        { onSessionGone: (r) => gone.push(r), setTimer: c.setTimer, clearTimer: c.clearTimer, log: () => {} });
    for (let i = 0; i < 100; i++) hls.trigger(Hls.Events.ERROR, fragError(404, 'session not found\n'));
    c.advance(60000);
    assert.equal(hls.startLoads, 0);
    assert.deepEqual(gone, ['404']);
});

test('hls-manager: without the player\'s callbacks a dead session still does not loop', () => {
    const hls = managerBus();
    const c = clock();
    setupHlsEvents(hls, { now: () => 0, setInterval: () => 0, clearInterval: () => {} }, null,
        { setTimer: c.setTimer, clearTimer: c.clearTimer, log: () => {} });
    for (let i = 0; i < 100; i++) hls.trigger(Hls.Events.ERROR, fragError(404, 'session not found\n'));
    c.advance(60000);
    assert.equal(hls.startLoads, 0);
});
