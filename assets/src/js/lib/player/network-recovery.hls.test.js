// The recovery against a REAL hls.js 1.6 instance and real XMLHttpRequests
// (jsdom's) to a local server that answers the way the transcoder and thp
// do. What this pins that the fake bus cannot: which errors hls.js's own
// error-controller makes fatal (and stops loading for, before our listener
// runs) and which it asks again -- with the player's own retry policy a
// playlist's 404/403 is the second kind --, that the status and the body are
// where network-recovery.js reads them, that a cross-origin Retry-After is
// not readable (thp sends no Access-Control-Expose-Headers), and -- the bug
// -- that nothing asks the server again. jsdom has no MediaSource, so no
// fragment is ever loaded here: playlists only (the master, a level, an
// audio rendition); fragments are the fake bus's (network-recovery.test.js),
// in the shapes fragment-loader.ts builds.

import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'https://webtor.io/res', pretendToBeVisual: true });
globalThis.window = dom.window;
globalThis.document = dom.window.document;
globalThis.self = dom.window;
Object.defineProperty(globalThis, 'navigator', { configurable: true, value: dom.window.navigator });

const { default: Hls } = await import('hls.js');
const { setupHlsEvents, HLS_CONFIG } = await import('./hls-manager.js');

const hits = new Map();
const server = http.createServer((req, res) => {
    const path = req.url.split('?')[0];
    hits.set(path, (hits.get(path) || 0) + 1);
    res.setHeader('Access-Control-Allow-Origin', '*');
    const text = (code, body, headers = {}) => {
        res.writeHead(code, { 'Content-Type': 'text/plain; charset=utf-8', ...headers });
        res.end(body);
    };
    if (path.endsWith('/live/session/abc/index.m3u8')) {
        res.writeHead(200, { 'Content-Type': 'application/vnd.apple.mpegurl' });
        res.end('#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=1280x720\nv0-720.m3u8\n');
        return;
    }
    // content-transcoder web.go sessionRouter: http.Error -> "session not found\n".
    if (path.endsWith('/live/session/abc/v0-720.m3u8')) return text(404, 'session not found\n');
    if (path.endsWith('/gone/session/abc/index.m3u8')) return text(404, 'session not found\n');
    // thp: a token it refuses, no body.
    if (path.endsWith('/expired/session/abc/index.m3u8')) return text(403, '');
    // thp refuseLimited: 429 + Retry-After: 5, not exposed to scripts.
    if (path.endsWith('/limited/session/abc/index.m3u8')) return text(429, '', { 'Retry-After': '5' });
    if (path.endsWith('/broken/session/abc/index.m3u8')) return text(502, 'bad gateway');
    // content-transcoder sessionPlaylistHandler: FFmpeg may not run again
    // (ErrorRestartLimit) -- 503 for good, until a seek or a new start.
    if (path.endsWith('/limit/session/abc/index.m3u8')) {
        res.writeHead(200, { 'Content-Type': 'application/vnd.apple.mpegurl' });
        res.end('#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=1280x720\nv0-720.m3u8\n');
        return;
    }
    if (path.endsWith('/limit/session/abc/v0-720.m3u8')) return text(503, 'transcoder restart limit reached\n');
    // The transcoder's own shape (web.go sessionMasterPlaylist): a video
    // level with an audio rendition beside it and a subtitle rendition, the
    // variants EVENT playlists -- live to hls.js (no CODECS: jsdom has no
    // MediaSource to accept any, and hls.js would refuse the manifest).
    // `dead*` answers the named
    // playlist the way a session gone (404) or a token refused (403) does;
    // the rest of the session still answers.
    const m = /\/(dead[a-z]+)\/session\/abc\/(.+)$/.exec(path);
    if (m) {
        const [, kind, file] = m;
        const failing = { deadvideo: 'v0-720.m3u8', deadaudio: 'a0.m3u8', deadtoken: 'v0-720.m3u8', deadinit: 'v0-720.m3u8' }[kind];
        if (file === failing) {
            if (kind === 'deadtoken') return text(403, '');
            if (kind === 'deadinit') return text(404, 'init not found\n');
            return text(404, 'session not found\n');
        }
        if (file === 'index.m3u8') {
            res.writeHead(200, { 'Content-Type': 'application/vnd.apple.mpegurl' });
            res.end('#EXTM3U\n'
                + '#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="en",LANGUAGE="en",DEFAULT=YES,AUTOSELECT=YES,URI="a0.m3u8"\n'
                + '#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="en",LANGUAGE="en",DEFAULT=YES,AUTOSELECT=YES,URI="s0.m3u8"\n'
                + '#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=1280x720,AUDIO="aud",SUBTITLES="subs"\n'
                + 'v0-720.m3u8\n');
            return;
        }
        if (file.endsWith('.m3u8')) {
            res.writeHead(200, { 'Content-Type': 'application/vnd.apple.mpegurl' });
            const seg = file.startsWith('s0') ? 's0-0.vtt' : file.replace('.m3u8', '-0.ts');
            res.end('#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXT-X-MEDIA-SEQUENCE:0\n'
                + `#EXTINF:4.000,\n${seg}\n`);
            return;
        }
    }
    return text(404, 'not found\n');
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;
after(() => server.close());

const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// run starts a real instance on `path` with hls-manager's own error
// handling and the given recovery options, and reports what happened.
async function run(path, recovery = {}, { waitMs = 600, config = {} } = {}) {
    // No retries of hls.js's own on 5xx: the fatal comes at once.
    const hls = new Hls({ manifestLoadingMaxRetry: 0, levelLoadingMaxRetry: 0, ...config });
    const startLoads = [];
    const origStart = hls.startLoad.bind(hls);
    hls.startLoad = (...a) => { startLoads.push(a); return origStart(...a); };
    const fatal = [];
    hls.on(Hls.Events.ERROR, (e, d) => { if (d.fatal) fatal.push(d); });
    const gone = [];
    const goneInfo = [];
    const gaveUp = [];
    const nonFatal = [];
    hls.on(Hls.Events.ERROR, (e, d) => { if (!d.fatal && d.type === Hls.ErrorTypes.NETWORK_ERROR) nonFatal.push(d); });
    setupHlsEvents(hls, { now: () => 0, setInterval: () => 0, clearInterval: () => {} }, null, {
        onSessionGone: (reason, status, info) => { gone.push([reason, status]); goneInfo.push(info); },
        onGiveUp: (status) => gaveUp.push(status),
        log: () => {},
        ...recovery,
    });
    hls.loadSource(`${base}${path}`);
    await wait(waitMs);
    return { hls, startLoads, fatal, gone, goneInfo, gaveUp, nonFatal };
}

test('real hls.js: a level playlist\'s 404 "session not found" is fatal, read as the session gone, and not asked again', async () => {
    hits.clear();
    const r = await run('/live/session/abc/index.m3u8');
    try {
        assert.equal(r.fatal.length, 1, 'hls.js made it fatal itself (error-controller onErrorOut)');
        assert.equal(r.fatal[0].details, Hls.ErrorDetails.LEVEL_LOAD_ERROR);
        assert.equal(r.fatal[0].response.code, 404, 'the status: data.response.code');
        assert.equal(r.fatal[0].networkDetails.responseText, 'session not found\n', 'the body: the xhr in networkDetails');
        assert.equal(r.hls.loadingEnabled, false, 'loading stopped before our listener ran');
        assert.deepEqual(r.gone, [['404', 404]]);
        // Only hls.js's own autostart; nothing of ours after the fatal error.
        assert.equal(r.startLoads.length, 1, 'the manifest\'s autostart, and nothing after');
        await wait(1500);
        assert.equal(hits.get('/live/session/abc/v0-720.m3u8'), 1, 'the dead playlist was asked once');
    } finally {
        r.hls.destroy();
    }
});

test('real hls.js: the master\'s 404 and 403 are the session gone; nothing is asked again', async () => {
    hits.clear();
    const a = await run('/gone/session/abc/index.m3u8');
    const b = await run('/expired/session/abc/index.m3u8');
    try {
        assert.deepEqual(a.gone, [['404', 404]]);
        assert.deepEqual(b.gone, [['403', 403]]);
        assert.equal(a.startLoads.length + b.startLoads.length, 0);
        await wait(1500);
        assert.equal(hits.get('/gone/session/abc/index.m3u8'), 1);
        assert.equal(hits.get('/expired/session/abc/index.m3u8'), 1);
    } finally {
        a.hls.destroy();
        b.hls.destroy();
    }
});

test('real hls.js: 429 waits the default 5 s (Retry-After is not exposed cross-origin), then loads again', async () => {
    hits.clear();
    const timers = [];
    const r = await run('/limited/session/abc/index.m3u8', {
        setTimer: (fn, ms) => { timers.push(ms); return setTimeout(fn, 0); },
        clearTimer: (id) => clearTimeout(id),
    });
    try {
        assert.equal(r.fatal[0].response.code, 429);
        assert.equal(/retry-after/i.test(r.fatal[0].networkDetails.getAllResponseHeaders()), false, 'not readable here, as in the browser');
        assert.equal(timers[0], 5000);
        assert.deepEqual(r.gone, []);
    } finally {
        r.hls.destroy();
    }
});

// The old handler's startLoad() after a fatal MASTER error asked for
// nothing: playlist-loader.ts loads the master only on MANIFEST_LOADING.
test('real hls.js: a master\'s 502 is loaded again on the backoff (loadSource), and destroy stops it', async () => {
    hits.clear();
    const timers = [];
    const r = await run('/broken/session/abc/index.m3u8', {
        setTimer: (fn, ms) => { timers.push(ms); return setTimeout(fn, 5); },
        clearTimer: (id) => clearTimeout(id),
    }, { waitMs: 400 });
    r.hls.destroy();
    const n = hits.get('/broken/session/abc/index.m3u8');
    assert.ok(n >= 3, `asked again: ${n}`);
    assert.deepEqual(timers.slice(0, 3), [1000, 2000, 4000], 'the backoff, not reset by its own reload');
    assert.equal(r.startLoads.length, 0);
    await wait(200);
    assert.equal(hits.get('/broken/session/abc/index.m3u8'), n, 'nothing after destroy');
});

// A 503 for good, with hls.js's own retries on (HLS_CONFIG has 100; 10 here,
// ~1 ms apart). hls.js makes it fatal after 1 + 10 requests -- its patience,
// untouched. Every fatal error's stopLoad() zeroes the counter it retries by
// (error-controller.ts stopLoad: playlistError), so each attempt of ours used
// to buy all 11 again: 11 × (maxAttempts + 1) requests before the card, ~5 h
// with the real config. From the first fatal error on its retries are ours.
// roundMs 0: the retries here are 1 ms apart, where hls.js's real ones are
// 1, 2, 4, 8, then 10 s apart -- each a round of its own past ROUND_MS.
// The give-up does not stop hls.js (the player's card does, when it shows):
// here, with no card, hls.js's own cycle runs out and ends fatal, and
// nothing of ours asks after that.
test('real hls.js: a 503 for good reaches the card after maxAttempts requests past hls.js\'s first cycle', async () => {
    hits.clear();
    const maxAttempts = 4;
    const at = [];
    const r0 = { gaveUp: [] };
    const r = await run('/limit/session/abc/index.m3u8', {
        setTimer: (fn, ms) => setTimeout(fn, 5),
        clearTimer: (id) => clearTimeout(id),
        maxAttempts,
        roundMs: 0,
        onGiveUp: (status) => { r0.gaveUp.push(status); at.push(hits.get('/limit/session/abc/v0-720.m3u8')); },
    }, { waitMs: 800, config: { levelLoadingMaxRetry: 10, levelLoadingRetryDelay: 1, levelLoadingMaxRetryTimeout: 1 } });
    try {
        assert.deepEqual(r0.gaveUp, [503]);
        assert.deepEqual(at, [11 + maxAttempts], `hls.js's 1 + 10, then ${maxAttempts} of ours and its own together`);
        assert.equal(r.fatal.length, 2, 'hls.js\'s first cycle, and its own second one run out after the give-up');
        const n = hits.get('/limit/session/abc/v0-720.m3u8');
        assert.equal(n, 11 + 11, 'our one startLoad bought one more cycle of hls.js\'s, not more');
        assert.equal(r.hls.loadingEnabled, false, 'stopped by its fatal error');
        await wait(300);
        assert.equal(hits.get('/limit/session/abc/v0-720.m3u8'), n, 'nothing of ours after the give-up');
    } finally {
        r.hls.destroy();
    }
});

// ---- a dead session's answers hls.js does NOT make fatal ------------------
//
// With the player's own retry policy (HLS_CONFIG: levelLoadingMaxRetry 100),
// a playlist's 404/403 is asked again on hls.js's backoff -- non-fatal until
// the 100th (base-playlist-controller.ts checkRetry). Measured in Chrome:
// 101 403s over 324 s, 3,000+ 404s. Read from the first one, on each loader
// jsdom lets load: the video's and the audio's playlists. A subtitle
// playlist needs attached media (subtitle-track-controller.ts), and
// fragments need a MediaSource: those are the fake bus's
// (network-recovery.test.js), in the shapes the loaders build.

const hitsOf = (kind, file) => hits.get(`/${kind}/session/abc/${file}`) || 0;

for (const [kind, file, loader, reason] of [
    ['deadvideo', 'v0-720.m3u8', 'video', '404'],
    ['deadaudio', 'a0.m3u8', 'audio', '404'],
    ['deadtoken', 'v0-720.m3u8', 'video', '403'],
]) {
    test(`real hls.js, the player's retry policy: the ${loader} playlist's non-fatal ${reason} is the session gone at the first answer`, async () => {
        hits.clear();
        const r = await run(`/${kind}/session/abc/index.m3u8`, {}, { waitMs: 700, config: { ...HLS_CONFIG } });
        try {
            assert.ok(r.nonFatal.length >= 1, 'hls.js would ask again: not fatal');
            assert.equal(r.fatal.length, 0, 'and it never went fatal');
            assert.deepEqual(r.gone, [[reason, Number(reason)]]);
            assert.deepEqual(r.goneInfo, [{ via: 'error-nonfatal', loader }]);
            assert.equal(r.hls.loadingEnabled, false, 'loading stopped');
            await wait(2500);
            assert.equal(hitsOf(kind, file), 1, `the dead ${file} asked once: hls.js's own retry went with stopLoad`);
        } finally {
            r.hls.destroy();
        }
    });
}

// Only a live session answers "init not found" (sessionRouter says "session
// not found" first), and one of its causes is the 10 s wait for a slow run's
// init: while hls.js asks again, it is hls.js's.
test('real hls.js: a non-fatal "init not found" is left to hls.js\'s own retries', async () => {
    hits.clear();
    const r = await run('/deadinit/session/abc/index.m3u8', {}, { waitMs: 2500, config: { ...HLS_CONFIG } });
    try {
        assert.ok(r.nonFatal.length >= 2, `hls.js asked again: ${r.nonFatal.length}`);
        assert.deepEqual(r.gone, []);
        assert.ok(hitsOf('deadinit', 'v0-720.m3u8') >= 2);
    } finally {
        r.hls.destroy();
    }
});
