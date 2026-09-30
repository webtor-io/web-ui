import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import {
    REASONS, STRIKING, AUDIO_STRIKING, SAME_INCIDENT_MS, NO_FRAMES_AFTER_MS, FAULT_TTL_MS, CLEAN_PLAY_S, RELATED_MS,
    passthroughHlsConfig, createPassthroughGuard, createAudioGuard, fallbackToOldRoute, fallbackAudio, fallbackURL, framesCounted,
    audioOfTrack, audioFallbackClass, messageSide, startAudioClass,
} from './passthrough.js';
import {
    MEMORY_KEY, OPTIN_KEY, AUDIO_OPTIN_KEY, CACHE_KEY, AUDIO_CACHE_KEY, PENDING_TTL_MS, loadMemory, installSubmitHook, clearPendingFallback, pendingFallbackFor,
    setPendingFallback, applyAudioUrlSwitch, startProbe,
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
    assert.deepEqual(events, [{ n: 'hevc-fallback', d: { reason: 'decode_error', cls: 'hevc10-2160', path: 'mse', audio: 'none' } }]);
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

// ---- multichannel audio: which side failed, and what the audio is -----------

test('audioOfTrack: Dolby by codec; AAC by its channel count, a PCE (0) counted as more than two', () => {
    for (const [track, want] of [
        [{ codec: 'ec-3', container: 'audio/mp4' }, 'dolby'],
        [{ codec: 'ac-3' }, 'dolby'],
        [{ codec: 'mp4a.40.2', levelCodec: 'ec-3' }, 'dolby'],
        [{ codec: 'mp4a.a6' }, 'dolby'],
        [{ codec: 'mp4a.40.2,hvc1.2.4.L153.90', container: 'video/mp4' }, 'aac'],
        [{ codec: 'ec-3,hvc1.2.4.L153.90' }, 'dolby'],
        [{ codec: 'mp4a.40.2', metadata: { channelCount: 6 } }, 'aac51'],
        [{ codec: 'mp4a.40.2', metadata: { channelCount: 0 } }, 'aac51'],
        [{ codec: 'mp4a.40.2', metadata: { channelCount: 2 } }, null],
        [{ codec: 'mp4a.40.5', metadata: { channelCount: 1 } }, null],
        [{ codec: 'mp4a.40.2' }, 'aac'],
        [{ codec: 'mp4a.40.2', metadata: {} }, 'aac'],
        [{ container: 'audio/mp4' }, undefined],
        [null, undefined],
        [undefined, undefined],
    ]) {
        assert.equal(audioOfTrack(track), want, JSON.stringify(track));
    }
});

// The messages are Chromium's, "<PipelineStatus>: <the media log's first
// error>" (batching_media_log.cc); the decoders' texts from
// decoder_stream.cc, ffmpeg_audio_decoder.cc, ffmpeg_video_decoder.cc;
// WebKit's from HTMLMediaElement.cpp (no player gives a detail). The two
// marked "Chrome 154" are what headless Chrome 154.0.8037.58 on a Mac put in
// MediaError.message on 2026-09-29 (the PCE stand; an fMP4 whose E-AC-3 init
// Chrome cannot take) -- with the group before the code.
test('messageSide: the side a MediaError message names, where it names one', () => {
    for (const [message, want] of [
        ['PIPELINE_ERROR_DECODE: audio decode error!', 'audio'],
        ['PIPELINE_ERROR_DECODE: Failed to send audio packet for decoding: timestamp=0 duration=21333', 'audio'],
        ['DECODER_ERROR_NOT_SUPPORTED: audio decoder initialization failed with DecoderStatus::Codes::kUnsupportedCodec', 'audio'],
        ['PIPELINE_ERROR_DECODE: VideoDecoder error', 'video'],
        ['PIPELINE_ERROR_DECODE: Failed to send video packet for decoding: timestamp=0', 'video'],
        ['CHUNK_DEMUXER_ERROR_APPEND_FAILED: audio and video', null],
        ['CHUNK_DEMUXER_ERROR_APPEND_FAILED: RunSegmentParserLoop: stream parsing failed. Data size=188 append_window_start=0 append_window_end=inf', null],
        // Chrome 154.
        ['PipelineStatus::CHUNK_DEMUXER_ERROR_APPEND_FAILED: RunSegmentParserLoop: stream parsing failed. append_window_start=0 append_window_end=inf', null],
        ['PipelineStatus::CHUNK_DEMUXER_ERROR_APPEND_FAILED: Unsupported audio format 0x65632d33 in stsd box.', 'audio'],
        ['Media failed to decode', null],
        ['', null],
        [undefined, null],
    ]) {
        assert.equal(messageSide({ error: { code: 3, message } }), want, String(message));
    }
    assert.equal(messageSide({ error: null }), null);
    assert.equal(messageSide({ get error() { throw new Error('no'); } }), null);
});

// An audio output that goes away under a playing stream (Bluetooth
// headphones disconnecting): Chromium's AudioRendererImpl::OnRenderError
// logs "audio render error" and fails the pipeline with AUDIO_RENDERER_ERROR,
// a MediaError 3. It names the audio -- but its output, not its decoder.
const OUTPUT_GONE = 'AUDIO_RENDERER_ERROR: audio render error';
test('messageSide: the audio output failing is no side -- not the audio\'s decoder', () => {
    assert.equal(messageSide({ error: { code: 3, message: OUTPUT_GONE } }), null);
    // As Chrome 154 spells the group before the code.
    assert.equal(messageSide({ error: { code: 3, message: `PipelineStatus::${OUTPUT_GONE}` } }), null);
    // Whatever the media log saw first: the code decides.
    assert.equal(messageSide({ error: { code: 3, message: 'AUDIO_RENDERER_ERROR: Output device error, falling back to null sink. device_status=1' } }), null);
});

// The events' audio class of a start: what its declaration made of the audio
// (the master's word), only where it declared an audio token.
test('startAudioClass: the master\'s class where the start declared audio, else none', () => {
    const win = page();
    for (const [decode, audioClass, want] of [
        [DECL_AV, 'dolby', 'dolby'],
        [DECL_AV, 'aac51', 'aac51'],
        [DECL_AV, '', 'none'],
        ['aac51', 'aac51', 'aac51'],
        ['hevc8,hevc10', 'dolby', 'none'],
        ['', 'aac51', 'none'],
        [DECL_AV, 'hevc10', 'none'],
        [DECL_AV, 'constructor', 'none'],
    ]) {
        const p = avPlayer(win, { decode, audioClass });
        assert.equal(startAudioClass(p.v), want, `${decode} / ${audioClass}`);
    }
    assert.equal(startAudioClass(null), 'none');
    assert.equal(startAudioClass({ get dataset() { throw new Error('no'); } }), 'none');
});

// What blamed the audio travels with the audio's fallback (the event's
// `by`), and nothing with the video's.
test('the guards say what blamed the audio: its buffer, its codec, the element\'s message, the native rule', () => {
    const got = [];
    const capture = (make) => clocked(page(), (o) => make({ ...o, fallback: (...a) => got.push(a) }));
    // Passthrough, the audio buffer's append.
    let win = page();
    let p = avPlayer(win);
    let hls = p.hls();
    let x = clocked(win, (o) => { const g = createPassthroughGuard({ video: p.v, win, doc: win.document, ...o, fallback: (...a) => got.push(a) }); g.setHls(hls); return g; });
    x.g.onBufferCodecs(EC3);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    x.advance(SAME_INCIDENT_MS + 1);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(got.pop(), ['media_error', 'mse', 'dolby', 'buffer']);
    // Passthrough, the audio codec refused.
    win = page();
    p = avPlayer(win);
    hls = p.hls();
    x = clocked(win, (o) => { const g = createPassthroughGuard({ video: p.v, win, doc: win.document, ...o, fallback: (...a) => got.push(a) }); g.setHls(hls); return g; });
    x.g.onBufferCodecs(EC3);
    x.g.onHlsError(hls, { type: 'mediaError', details: 'bufferAddCodecError', sourceBufferName: 'audio', fatal: true });
    assert.deepEqual(got.pop(), ['codecs_rejected', 'mse', 'dolby', 'codec']);
    // Passthrough, native: the element's message.
    win = page();
    p = avPlayer(win, { audioClass: 'aac51' });
    x = clocked(win, (o) => createPassthroughGuard({ video: p.v, win, doc: win.document, ...o, fallback: (...a) => got.push(a) }));
    p.failWith(3, 'PIPELINE_ERROR_DECODE: audio decode error!');
    assert.deepEqual(got.pop(), ['decode_error', 'native', 'aac51', 'message']);
    // Dolby charged first with nothing pinned: `by` says so.
    win = page();
    p = avPlayer(win, { audioClass: 'dolby' });
    x = clocked(win, (o) => createPassthroughGuard({ video: p.v, win, doc: win.document, ...o, fallback: (...a) => got.push(a) }));
    p.failWith(3);
    assert.deepEqual(got.pop(), ['decode_error', 'native', 'dolby', 'unpinned']);
    // The video's fallback: no `by`.
    win = page();
    p = avPlayer(win, { audioClass: 'aac51' });
    x = clocked(win, (o) => createPassthroughGuard({ video: p.v, win, doc: win.document, ...o, fallback: (...a) => got.push(a) }));
    p.failWith(3);
    assert.deepEqual(got.pop(), ['decode_error', 'native', null, null]);
    // The audio guard: the buffer's pin, and the native rule.
    win = page();
    p = avPlayer(win, { route: 'reencode', decode: 'aac51', audioClass: 'aac51' });
    hls = p.hls();
    x = capture((o) => { const g = createAudioGuard({ video: p.v, ...o }); g.setHls(hls); return g; });
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(got.pop(), ['media_error', 'mse', 'aac51', 'buffer']);
    win = page();
    p = avPlayer(win, { route: 'reencode', decode: 'aac51', audioClass: 'aac51' });
    x = capture((o) => createAudioGuard({ video: p.v, ...o }));
    p.failWith(4);
    assert.deepEqual(got.pop(), ['src_unsupported', 'native', 'aac51', 'native']);
    assert.equal(got.length, 0);
});

test('audio guard: an audio output that went away is not the audio failing -- hls-manager\'s, no restart, no strike', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', container: 'audio/mp4', metadata: { channelCount: 6 } } });
    for (let i = 0; i < 3; i++) {
        // The element fails with the output's error; hls.js learns of it at
        // its next append (the MediaSource has ended) -- a fatal media error.
        p.setError(3, OUTPUT_GONE);
        assert.equal(x.g.onHlsError(hls, MEDIA()), false, `failure ${i + 1}: left to hls-manager.js`);
        x.advance(SAME_INCIDENT_MS + 1);
    }
    assert.equal(hls.recovered, 0, 'recovered by hls-manager, not by the guard');
    assert.deepEqual(x.fired, []);
});

