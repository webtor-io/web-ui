import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import {
    REASONS, STRIKING, SAME_INCIDENT_MS, NO_FRAMES_AFTER_MS,
    passthroughHlsConfig, createPassthroughGuard, fallbackToOldRoute, fallbackURL, framesCounted,
} from './passthrough.js';
import {
    MEMORY_KEY, OPTIN_KEY, CACHE_KEY, PENDING_TTL_MS, loadMemory, installSubmitHook, clearPendingFallback, pendingFallbackFor,
} from './decode-declaration.js';

// ---- the fragment policy -----------------------------------------------------

test('passthrough: a segment gets the job\'s load time and 3 tries; HTTP errors keep today\'s 100 retries', () => {
    const base = { fragLoadingMaxRetry: 100, fragLoadingMaxRetryTimeout: 10000, maxBufferSize: 50e6 };
    const c = passthroughHlsConfig(base, 360000);
    assert.equal(c.maxBufferSize, 50e6, 'the rest of the config is the old one');
    assert.deepEqual(c.fragLoadPolicy.default, {
        maxTimeToFirstByteMs: 10000,
        maxLoadTimeMs: 360000,
        timeoutRetry: { maxNumRetry: 2, retryDelayMs: 0, maxRetryDelayMs: 0 },
        errorRetry: { maxNumRetry: 100, retryDelayMs: 1000, maxRetryDelayMs: 10000 },
    });
    assert.equal(passthroughHlsConfig(base, 0).fragLoadPolicy.default.maxLoadTimeMs, 120000, 'no number: hls.js\'s own 120 s');
    assert.equal(passthroughHlsConfig(base, 'x').fragLoadPolicy.default.maxLoadTimeMs, 120000);
});

// The errorRetry above is what hls.js itself makes of the legacy settings
// every stream has today: read off a real instance of the installed hls.js.
test('passthrough: errorRetry is what hls.js makes of today\'s legacy settings', async () => {
    const { default: Hls } = await import('hls.js');
    const legacy = {
        fragLoadingMaxRetry: 100,
        fragLoadingMaxRetryTimeout: 1000 * 10,
    };
    const h = new Hls(legacy);
    const today = h.config.fragLoadPolicy.default;
    const ours = passthroughHlsConfig(legacy, 300000).fragLoadPolicy.default;
    assert.deepEqual(ours.errorRetry, today.errorRetry);
    assert.equal(today.timeoutRetry.maxNumRetry, 100, 'today: 100 refetches of a timed-out segment');
    assert.equal(today.maxLoadTimeMs, 120000);
    h.destroy();
});

// ---- the guard -------------------------------------------------------------

function page({ url = 'https://webtor.io/ru/res1?file=a.mkv', storageThrows = false } = {}) {
    const dom = new JSDOM('<!doctype html><body></body>', { url, pretendToBeVisual: true });
    const win = dom.window;
    if (storageThrows) {
        Object.defineProperty(win, 'localStorage', { get() { throw new win.DOMException('denied', 'SecurityError'); }, configurable: true });
    }
    return win;
}

function player(win, { cls = 'hevc10-2160', rid = 'res1', iid = 'item1', path = 'a.mkv' } = {}) {
    const v = win.document.createElement('video');
    v.dataset.videoRoute = 'passthrough';
    v.dataset.videoClass = cls;
    v.dataset.resourceId = rid;
    v.dataset.itemId = iid;
    v.dataset.path = path;
    win.document.body.appendChild(v);
    let err = null;
    Object.defineProperty(v, 'error', { get: () => err, configurable: true });
    let ct = 0;
    Object.defineProperty(v, 'currentTime', { get: () => ct, set: (x) => { ct = x; }, configurable: true });
    let w = 1920;
    Object.defineProperty(v, 'videoWidth', { get: () => w, configurable: true });
    let frames = null;
    v.getVideoPlaybackQuality = () => (frames === null ? undefined : { totalVideoFrames: frames });
    return {
        v,
        failWith: (code) => { err = code ? { code } : null; v.dispatchEvent(new win.Event('error')); },
        setError: (code) => { err = code ? { code } : null; },
        setTime: (x) => { ct = x; },
        setWidth: (x) => { w = x; },
        setFrames: (x) => { frames = x; },
    };
}