test('passthrough guard: an audio output that went away is not the AAC 5.1\'s; with Dolby, Dolby first', () => {
    for (const [audioClass, want] of [['aac51', null], ['dolby', 'dolby']]) {
        const win = page();
        const p = avPlayer(win, { audioClass });
        const hls = p.hls();
        const x = ptGuard(win, p, hls);
        p.failWith(3, OUTPUT_GONE);
        assert.equal(hls.recovered, 1);
        x.advance(SAME_INCIDENT_MS + 1);
        p.failWith(3, OUTPUT_GONE);
        // The passthrough guard's own rule reads the MediaError 3 as the
        // decoder's; nothing pins it on a side, so with Dolby in play
        // Dolby goes first (a restart without it, the video keeps its route)
        // -- cheaper than the HEVC strike it was before.
        assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse', audio: want }], audioClass);
    }
});

test('audioFallbackClass: the audio\'s only where it is blamed -- Dolby or not -- never the picture\'s or the viewer\'s', () => {
    const REASONS_AUDIO = ['codecs_rejected', 'decode_error', 'media_error', 'src_unsupported'];
    for (const reason of REASONS_AUDIO) {
        assert.equal(audioFallbackClass({ reason, fault: 'audio', audio: 'aac51' }), 'aac51', reason);
        assert.equal(audioFallbackClass({ reason, fault: 'audio', audio: 'dolby' }), 'dolby', reason);
        assert.equal(audioFallbackClass({ reason, fault: null, audio: 'dolby' }), 'dolby', `${reason}: Dolby in play, nobody blamed -- Dolby first, the cheap wrong answer`);
        assert.equal(audioFallbackClass({ reason, fault: null, audio: 'aac51' }), null, `${reason}: AAC 5.1 nobody blamed is the video's`);
        assert.equal(audioFallbackClass({ reason, fault: 'video', audio: 'dolby' }), null, `${reason}: the video's`);
        assert.equal(audioFallbackClass({ reason, fault: 'audio', audio: null }), null, `${reason}: audio the declaration did not change`);
    }
    // fragment_loop: a video fragment the browser dropped (fragment-loop.js).
    for (const reason of ['no_frames', 'user', 'fragment_loop']) {
        assert.equal(audioFallbackClass({ reason, fault: 'audio', audio: 'dolby' }), null, reason);
    }
    assert.equal(audioFallbackClass({ reason: 'decode_error', fault: 'audio', audio: 'hevc10' }), null, 'not an audio class');
});