function fakeHls() {
    return { recovered: 0, recoverMediaError() { this.recovered++; } };
}

// A guard with its clock and timers in the test's hands.
function guarded(win, p, { hls = null } = {}) {
    let t = 0;
    const timers = [];
    const fired = [];
    const g = createPassthroughGuard({
        video: p.v, win, doc: win.document,
        fallback: (reason, path) => fired.push({ reason, path }),
        now: () => t,
        setTimer: (fn, ms) => { timers.push({ fn, at: t + ms }); return timers.length; },
        clearTimer: (id) => { if (timers[id - 1]) timers[id - 1].fn = null; },
    });
    if (hls) g.setHls(hls);
    return {
        g, fired,
        advance: (ms) => {
            t += ms;
            for (const x of timers) if (x.fn && x.at <= t) { const fn = x.fn; x.fn = null; fn(); }
        },
    };
}

const MEDIA = (details = 'bufferAppendError') => ({ type: 'mediaError', details, fatal: true });

test('guard: a media error is recovered once; the next one gives the file up (media_error without the element\'s word)', () => {
    const win = page();
    const p = player(win);
    const hls = fakeHls();
    const x = guarded(win, p, { hls });
    assert.equal(x.g.onHlsError(hls, MEDIA()), true);
    assert.equal(hls.recovered, 1);
    assert.deepEqual(x.fired, []);
    x.advance(SAME_INCIDENT_MS + 1);
    assert.equal(x.g.onHlsError(hls, MEDIA('fragParsingError')), true);
    assert.equal(hls.recovered, 1, 'not a second recovery');
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse' }]);
    x.advance(5000);
    x.g.onHlsError(hls, MEDIA());
    assert.equal(x.fired.length, 1, 'once');
});

test('guard: the element\'s decode error makes it decode_error', () => {
    const win = page();
    const p = player(win);
    const hls = fakeHls();
    const x = guarded(win, p, { hls });
    p.setError(3);
    x.g.onHlsError(hls, MEDIA());
    x.advance(SAME_INCIDENT_MS + 1);
    p.setError(null); // recovered: a new MediaSource, no error on the element
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse' }]);
});

test('guard: the element\'s decode error on the hls.js path acts without waiting for hls.js', () => {
    // hls.js does not listen for the element's `error` and learns of it at
    // its next append -- with a full buffer, much later or never.
    const win = page();
    const p = player(win);
    const hls = fakeHls();
    const x = guarded(win, p, { hls });
    p.failWith(3);
    assert.equal(hls.recovered, 1, 'recovered at once');
    // hls.js reports the same failure at its next append: one incident.
    x.advance(100);
    x.g.onHlsError(hls, MEDIA());
    assert.equal(hls.recovered, 1);
    assert.deepEqual(x.fired, []);
    x.advance(SAME_INCIDENT_MS + 1);
    p.failWith(3);
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse' }]);
});

test('guard: a codec string the browser refused gives up at once, with no recovery', () => {
    for (const data of [
        { type: 'mediaError', details: 'manifestIncompatibleCodecsError', fatal: true },
        { type: 'mediaError', details: 'bufferAddCodecError', fatal: true },
    ]) {
        const win = page();
        const hls = fakeHls();
        const x = guarded(win, player(win), { hls });
        assert.equal(x.g.onHlsError(hls, data), true);
        assert.equal(hls.recovered, 0);
        assert.deepEqual(x.fired, [{ reason: 'codecs_rejected', path: 'mse' }]);
    }
    // Not fatal: hls.js deals with it (another level), and so do we not.
    const win = page();
    const hls = fakeHls();
    const x = guarded(win, player(win), { hls });
    assert.equal(x.g.onHlsError(hls, { type: 'mediaError', details: 'bufferAddCodecError', fatal: false }), false);
    assert.deepEqual(x.fired, []);
});

test('guard: network errors and non-fatal ones are not its business', () => {
    const win = page();
    const hls = fakeHls();
    const x = guarded(win, player(win), { hls });
    for (const data of [
        { type: 'networkError', details: 'fragLoadTimeOut', fatal: true },
        { type: 'networkError', details: 'fragLoadError', fatal: true },
        { type: 'mediaError', details: 'bufferStalledError', fatal: false },
    ]) {
        assert.equal(x.g.onHlsError(hls, data), false, data.details);
    }
    assert.equal(hls.recovered, 0);
    assert.deepEqual(x.fired, []);
});

test('guard, native HLS: the element\'s code 3 is decode_error, 4 src_unsupported, 2 nothing', () => {
    for (const [code, want] of [[3, 'decode_error'], [4, 'src_unsupported'], [2, null], [1, null]]) {
        const win = page();
        const p = player(win);
        const x = guarded(win, p);
        p.failWith(code);
        assert.deepEqual(x.fired, want ? [{ reason: want, path: 'native' }] : [], `code ${code}`);
    }
    // On the hls.js path a code 4 is left to hls.js: a detach during its own
    // recovery must not read as a refused source.
    const win = page();
    const p = player(win);
    const x = guarded(win, p, { hls: fakeHls() });
    p.failWith(4);
    assert.deepEqual(x.fired, []);
});

test('watchdog: time ran on with no picture -> no_frames', () => {
    const win = page();
    const p = player(win);
    const x = guarded(win, p);
    p.setWidth(0);
    p.v.dispatchEvent(new win.Event('playing'));
    p.setTime(9);
    x.advance(NO_FRAMES_AFTER_MS);
    assert.deepEqual(x.fired, [{ reason: 'no_frames', path: 'native' }]);
});

test('watchdog: not on a pause, not in a tab that was hidden, not with a picture', () => {
    // Paused: time did not run on.
    let win = page();
    let p = player(win);
    let x = guarded(win, p);
    p.setWidth(0);
    p.v.dispatchEvent(new win.Event('playing'));
    p.setTime(1.5);
    x.advance(NO_FRAMES_AFTER_MS);
    assert.deepEqual(x.fired, [], 'paused');
    // Hidden at some point: a background tab may render nothing.
    win = page();
    p = player(win);
    x = guarded(win, p);
    p.setWidth(0);
    p.v.dispatchEvent(new win.Event('playing'));
    Object.defineProperty(win.document, 'hidden', { get: () => true, configurable: true });
    win.document.dispatchEvent(new win.Event('visibilitychange'));
    Object.defineProperty(win.document, 'hidden', { get: () => false, configurable: true });
    p.setTime(9);
    x.advance(NO_FRAMES_AFTER_MS);
    assert.deepEqual(x.fired, [], 'hidden');
    // A picture, and frames counted: this page's counter works from now on.
    win = page();
    p = player(win);
    x = guarded(win, p);
    p.setFrames(240);
    p.v.dispatchEvent(new win.Event('playing'));
    p.setTime(9);
    x.advance(NO_FRAMES_AFTER_MS);
    assert.deepEqual(x.fired, []);
    assert.equal(framesCounted(win), true);
});

test('watchdog: zero frames with a width counts only where the counter was seen to work', () => {
    // Android Chrome's native player reads 0 frames while it plays.
    let win = page();
    let p = player(win);
    let x = guarded(win, p);
    p.setFrames(0);
    p.v.dispatchEvent(new win.Event('playing'));
    p.setTime(9);
    x.advance(NO_FRAMES_AFTER_MS);
    assert.deepEqual(x.fired, [], 'an unproven counter proves nothing');
    win = page();
    win.__wtFramesCounted = true;
    p = player(win);
    x = guarded(win, p);
    p.setFrames(0);
    p.v.dispatchEvent(new win.Event('playing'));
    p.setTime(9);
    x.advance(NO_FRAMES_AFTER_MS);
    assert.deepEqual(x.fired, [{ reason: 'no_frames', path: 'native' }]);
});