// A player whose error carries a message, and an hls.js whose recovery
// reloads the element -- which clears its error (the media element load
// algorithm; hls.js detachMedia calls load()).
// decode: the declaration the start sent -- by default one with every audio
// token (every browser that answers Dolby, since the audio's stage 5).
const DECL_AV = 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq,aac51,ac3,ec3';
function avPlayer(win, { route = 'passthrough', audioClass = '', cls = 'hevc10-2160', decode = DECL_AV } = {}) {
    const v = win.document.createElement('video');
    v.dataset.videoRoute = route;
    if (route === 'passthrough') v.dataset.videoClass = cls;
    if (audioClass) v.dataset.audioClass = audioClass;
    if (decode) v.dataset.decode = decode;
    v.dataset.resourceId = 'res1';
    v.dataset.itemId = 'item1';
    v.dataset.path = 'a.mkv';
    win.document.body.appendChild(v);
    let err = null;
    Object.defineProperty(v, 'error', { get: () => err, configurable: true });
    let ct = 0;
    Object.defineProperty(v, 'currentTime', { get: () => ct, set: (x) => { ct = x; }, configurable: true });
    const tick = () => v.dispatchEvent(new win.Event('timeupdate'));
    return {
        v,
        failWith: (code, message = '') => { err = code ? { code, message } : null; v.dispatchEvent(new win.Event('error')); },
        setError: (code, message = '') => { err = code ? { code, message } : null; },
        hls: () => ({
            recovered: 0, stopped: 0, detached: 0,
            recoverMediaError() { this.recovered++; err = null; },
            stopLoad() { this.stopped++; },
            detachMedia() { this.detached++; },
        }),
        // plays `s` seconds of media: a timeupdate every 250 ms of it.
        play: (s) => { tick(); for (let i = 0; i < s * 4; i++) { ct += 0.25; tick(); } },
        // jumps (a seek): a single step, no playback.
        seek: (to) => { ct = to; tick(); },
    };
}

function clocked(win, make) {
    let t = 0;
    const timers = [];
    const fired = [];
    const g = make({
        now: () => t,
        setTimer: (fn, ms) => { timers.push({ fn, at: t + ms }); return timers.length; },
        clearTimer: (id) => { if (timers[id - 1]) timers[id - 1].fn = null; },
        fallback: (reason, path, audio) => fired.push({ reason, path, audio }),
    });
    return {
        g, fired,
        advance: (ms) => {
            t += ms;
            for (const x of timers) if (x.fn && x.at <= t) { const fn = x.fn; x.fn = null; fn(); }
        },
    };
}
const ptGuard = (win, p, hls) => clocked(win, (o) => {
    const g = createPassthroughGuard({ video: p.v, win, doc: win.document, ...o });
    if (hls) g.setHls(hls);
    return g;
});
const auGuard = (win, p, hls) => clocked(win, (o) => {
    const g = createAudioGuard({ video: p.v, ...o });
    if (hls) g.setHls(hls);
    return g;
});

const APPENDING = (sb) => ({ type: 'mediaError', details: 'bufferAppendingError', sourceBufferName: sb, fatal: false });
const EC3 = { audio: { codec: 'ec-3', container: 'audio/mp4', id: 'audio' } };

// Dolby in play and nobody blaming a side: Dolby first. A false `dolby`
// strike costs AAC 5.1 instead of Dolby for 7 days; a false HEVC strike
// costs passthrough and 4K for 7 days -- the cheap wrong answer goes first,
// and a video that was the one failing fails again without Dolby
// (passthrough guard: the restart is the audio's, the video keeps its route).
test('passthrough guard: Dolby in play and nobody blamed -- Dolby first, recovered once as before', () => {
    const win = page();
    const p = avPlayer(win);
    const hls = p.hls();
    const x = ptGuard(win, p, hls);
    x.g.onBufferCodecs(EC3);
    p.failWith(3);
    assert.equal(hls.recovered, 1, 'recovered once, as before');
    x.advance(SAME_INCIDENT_MS + 1);
    p.failWith(3);
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse', audio: 'dolby' }]);
});

test('passthrough guard, native HLS: the master\'s Dolby with nothing to name a side -- Dolby first', () => {
    for (const [code, reason] of [[3, 'decode_error'], [4, 'src_unsupported']]) {
        const win = page();
        const p = avPlayer(win, { audioClass: 'dolby' });
        const x = ptGuard(win, p);
        p.failWith(code);
        assert.deepEqual(x.fired, [{ reason, path: 'native', audio: 'dolby' }], String(code));
    }
});

// A failure pinned on the video is the video's, Dolby in play or not.
test('passthrough guard: Dolby in play, the video\'s own buffer failing -- the video\'s', () => {
    const win = page();
    const p = avPlayer(win);
    const hls = p.hls();
    const x = ptGuard(win, p, hls);
    x.g.onBufferCodecs(EC3);
    x.g.onHlsError(hls, APPENDING('video'));
    p.failWith(3);
    x.advance(SAME_INCIDENT_MS + 1);
    x.g.onHlsError(hls, APPENDING('video'));
    p.failWith(3);
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse', audio: null }]);
});

// The evidence that still charges Dolby: the audio buffer's own append
// failing, and the element's message naming the audio's decoder (the audio
// codec refused: below).
test('passthrough guard: Dolby is charged where the audio is blamed -- its buffer\'s append, or the element\'s word', () => {
    let win = page();
    let p = avPlayer(win);
    const hls = p.hls();
    let x = ptGuard(win, p, hls);
    x.g.onBufferCodecs(EC3);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    x.advance(SAME_INCIDENT_MS + 1);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'dolby' }]);
    win = page();
    p = avPlayer(win, { audioClass: 'dolby' });
    x = ptGuard(win, p);
    p.failWith(3, 'PIPELINE_ERROR_DECODE: audio decode error!');
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'native', audio: 'dolby' }]);
});

test('passthrough guard: a failure pinned on the video stays the video\'s, Dolby or not', () => {
    // hls.js names the buffer whose append failed.
    let win = page();
    let p = avPlayer(win);
    let hls = p.hls();
    let x = ptGuard(win, p, hls);
    x.g.onBufferCodecs(EC3);
    x.g.onHlsError(hls, MEDIA());
    x.advance(SAME_INCIDENT_MS + 1);
    x.g.onHlsError(hls, APPENDING('video'));
    p.setError(3);
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse', audio: null }]);
    // The element's message names the video.
    win = page();
    p = avPlayer(win, { audioClass: 'dolby' });
    x = ptGuard(win, p);
    p.failWith(3, 'PIPELINE_ERROR_DECODE: video decoder reinitialization failed');
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'native', audio: null }]);
    // No picture is the video's.
    win = page();
    p = avPlayer(win, { audioClass: 'dolby' });
    x = ptGuard(win, p);
    Object.defineProperty(p.v, 'videoWidth', { get: () => 0, configurable: true });
    let ct = 0;
    Object.defineProperty(p.v, 'currentTime', { get: () => ct, configurable: true });
    p.v.dispatchEvent(new win.Event('playing'));
    ct = 9;
    x.advance(NO_FRAMES_AFTER_MS);
    assert.deepEqual(x.fired, [{ reason: 'no_frames', path: 'native', audio: null }]);
});

test('passthrough guard: AAC 5.1 is charged only where the audio is blamed', () => {
    // Nobody blamed: the video's, as before multichannel audio.
    let win = page();
    let p = avPlayer(win, { audioClass: 'aac51' });
    let x = ptGuard(win, p);
    p.failWith(3);
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'native', audio: null }]);
    // hls.js names the audio buffer: the audio's -- media_error included.
    win = page();
    p = avPlayer(win, { audioClass: 'aac51' });
    const hls = p.hls();
    x = ptGuard(win, p, hls);
    x.g.onHlsError(hls, MEDIA());
    x.advance(SAME_INCIDENT_MS + 1);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }]);
    // The element's message names the audio.
    win = page();
    p = avPlayer(win, { audioClass: 'aac51' });
    x = ptGuard(win, p);
    p.failWith(3, 'PIPELINE_ERROR_DECODE: audio decode error!');
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'native', audio: 'aac51' }]);
});

test('passthrough guard: what hls.js buffers outranks the master; an AAC track of unknown count is the master\'s AAC 5.1 or nothing', () => {
    // The master names Dolby (another rendition); the one in play is stereo AAC.
    let win = page();
    let p = avPlayer(win, { audioClass: 'dolby' });
    let hls = p.hls();
    let x = ptGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', metadata: { channelCount: 2 } } });
    x.g.onHlsError(hls, MEDIA());
    x.advance(SAME_INCIDENT_MS + 1);
    p.setError(3);
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse', audio: null }]);
    // An fMP4 AAC track (no channel count) where the master names Dolby:
    // not the Dolby -- another rendition -- so nobody blamed is the video's,
    // and the audio blamed is AAC 5.1's.
    for (const [pin, want] of [[null, null], ['audio', 'aac51']]) {
        win = page();
        p = avPlayer(win, { audioClass: 'dolby' });
        hls = p.hls();
        x = ptGuard(win, p, hls);
        x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', container: 'audio/mp4' } });
        x.g.onBufferCodecs({ video: { codec: 'hvc1.2.4.L153.90' } });
        x.g.onHlsError(hls, MEDIA());
        x.advance(SAME_INCIDENT_MS + 1);
        if (pin) x.g.onHlsError(hls, APPENDING(pin));
        p.setError(3);
        x.g.onHlsError(hls, MEDIA());
        assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse', audio: want }], `pinned on ${pin}`);
    }
    // No master word: an AAC of unknown count is the stereo of old.
    win = page();
    p = avPlayer(win);
    hls = p.hls();
    x = ptGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', container: 'audio/mp4' } });
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    x.advance(SAME_INCIDENT_MS + 1);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: null }]);
});

// A refused manifest names no codec: hls.js drops every variant one of whose
// CODECS its MediaSource refuses (level-controller.ts). With Dolby in the
// master, Dolby first: the restart without it settles which codec it was
// (refused again -- the video's), where the video's fallback would have sent
// a 4K film to the old route's refusal.
test('passthrough guard: the audio codec refused is the audio\'s; a refused manifest with Dolby -- Dolby first', () => {
    let win = page();
    let p = avPlayer(win);
    let hls = p.hls();
    let x = ptGuard(win, p, hls);
    x.g.onBufferCodecs(EC3);
    x.g.onHlsError(hls, { type: 'mediaError', details: 'bufferAddCodecError', sourceBufferName: 'audio', fatal: true });
    assert.deepEqual(x.fired, [{ reason: 'codecs_rejected', path: 'mse', audio: 'dolby' }]);
    win = page();
    p = avPlayer(win, { audioClass: 'dolby' });
    hls = p.hls();
    x = ptGuard(win, p, hls);
    x.g.onHlsError(hls, { type: 'mediaError', details: 'bufferAddCodecError', sourceBufferName: 'video', fatal: true });
    assert.deepEqual(x.fired, [{ reason: 'codecs_rejected', path: 'mse', audio: null }], 'the video\'s codec refused');
    win = page();
    p = avPlayer(win, { audioClass: 'dolby' });
    hls = p.hls();
    x = ptGuard(win, p, hls);
    x.g.onHlsError(hls, { type: 'mediaError', details: 'manifestIncompatibleCodecsError', fatal: true });
    assert.deepEqual(x.fired, [{ reason: 'codecs_rejected', path: 'mse', audio: 'dolby' }]);
});

test('passthrough guard: a pin is forgotten at the recovery, and after FAULT_TTL_MS', () => {
    // Pinned on the audio before the recovery: the next failure is nobody's.
    let win = page();
    let p = avPlayer(win, { audioClass: 'aac51' });
    let hls = p.hls();
    let x = ptGuard(win, p, hls);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.equal(hls.recovered, 1);
    x.advance(SAME_INCIDENT_MS + 1);
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: null }]);
    // An old pin (hls.js dealt with that one itself) blames nothing now.
    win = page();
    p = avPlayer(win, { audioClass: 'aac51' });
    x = ptGuard(win, p);
    x.g.onHlsError(null, APPENDING('audio'));
    x.advance(FAULT_TTL_MS + 1);
    p.failWith(3);
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'native', audio: null }]);
});

// A report within SAME_INCIDENT_MS after the one recovery is either the
// same incident told twice or the new attachment failing at once -- and
// then nothing else comes: hls.js stops loading on a fatal error. The
// element tells them apart at the window's end.
test('guard: a failure right after the recovery is looked at again at the window\'s end', () => {
    // The recovered attachment fails again at once and the element says so.
    let win = page();
    let p = avPlayer(win, { audioClass: 'dolby' });
    let hls = p.hls();
    let x = ptGuard(win, p, hls);
    p.failWith(3);
    assert.equal(hls.recovered, 1);
    x.advance(200);
    p.failWith(3);
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [], 'within the window: not yet');
    x.advance(SAME_INCIDENT_MS);
    assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse', audio: 'dolby' }], 'given up (Dolby first: nothing pinned)');
    // The same incident told twice: the recovery cleared the element, and
    // it stays clear -- nothing happens.
    win = page();
    p = avPlayer(win, { audioClass: 'dolby' });
    hls = p.hls();
    x = ptGuard(win, p, hls);
    p.failWith(3);
    x.advance(100);
    x.g.onHlsError(hls, MEDIA());
    x.advance(SAME_INCIDENT_MS * 3);
    assert.deepEqual(x.fired, []);
    assert.equal(hls.recovered, 1);
});

// ---- the fallback of the audio -----------------------------------------------

function audioDeclaringPage(opts) {
    const win = declaringPage(opts);
    try {
        win.localStorage.setItem(AUDIO_OPTIN_KEY, 'on');
        win.localStorage.setItem(AUDIO_CACHE_KEY, JSON.stringify({ ua: win.navigator.userAgent, tokens: ['aac51', 'ac3', 'ec3'], at: Date.now() }));
    } catch (e) { /* no storage */ }
    return win;
}
const VIDEO = 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq';