test('guard: fire() is the same once as the automatic path', () => {
    const win = page();
    const p = player(win);
    const x = guarded(win, p);
    assert.equal(x.g.fire('user'), true);
    assert.equal(x.g.fire('user'), false);
    p.failWith(3);
    assert.deepEqual(x.fired, [{ reason: 'user', path: 'native' }]);
});

// ---- the fallback ------------------------------------------------------------

function startForm(win, { rid = 'res1', iid = 'item1' } = {}) {
    const f = win.document.createElement('form');
    f.setAttribute('action', '/ru/stream-video');
    f.setAttribute('method', 'post');
    f.className = 'stream-video';
    f.innerHTML = `<input type="hidden" name="resource-id" value="${rid}"><input type="hidden" name="item-id" value="${iid}">`;
    win.document.body.appendChild(f);
    return f;
}

// The form as the async submit reads it: the fields at submit time.
function captureSubmits(win) {
    const got = [];
    win.document.addEventListener('submit', (e) => {
        e.preventDefault();
        got.push(Object.fromEntries(new win.FormData(e.target)));
    });
    return got;
}

function declaringPage(opts) {
    const win = page(opts);
    try {
        win.localStorage.setItem(OPTIN_KEY, 'on');
        win.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: win.navigator.userAgent, tokens: ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160', 'hdr-pq'], at: Date.now() }));
    } catch (e) { /* no storage */ }
    installSubmitHook(win.document, win);
    return win;
}