test('fallbackAudio, dolby: the start form again without Dolby -- the video keeps its route -- and a strike', () => {
    const win = audioDeclaringPage();
    const form = startForm(win);
    const submits = captureSubmits(win);
    const events = [];
    const p = avPlayer(win, { audioClass: 'dolby' });
    const how = fallbackAudio({ video: p.v, reason: 'decode_error', cls: 'dolby', path: 'mse', by: 'buffer', win, doc: win.document, track: (n, d) => events.push({ n, d }) });
    assert.equal(how, 'form');
    assert.deepEqual(submits[0], { 'resource-id': 'res1', 'item-id': 'item1', decode: `${VIDEO},aac51`, 'decode-fallback': 'decode_error', 'decode-class': 'dolby' });
    assert.deepEqual(events, [{ n: 'audio-fallback', d: { reason: 'decode_error', cls: 'dolby', path: 'mse', route: 'passthrough', audio: 'dolby', by: 'buffer' } }]);
    const m = loadMemory(win);
    assert.deepEqual(Object.keys(m.audio), ['res1/item1']);
    assert.deepEqual(m.sources, {}, 'the file keeps its declaration');
    assert.deepEqual(m.strikes.dolby.map((x) => x.src), ['res1/item1']);
    assert.equal(m.strikes['hevc10-2160'], undefined, 'the video class is not struck');
    // The restart's player came up: the file stays without Dolby by the memory.
    clearPendingFallback(win);
    form.requestSubmit();
    assert.equal(submits[1].decode, `${VIDEO},aac51`);
    assert.equal(submits[1]['decode-fallback'], undefined);
});

test('fallbackAudio, aac51 on the old route: without any audio token, the video tokens as before', () => {
    const win = audioDeclaringPage();
    startForm(win);
    const submits = captureSubmits(win);
    const events = [];
    const p = avPlayer(win, { route: 'reencode', audioClass: 'aac51', decode: `${VIDEO},aac51,ac3,ec3` });
    fallbackAudio({ video: p.v, reason: 'media_error', cls: 'aac51', path: 'mse', win, doc: win.document, track: (n, d) => events.push({ n, d }) });
    assert.equal(submits[0].decode, VIDEO);
    assert.equal(submits[0]['decode-class'], 'aac51');
    assert.deepEqual(events[0].d, { reason: 'media_error', cls: 'aac51', path: 'mse', route: 'reencode', audio: 'aac51', by: '' }, 'no `by` given: empty');
    assert.deepEqual(loadMemory(win).strikes.aac51.map((x) => x.src), ['res1/item1']);
});

test('fallbackAudio: only the decoder\'s and the media\'s failures strike the audio class', () => {
    for (const reason of REASONS) {
        const win = audioDeclaringPage();
        startForm(win);
        captureSubmits(win);
        fallbackAudio({ video: avPlayer(win, { audioClass: 'dolby' }).v, reason, cls: 'dolby', win, doc: win.document, track: () => {} });
        const m = loadMemory(win);
        assert.ok(m.audio['res1/item1'].dolby, `${reason}: the file is remembered`);
        assert.equal(!!m.strikes.dolby, AUDIO_STRIKING.has(reason), reason);
    }
    assert.deepEqual([...AUDIO_STRIKING].sort(), ['decode_error', 'media_error']);
});