test('fallback on the resource page: the start form again, with why, without a declaration', () => {
    const win = declaringPage();
    const form = startForm(win);
    const submits = captureSubmits(win);
    const events = [];
    const p = player(win);
    const how = fallbackToOldRoute({ video: p.v, reason: 'decode_error', path: 'mse', win, doc: win.document, track: (n, d) => events.push({ n, d }) });
    assert.equal(how, 'form');
    assert.equal(submits.length, 1);
    assert.equal(submits[0]['decode-fallback'], 'decode_error');
    assert.equal(submits[0]['decode-class'], 'hevc10-2160');
    assert.equal(submits[0].decode, undefined, 'the old route for this file');
    assert.deepEqual(events, [{ n: 'hevc-fallback', d: { reason: 'decode_error', cls: 'hevc10-2160', path: 'mse' } }]);
    const m = loadMemory(win);
    assert.ok(m.sources['res1/item1'], 'the file is remembered');
    assert.deepEqual(m.strikes['hevc10-2160'].map((s) => s.src), ['res1/item1'], 'a decoder failure strikes its class');
    // Turnstile's second pass carries the same fields.
    form.requestSubmit();
    assert.equal(submits[1]['decode-fallback'], 'decode_error');
    // The restart's player came up: nothing after it carries them, and the
    // file stays on the old route by the memory.
    clearPendingFallback(win);
    form.requestSubmit();
    assert.equal(submits[2]['decode-fallback'], undefined);
    assert.equal(submits[2]['decode-class'], undefined);
    assert.equal(submits[2].decode, undefined);
    // Another file of the page declares as before.
    const other = startForm(win, { iid: 'item2' });
    other.requestSubmit();
    assert.equal(submits[3].decode, 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq');
    assert.equal(submits[3]['decode-fallback'], undefined);
});

test('fallback: the note does not outlive PENDING_TTL_MS', () => {
    const win = declaringPage();
    const form = startForm(win);
    const submits = captureSubmits(win);
    const p = player(win);
    const t0 = Date.now();
    fallbackToOldRoute({ video: p.v, reason: 'user', win, doc: win.document, track: () => {} });
    assert.ok(pendingFallbackFor(win, { resourceId: 'res1', itemId: 'item1' }, t0 + PENDING_TTL_MS - 1000));
    assert.equal(pendingFallbackFor(win, { resourceId: 'res1', itemId: 'item1' }, t0 + PENDING_TTL_MS + 1000), null);
    assert.equal(submits.length, 1);
});

test('fallback: only a decoder failure strikes the class', () => {
    for (const reason of REASONS) {
        const win = declaringPage();
        startForm(win);
        captureSubmits(win);
        const p = player(win);
        fallbackToOldRoute({ video: p.v, reason, win, doc: win.document, track: () => {} });
        const m = loadMemory(win);
        assert.ok(m.sources['res1/item1'], `${reason}: the file is remembered`);
        assert.equal(!!m.strikes['hevc10-2160'], STRIKING.has(reason), reason);
    }
    assert.deepEqual([...STRIKING].sort(), ['decode_error', 'no_frames']);
});

test('fallback after a quiet move to the next file: this file, not the one the page\'s form still names', () => {
    const win = declaringPage();
    startForm(win, { iid: 'ep1' });
    const submits = captureSubmits(win);
    const p = player(win, { iid: 'ep2', path: 'S01/ep2.mkv' });
    const went = [];
    const how = fallbackToOldRoute({ video: p.v, reason: 'no_frames', win, doc: win.document, track: () => {}, navigate: (u) => went.push(u) });
    assert.equal(how, 'navigate');
    assert.equal(submits.length, 0, 'the previous episode is not restarted');
    assert.equal(went.length, 1);
    const u = new URL(went[0], 'https://webtor.io');
    assert.equal(u.pathname, '/ru/res1');
    assert.equal(u.searchParams.get('file'), 'S01/ep2.mkv');
    const h = new URLSearchParams(u.hash.slice(1));
    assert.equal(h.get('action'), 'stream');
    assert.equal(h.get('decode-fallback'), 'no_frames');
    assert.equal(h.get('decode-class'), 'hevc10-2160');
    assert.ok(loadMemory(win).sources['res1/ep2'], 'the file that failed is the one remembered');
});

test('fallbackURL drops file-idx and keeps the rest of the query', () => {
    assert.equal(fallbackURL('https://webtor.io/ru/abc?file-idx=3&x=1#old', 'a b.mkv', 'user', 'hevc8'),
        '/ru/abc?x=1&file=a+b.mkv#action=stream&decode-fallback=user&decode-class=hevc8');
});

test('fallback in an embed: its POST again, with why, no declaration -- not a reload of the old one', () => {
    for (const storageThrows of [false, true]) {
        const win = declaringPage({ url: 'https://webtor.io/embed?id=e1', storageThrows });
        win._embedSettings = { magnet: 'x', lang: 'en' };
        win._CSRF = 'csrf';
        win._sessionID = 'sid';
        const posted = [];
        win.HTMLFormElement.prototype.submit = function () { posted.push(Object.fromEntries(new win.FormData(this))); };
        const p = player(win);
        const how = fallbackToOldRoute({ video: p.v, reason: 'decode_error', win, doc: win.document, track: () => {} });
        assert.equal(how, 'embed');
        assert.equal(posted.length, 1);
        assert.deepEqual(posted[0], {
            _csrf: 'csrf', _sessionID: 'sid', settings: JSON.stringify({ magnet: 'x', lang: 'en' }),
            'decode-fallback': 'decode_error', 'decode-class': 'hevc10-2160',
        }, `storage throws: ${storageThrows}`);
    }
});

test('fallback: a class the stream did not name is unknown, and strikes nothing', () => {
    const win = declaringPage();
    startForm(win);
    const submits = captureSubmits(win);
    const p = player(win, { cls: '' });
    fallbackToOldRoute({ video: p.v, reason: 'decode_error', win, doc: win.document, track: () => {} });
    assert.equal(submits[0]['decode-class'], 'unknown');
    assert.deepEqual(loadMemory(win).strikes, {});
});

test('fallback: the memory key is the declaration module\'s', () => {
    const win = declaringPage();
    startForm(win);
    captureSubmits(win);
    fallbackToOldRoute({ video: player(win).v, reason: 'user', win, doc: win.document, track: () => {} });
    assert.ok(JSON.parse(win.localStorage.getItem(MEMORY_KEY)).sources['res1/item1']);
});