// Without storage (stage 5: such a browser takes part) the embed's own
// page holds what it knows: the ?audio=on of its address and its probe's
// answer -- the restart keeps them.
test('fallbackAudio in an embed: its POST again, with the declaration it keeps', async () => {
    for (const storageThrows of [false, true]) {
        const win = audioDeclaringPage({ url: `https://webtor.io/embed?id=e1${storageThrows ? '&audio=on' : ''}`, storageThrows });
        if (storageThrows) {
            applyAudioUrlSwitch(win);
            await startProbe(win, { load: async () => ({ declarationSupport: () => ({ path: 'mse', hevc: ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160'], pq: Promise.resolve(true), audio: Promise.resolve(['aac51', 'ac3', 'ec3']) }), envFromWindow: () => ({}) }) });
            await win.__wtDecode.audio;
        }
        win._embedSettings = { magnet: 'x' };
        win._CSRF = 'csrf';
        win._sessionID = 'sid';
        const posted = [];
        win.HTMLFormElement.prototype.submit = function () { posted.push(Object.fromEntries(new win.FormData(this))); };
        const how = fallbackAudio({ video: avPlayer(win, { audioClass: 'dolby' }).v, reason: 'decode_error', cls: 'dolby', win, doc: win.document, track: () => {} });
        assert.equal(how, 'embed');
        assert.equal(posted[0].decode, `${VIDEO},aac51`, `storage throws: ${storageThrows}`);
        assert.equal(posted[0]['decode-class'], 'dolby');
        assert.equal(posted[0]['decode-fallback'], 'decode_error');
    }
});

test('fallbackAudio after a quiet move to the next file: the deep link carries the class, and the start it makes leaves Dolby out', () => {
    const win = audioDeclaringPage();
    startForm(win, { iid: 'ep1' });
    const submits = captureSubmits(win);
    const p = avPlayer(win, { audioClass: 'dolby' });
    p.v.dataset.itemId = 'ep2';
    p.v.dataset.path = 'S01/ep2.mkv';
    const went = [];
    const how = fallbackAudio({ video: p.v, reason: 'decode_error', cls: 'dolby', win, doc: win.document, track: () => {}, navigate: (u) => went.push(u) });
    assert.equal(how, 'navigate');
    assert.equal(submits.length, 0);
    const h = new URLSearchParams(new URL(went[0], 'https://webtor.io').hash.slice(1));
    assert.equal(h.get('decode-class'), 'dolby');
    // The page that link loads (app/resource/get.js) sets the note from
    // the hash; its start of ep2 declares the rest -- here without storage
    // of the first page's memory: the note alone does it.
    const next = audioDeclaringPage();
    next.localStorage.removeItem(MEMORY_KEY);
    const f = startForm(next, { iid: 'ep2' });
    const got = captureSubmits(next);
    setPendingFallback(next, { resourceId: 'res1', itemId: 'ep2', reason: h.get('decode-fallback'), cls: h.get('decode-class') });
    f.requestSubmit();
    assert.equal(got[0].decode, `${VIDEO},aac51`);
    assert.equal(got[0]['decode-class'], 'dolby');
});

test('fallbackAudio with a class that is not the audio\'s is the video\'s fallback', () => {
    const win = audioDeclaringPage();
    startForm(win);
    const submits = captureSubmits(win);
    const events = [];
    fallbackAudio({ video: avPlayer(win).v, reason: 'decode_error', cls: 'hevc10', win, doc: win.document, track: (n, d) => events.push(n) });
    assert.equal(submits[0].decode, undefined);
    assert.deepEqual(events, ['hevc-fallback']);
});

// ---- the old route with multichannel audio (createAudioGuard) ---------------

// Finding 4, the measured case: AAC whose layout is in a PCE, copied into
// MPEG-TS for a browser that declared aac51. Chrome refuses the audio
// append (MediaError 4, CHUNK_DEMUXER_ERROR_APPEND_FAILED; hls.js
// bufferAppendingError on the audio buffer, then a fatal
// bufferAppendError), and a recovery fails the same way.
test('audio guard: a PCE on the old route -- recovered once, then given up to a restart without audio tokens', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', container: 'audio/mp4', metadata: { channelCount: 0 } } });
    const failOnce = () => {
        assert.equal(x.g.onHlsError(hls, APPENDING('audio')), false, 'a non-fatal error is hls-manager\'s, as ever');
        p.failWith(4, 'CHUNK_DEMUXER_ERROR_APPEND_FAILED: Failed to prepare audio sample');
        return x.g.onHlsError(hls, MEDIA());
    };
    assert.equal(failOnce(), true);
    assert.equal(hls.recovered, 1);
    x.advance(SAME_INCIDENT_MS + 1);
    assert.equal(failOnce(), true);
    assert.equal(hls.recovered, 1, 'once');
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }]);
});

// The same, with nothing but hls.js's pin to say it is the audio (a
// MediaError message that names no stream), and a seek between the two:
// a jump is not playback, so the recovery has shown nothing.
test('audio guard: the PCE case needs only the audio buffer\'s pin; a seek in between is no clean playback', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51` });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', container: 'audio/mp4', metadata: { channelCount: 0 } } });
    const failOnce = () => {
        x.g.onHlsError(hls, APPENDING('audio'));
        p.setError(4, 'CHUNK_DEMUXER_ERROR_APPEND_FAILED: RunSegmentParserLoop: stream parsing failed.');
        return x.g.onHlsError(hls, MEDIA());
    };
    p.play(1);
    assert.equal(failOnce(), true);
    assert.equal(hls.recovered, 1);
    p.seek(600);
    x.advance(20000);
    assert.equal(failOnce(), true);
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }]);
});

// Each report of the audio guard is a failure of its own (the recovery
// forgets the pin): the re-failure right after the recovery gives up at
// once, not at the end of a same-incident window.
test('audio guard: the re-failure right after the recovery gives up at once', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: 'aac51', audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    x.advance(150);
    p.setError(4);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }]);
    assert.equal(hls.recovered, 1);
});

// What Chrome 154 with hls.js 1.6.14 did with the PCE stream in a muxed
// media playlist (reproduced 2026-09-29 with ffmpeg -aac_pce): after the
// recovery the element's error is not set yet when the append fails, so
// buffer-controller.ts makes the bufferAppendError non-fatal, and hls.js's
// error controller recovers the ended MediaSource by itself
// (error-controller.ts onErrorOut) before any listener of ours -- ~1 000
// times a second, never fatal again. The guard counts those, gives up at the
// next one, and stops hls.js. The event as hls.js hands it to listeners:
// resolved by content steering, the SourceBuffer's own message.
const SELF_RECOVERED = () => ({ type: 'mediaError', details: 'bufferAppendError', sourceBufferName: 'audio', fatal: false,
    error: new Error('audio SourceBuffer error. MediaSource readyState: ended'), errorAction: { action: 2, flags: 1, resolved: true } });
// The same append error on the old route's real master (content-transcoder
// golden_old_route.json: an EXT-X-MEDIA audio rendition beside the video
// level), Chrome 154 + hls.js 1.6.14, reproduced 2026-09-29 by both reviews
// of the audio stage 5: the SourceBuffer's error event comes while the
// MediaSource still reads `open`. onErrorOut takes it as resolved and
// recovers nothing; the element holds MediaError 4 and no error follows.
const UNRECOVERED = () => ({ ...SELF_RECOVERED(), error: new Error('audio SourceBuffer error. MediaSource readyState: open') });
test('audio guard: a failure hls.js recovered by itself counts; the next gives up and stops hls.js', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51` });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', container: 'audio/mp4', metadata: { channelCount: 0 } } });
    // The first failure, fatal: the guard's recovery.
    x.g.onHlsError(hls, APPENDING('audio'));
    p.setError(4, 'PipelineStatus::CHUNK_DEMUXER_ERROR_APPEND_FAILED: RunSegmentParserLoop: stream parsing failed.');
    assert.equal(x.g.onHlsError(hls, MEDIA()), true);
    assert.equal(hls.recovered, 1);
    // The recovered attachment fails; hls.js recovers it itself.
    x.advance(5);
    x.g.onHlsError(hls, APPENDING('audio'));
    assert.equal(x.g.onHlsError(hls, SELF_RECOVERED()), false, 'a non-fatal error: hls-manager only warns');
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }]);
    assert.equal(hls.recovered, 1, 'no recovery of the guard\'s own on top of hls.js\'s');
    assert.deepEqual([hls.stopped, hls.detached], [1, 1], 'hls.js stopped: its own recovery re-attaches no more');
    // Nothing after it is the guard's to act on.
    x.advance(5);
    x.g.onHlsError(hls, APPENDING('audio'));
    assert.equal(x.g.onHlsError(hls, SELF_RECOVERED()), true);
    assert.equal(x.fired.length, 1);
});

test('audio guard: the first failure recovered by hls.js itself is the one recovery', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, SELF_RECOVERED());
    assert.equal(hls.recovered, 0, 'hls.js recovered it: the guard makes no recovery of its own');
    assert.deepEqual(x.fired, []);
    x.advance(3);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, SELF_RECOVERED());
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }]);
    // Without the pin a non-fatal append error is nobody's.
    const w2 = page();
    const q = avPlayer(w2, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const h2 = q.hls();
    const y = auGuard(w2, q, h2);
    for (let i = 0; i < 5; i++) {
        y.advance(FAULT_TTL_MS + 1);
        y.g.onHlsError(h2, SELF_RECOVERED());
    }
    assert.deepEqual(y.fired, []);
});

// The old route's real shape: the first failure of the audio comes
// non-fatal and `open`, and hls.js recovers nothing. Without the guard's
// own recovery the player stays dead -- no restart, no memory.
test('audio guard: a non-fatal append error hls.js did not recover (readyState open) is the guard\'s to recover; the next gives up', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51` });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', container: 'audio/mp4', metadata: { channelCount: 0 } } });
    x.g.onHlsError(hls, APPENDING('audio'));
    p.setError(4, 'PipelineStatus::CHUNK_DEMUXER_ERROR_APPEND_FAILED: RunSegmentParserLoop: stream parsing failed.');
    assert.equal(x.g.onHlsError(hls, UNRECOVERED()), true, 'handled: hls-manager has nothing to add to it');
    assert.equal(hls.recovered, 1, 'nobody else recovers it');
    assert.deepEqual(x.fired, []);
    // The recovered attachment fails the same way, `open` again or `ended`.
    for (const again of [UNRECOVERED, SELF_RECOVERED]) {
        const w = page();
        const q = avPlayer(w, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
        const h = q.hls();
        const y = auGuard(w, q, h);
        y.g.onHlsError(h, APPENDING('audio'));
        y.g.onHlsError(h, UNRECOVERED());
        y.advance(5);
        y.g.onHlsError(h, APPENDING('audio'));
        y.g.onHlsError(h, again());
        assert.deepEqual(y.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }], again.name);
        assert.equal(h.recovered, 1, again.name);
        assert.deepEqual([h.stopped, h.detached], [1, 1], again.name);
    }
    // Without the pin it is nobody's, as ever.
    const w3 = page();
    const r = avPlayer(w3, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const h3 = r.hls();
    const z = auGuard(w3, r, h3);
    assert.equal(z.g.onHlsError(h3, UNRECOVERED()), false);
    assert.equal(h3.recovered, 0);
});

// The internal recovery these tests stand in for is the installed hls.js's:
// an upgrade that drops or changes it must be looked at again.
test('audio guard: the installed hls.js recovers an ended MediaSource by itself after a non-fatal append error, and only that', async () => {
    const { readFile } = await import('node:fs/promises');
    const read = (p) => readFile(new URL(`../../../../../node_modules/hls.js/src/${p}`, import.meta.url), 'utf8');
    const src = await read('controller/error-controller.ts');
    const out = src.slice(src.indexOf('public onErrorOut('), src.indexOf('private sendAlternateToPenaltyBox('));
    // The one recoverMediaError of onErrorOut, and the condition over it:
    // resolved (else fatal) and `ended` -- an `open` one is left alone.
    assert.equal(out.split('recoverMediaError(').length - 1, 1, 'one recovery in onErrorOut');
    assert.match(out, /!data\.errorAction\.resolved[\s\S]*?data\.fatal = true;\s*\} else if \(\/MediaSource readyState: ended\/\.test\(data\.error\.message\)\) \{(?:(?!break;)[\s\S])*this\.hls\.recoverMediaError\(\);/);
    // A non-fatal bufferAppendError is resolved by content steering, so it
    // stays non-fatal whatever its message (a single level has no other
    // resolution): the `open` case reaches us non-fatal, unrecovered.
    const cs = await read('controller/content-steering-controller.ts');
    assert.match(cs, /data\.details === ErrorDetails\.BUFFER_APPEND_ERROR && !data\.fatal\)\s*\{[^}]*errorAction\.resolved = true;/);
    // onErrorOut runs before the application's listeners.
    const hlsTs = await read('hls.ts');
    assert.ok(hlsTs.indexOf('this.on(Events.ERROR, onErrorOut, errorController)') > 0);
    const dist = await readFile(new URL('../../../../../node_modules/hls.js/dist/hls.mjs', import.meta.url), 'utf8');
    assert.ok(dist.includes('Attempting to recover from media error'), 'the bundled build has it too');
    const buf = await read('controller/buffer-controller.ts');
    assert.match(buf, /SourceBuffer error\. MediaSource readyState: \$\{this\.mediaSource\?\.readyState\}/, 'the message onSBUpdateError gives the append error');
});

// Finding 1 of the audio stage 5 review: with AAC 5.1 declared by every
// browser, about 45% of sources are multichannel, and the guard used to take
// any two fatal media errors of a session -- the audio's or not, an hour
// apart -- for the audio failing. A failure nobody pinned on the audio is
// the old route's: hls-manager.js recovers it, as on every stream.
test('audio guard: a fatal media error nobody pinned on the audio is hls-manager\'s, however many', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', container: 'audio/mp4', metadata: { channelCount: 6 } } });
    for (const [what, prep] of [
        ['a bufferAppendError alone', () => {}],
        ['the element said decode, naming no stream', () => p.setError(3, 'PIPELINE_ERROR_DECODE')],
        ['a fragment that did not parse', () => {}],
        ['a non-fatal append error of the video buffer', () => x.g.onHlsError(hls, APPENDING('video'))],
    ]) {
        x.advance(FAULT_TTL_MS + 1);
        prep();
        const data = what === 'a fragment that did not parse' ? MEDIA('fragParsingError') : MEDIA();
        assert.equal(x.g.onHlsError(hls, data), false, what);
    }
    assert.equal(hls.recovered, 0, 'recovered by hls-manager, not by the guard');
    assert.deepEqual(x.fired, []);
});

// The recovery worked when the stream then plays: a failure of the audio
// after CLEAN_PLAY_S of media is a first one again, recovered -- not the
// restart from the start.
test('audio guard: two failures of the audio with CLEAN_PLAY_S played in between are two first ones', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    const failOnce = () => {
        x.g.onHlsError(hls, APPENDING('audio'));
        return x.g.onHlsError(hls, MEDIA());
    };
    assert.equal(failOnce(), true);
    assert.equal(hls.recovered, 1);
    p.play(CLEAN_PLAY_S + 1);
    x.advance(60000);
    assert.equal(failOnce(), true);
    assert.equal(hls.recovered, 2, 'recovered again');
    assert.deepEqual(x.fired, []);
    // And the one right after that recovery, with nothing played: the same
    // fault -- the file is given up.
    x.advance(SAME_INCIDENT_MS + 1);
    assert.equal(failOnce(), true);
    assert.equal(hls.recovered, 2);
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }]);
});

test('audio guard: less than CLEAN_PLAY_S played since the recovery -- the same fault, given up', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    p.play(CLEAN_PLAY_S * 3);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.equal(hls.recovered, 1, 'what played before the recovery does not count after it');
    p.play(CLEAN_PLAY_S - 5);
    x.advance(60000);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    assert.deepEqual(x.fired, [{ reason: 'media_error', path: 'mse', audio: 'aac51' }]);
});

// A viewer who paused after the recovery shows nothing either way; a
// failure RELATED_MS later is not the same fault told again.
test('audio guard: a failure of the audio RELATED_MS after the recovery is a first one, however little played', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onHlsError(hls, APPENDING('audio'));
    x.g.onHlsError(hls, MEDIA());
    x.advance(RELATED_MS);
    x.g.onHlsError(hls, APPENDING('audio'));
    assert.equal(x.g.onHlsError(hls, MEDIA()), true);
    assert.equal(hls.recovered, 2);
    assert.deepEqual(x.fired, []);
});

test('audio guard: audio the declaration did not change, or a failure pinned on the video -- the old route\'s handling', () => {
    // Stereo in play, whatever the master says.
    let win = page();
    let p = avPlayer(win, { route: 'reencode', decode: 'aac51', audioClass: 'aac51' });
    let hls = p.hls();
    let x = auGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', metadata: { channelCount: 2 } } });
    for (let i = 0; i < 3; i++) {
        x.advance(SAME_INCIDENT_MS + 1);
        assert.equal(x.g.onHlsError(hls, MEDIA()), false, 'hls-manager recovers it');
    }
    assert.equal(hls.recovered, 0);
    assert.deepEqual(x.fired, []);
    // No class from anyone.
    win = page();
    p = avPlayer(win, { route: 'reencode', decode: 'aac51' });
    hls = p.hls();
    x = auGuard(win, p, hls);
    assert.equal(x.g.onHlsError(hls, MEDIA()), false);
    // The video buffer's append failed.
    win = page();
    p = avPlayer(win, { route: 'reencode', decode: 'aac51', audioClass: 'aac51' });
    hls = p.hls();
    x = auGuard(win, p, hls);
    x.g.onHlsError(hls, APPENDING('video'));
    assert.equal(x.g.onHlsError(hls, MEDIA()), false);
    assert.deepEqual(x.fired, []);
});

test('audio guard: on the hls.js path the element\'s errors are hls.js\'s to learn of, as on every old-route stream', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: 'aac51', audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    p.failWith(3);
    p.failWith(4);
    assert.equal(hls.recovered, 0);
    assert.deepEqual(x.fired, []);
});

test('audio guard, native HLS: the element\'s 3 and 4, where the audio was changed', () => {
    for (const [code, want] of [[3, 'decode_error'], [4, 'src_unsupported'], [2, null]]) {
        const win = page();
        const p = avPlayer(win, { route: 'copy', decode: 'aac51', audioClass: 'aac51' });
        const x = auGuard(win, p);
        p.failWith(code);
        assert.deepEqual(x.fired, want ? [{ reason: want, path: 'native', audio: 'aac51' }] : [], String(code));
    }
    let win = page();
    let p = avPlayer(win, { route: 'copy', decode: 'aac51' });
    let x = auGuard(win, p);
    p.failWith(3);
    assert.deepEqual(x.fired, [], 'the master said the audio is as it always was');
    win = page();
    p = avPlayer(win, { route: 'copy', decode: 'aac51', audioClass: 'aac51' });
    x = auGuard(win, p);
    p.failWith(3, 'video decoder error');
    assert.deepEqual(x.fired, [], 'a message that names the video alone keeps it the video\'s');
});

// Native HLS names no side and recovers nothing: the guard acts only at
// the start, where a restart from the start takes nothing from the viewer.
// Once CLEAN_PLAY_S of media played the audio has decoded here, and an
// error is the old route's (no restart), as on every stream.
test('audio guard, native HLS: only before CLEAN_PLAY_S of media has played', () => {
    for (const [played, want] of [[0, 1], [CLEAN_PLAY_S - 5, 1], [CLEAN_PLAY_S + 1, 0]]) {
        const win = page();
        const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
        const x = auGuard(win, p);
        p.play(played);
        p.failWith(3);
        assert.equal(x.fired.length, want, `after ${played} s`);
    }
    // A seek far into the film is no playback: still the start.
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: `${VIDEO},aac51`, audioClass: 'aac51' });
    const x = auGuard(win, p);
    p.play(1);
    p.seek(3600);
    p.play(5);
    p.failWith(4);
    assert.deepEqual(x.fired, [{ reason: 'src_unsupported', path: 'native', audio: 'aac51' }]);
});

test('audio guard: once, and nothing after it is hls-manager\'s to recover', () => {
    const win = page();
    const p = avPlayer(win, { route: 'copy', decode: 'aac51', audioClass: 'aac51' });
    const x = auGuard(win, p);
    p.failWith(3);
    p.failWith(3);
    assert.equal(x.fired.length, 1);
    assert.equal(x.g.onHlsError(null, MEDIA()), true, 'the page is restarting');
});

// A browser opted out of audio (?audio=off), or one whose probe answered no
// audio token, declares none (data-decode): its
// passthrough's failures keep the video's rules whatever hls.js reports --
// even Dolby a transcoder sent anyway -- and fallbackAudio is never reached.
test('passthrough guard: a start that declared no audio token has no audio class, whatever hls.js or the tag says', () => {
    const VIDEO_ONLY = 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq';
    for (const [name, prep] of [
        ['Dolby buffered', (x) => x.g.onBufferCodecs(EC3)],
        ['Dolby on the tag', () => {}],
        ['the audio buffer pinned', (x, hls) => { x.g.onBufferCodecs(EC3); x.g.onHlsError(hls, APPENDING('audio')); }],
    ]) {
        const win = page();
        const p = avPlayer(win, { decode: VIDEO_ONLY, audioClass: name === 'Dolby on the tag' ? 'dolby' : '' });
        const hls = p.hls();
        const x = ptGuard(win, p, hls);
        prep(x, hls);
        p.failWith(3);
        x.advance(SAME_INCIDENT_MS + 1);
        prep(x, hls);
        p.failWith(3);
        assert.deepEqual(x.fired, [{ reason: 'decode_error', path: 'mse', audio: null }], name);
    }
});

test('audio guard: nothing for a start that declared no audio token, even if one were made', () => {
    const win = page();
    const p = avPlayer(win, { route: 'reencode', decode: 'hevc8,hdr-pq', audioClass: 'aac51' });
    const hls = p.hls();
    const x = auGuard(win, p, hls);
    x.g.onBufferCodecs({ audio: { codec: 'mp4a.40.2', metadata: { channelCount: 6 } } });
    assert.equal(x.g.onHlsError(hls, MEDIA()), false, 'hls-manager recovers it, as ever');
    assert.equal(hls.recovered, 0);
    const nat = page();
    const q = avPlayer(nat, { route: 'reencode', decode: '', audioClass: 'aac51' });
    const y = auGuard(nat, q);
    q.failWith(3);
    assert.deepEqual(y.fired, []);
});

test('fallbackAudio of a browser opted out of audio (?audio=off): the restart declares no audio token', () => {
    const win = audioDeclaringPage();
    win.localStorage.setItem(AUDIO_OPTIN_KEY, 'off');
    startForm(win);
    const submits = captureSubmits(win);
    fallbackAudio({ video: avPlayer(win, { audioClass: 'dolby' }).v, reason: 'decode_error', cls: 'dolby', win, doc: win.document, track: () => {} });
    assert.equal(submits[0].decode, VIDEO, 'the video only, as every start of this browser');
});

// The default since the audio's stage 5 for AAC 5.1: aac51 without Dolby.
// Since the audio's stage 5 for Dolby such a browser declares ac3/ec3 by
// default; the restart after a Dolby failure is the rest: the video and aac51.
test('fallbackAudio of a browser with the audio switch never opened: the restart keeps aac51', () => {
    const win = audioDeclaringPage();
    win.localStorage.removeItem(AUDIO_OPTIN_KEY);
    startForm(win);
    const submits = captureSubmits(win);
    fallbackAudio({ video: avPlayer(win, { audioClass: 'dolby' }).v, reason: 'decode_error', cls: 'dolby', win, doc: win.document, track: () => {} });
    assert.equal(submits[0].decode, `${VIDEO},aac51`);
    const again = audioDeclaringPage();
    again.localStorage.removeItem(AUDIO_OPTIN_KEY);
    startForm(again);
    const next = captureSubmits(again);
    fallbackAudio({ video: avPlayer(again, { route: 'reencode', audioClass: 'aac51', decode: `${VIDEO},aac51` }).v, reason: 'media_error', cls: 'aac51', win: again, doc: again.document, track: () => {} });
    assert.equal(next[0].decode, VIDEO, 'an AAC 5.1 failure: no audio token');
});
