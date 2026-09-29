import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import Hls from 'hls.js';
import {
    EVENT,
    STORAGE_KEY,
    TTL_MS,
    PAGE_FLAG,
    MSE_TYPES,
    probeStatic,
    probeCodecSupport,
    envFromWindow,
    sourceCodec,
    playbackPath,
    reportCodecSupport,
    whenPlaying,
    DECODE_HEVC,
    DECODE_TOKENS,
    DECODE_VIDEO_TOKENS,
    DECODE_AUDIO_TOKENS,
    DECODE_DOLBY,
    AAC51_TOKEN,
    AAC51_CODEC,
    AAC51_AUDIO,
    audioMseType,
    audioFileType,
    dolbyDecodeTokens,
    PQ_CODEC,
    HEVC_ANSWER_INACCURATE,
    mseType,
    fileType,
    isIOSLike,
    hlsJsSupported,
    decodePath,
    hevcDecodeTokens,
    decodeTokens,
    declarationSupport,
    dynamicRange,
    QUALITY_EVENT,
    QUALITY_PAGE_FLAG,
    QUALITY_AFTER_S,
    playbackQuality,
    afterPlayed,
    watchPlaybackQuality,
} from './codec-support.js';

// ---- fakes ---------------------------------------------------------------

// A MediaSource-like constructor whose isTypeSupported says yes to the types
// in `yes`.
function mediaSource(yes) {
    const MS = function () {};
    MS.isTypeSupported = (type) => yes.includes(type);
    return MS;
}

const canPlay = (answers) => (type) => answers[type] || '';

const HLS = 'application/vnd.apple.mpegurl';

function capabilities(answers) {
    return {
        calls: [],
        decodingInfo(config) {
            this.calls.push(config);
            const a = answers[config.video.contentType];
            return Promise.resolve(a || { supported: false, smooth: false, powerEfficient: false });
        },
    };
}

const ALL_FALSE_MC = {
    mc_hvc: false, mc_hvc_sm: false, mc_hvc_pe: false,
    mc_av1: false, mc_av1_sm: false, mc_av1_pe: false,
};

// The declaration's fields where nothing is declared: these fakes answer
// the existing questions but are no browser hls.js would run in (no basic
// H.264) and have no native HLS, so the declaration's path is 'none'.
const NO_DECLARATION = {
    hevc8: false, hevc10: false, 'hevc8-2160': false, 'hevc10-2160': false, 'hevc-high': false,
    'hdr-pq': false, aac51: false, ac3: false, ec3: false,
    decode: '', decode_path: 'none', 'dynamic-range': 'unknown',
};

// ---- the probe -----------------------------------------------------------

test('Chrome-like: MSE says HEVC and AV1, the hardware answers per codec', async () => {
    const mc = capabilities({
        [MSE_TYPES.hvc]: { supported: true, smooth: true, powerEfficient: true },
        [MSE_TYPES.av1]: { supported: true, smooth: true, powerEfficient: false },
    });
    const got = await probeCodecSupport({
        MediaSource: mediaSource([MSE_TYPES.hvc, MSE_TYPES.hev, MSE_TYPES.hvc10, MSE_TYPES.av1, MSE_TYPES.av1_10, MSE_TYPES.av1_4k]),
        canPlayType: canPlay({ [MSE_TYPES.hvc]: 'probably', [MSE_TYPES.av1]: 'probably' }),
        mediaCapabilities: mc,
    });
    assert.deepEqual(got, {
        mse: 'mse',
        hvc: true, hev: true, hvc10: true, hvc4k: false,
        av1: true, av1_10: true, av1_4k: true,
        n_hls: false, n_hvc: true, n_av1: true,
        mc: true,
        mc_hvc: true, mc_hvc_sm: true, mc_hvc_pe: true,
        mc_av1: true, mc_av1_sm: true, mc_av1_pe: false,
        ...NO_DECLARATION,
    });
    // Asked about a 1080p stream through MSE, with every field Firefox
    // insists on.
    assert.equal(mc.calls.length, 2);
    for (const c of mc.calls) {
        assert.equal(c.type, 'media-source');
        assert.equal(c.video.width, 1920);
        assert.equal(c.video.height, 1080);
        assert.ok(c.video.bitrate > 0);
        assert.ok(c.video.framerate > 0);
    }
});

test('Firefox-like: AV1 only, no native HLS, HEVC decodingInfo says no', async () => {
    const got = await probeCodecSupport({
        MediaSource: mediaSource([MSE_TYPES.av1, MSE_TYPES.av1_10]),
        canPlayType: canPlay({ [MSE_TYPES.av1]: 'probably' }),
        mediaCapabilities: capabilities({
            [MSE_TYPES.av1]: { supported: true, smooth: true, powerEfficient: true },
        }),
    });
    assert.deepEqual(got, {
        mse: 'mse',
        hvc: false, hev: false, hvc10: false, hvc4k: false,
        av1: true, av1_10: true, av1_4k: false,
        n_hls: false, n_hvc: false, n_av1: true,
        mc: true,
        mc_hvc: false, mc_hvc_sm: false, mc_hvc_pe: false,
        mc_av1: true, mc_av1_sm: true, mc_av1_pe: true,
        ...NO_DECLARATION,
    });
});

test('Safari on iPhone: ManagedMediaSource only, native HLS and HEVC', async () => {
    const got = await probeCodecSupport({
        ManagedMediaSource: mediaSource([MSE_TYPES.hvc, MSE_TYPES.hvc10]),
        canPlayType: canPlay({ [HLS]: 'maybe', [MSE_TYPES.hvc]: 'probably' }),
        mediaCapabilities: capabilities({
            [MSE_TYPES.hvc]: { supported: true, smooth: true, powerEfficient: true },
        }),
    });
    assert.equal(got.mse, 'mms');
    assert.equal(got.hvc, true, 'isTypeSupported is asked of the ManagedMediaSource');
    assert.equal(got.hvc10, true);
    assert.equal(got.av1, false);
    assert.equal(got.n_hls, true);
    assert.equal(got.n_hvc, true);
    assert.equal(got.n_av1, false, 'no AV1 decoder on this iPhone');
    assert.equal(got.mc_hvc_pe, true);
});

test('where both exist, MediaSource is the flavour and the one asked', () => {
    const got = probeStatic({
        MediaSource: mediaSource([MSE_TYPES.av1]),
        ManagedMediaSource: mediaSource([MSE_TYPES.hvc]),
    });
    assert.equal(got.mse, 'mse');
    assert.equal(got.av1, true);
    assert.equal(got.hvc, false);
});

test('no MSE, no mediaCapabilities: every answer is false', async () => {
    const got = await probeCodecSupport({ canPlayType: canPlay({}) });
    assert.deepEqual(got, {
        mse: 'none',
        hvc: false, hev: false, hvc10: false, hvc4k: false,
        av1: false, av1_10: false, av1_4k: false,
        n_hls: false, n_hvc: false, n_av1: false,
        mc: false,
        ...ALL_FALSE_MC,
        ...NO_DECLARATION,
    });
    // And with nothing at all.
    const empty = await probeCodecSupport();
    assert.equal(empty.mse, 'none');
    assert.equal(empty.n_hls, false);
    assert.equal(empty.mc, false);
});

test('throwing APIs answer false, never throw', async () => {
    const MS = function () {};
    MS.isTypeSupported = () => { throw new Error('boom'); };
    const got = await probeCodecSupport({
        MediaSource: MS,
        canPlayType: () => { throw new Error('boom'); },
        mediaCapabilities: { decodingInfo: () => { throw new TypeError('bad config'); } },
    });
    assert.equal(got.mse, 'mse');
    for (const k of Object.keys(MSE_TYPES)) assert.equal(got[k], false, k);
    assert.equal(got.n_hls, false);
    assert.equal(got.n_hvc, false);
    assert.equal(got.n_av1, false);
    assert.equal(got.mc, true);
    for (const [k, v] of Object.entries(ALL_FALSE_MC)) assert.equal(got[k], v, k);

    // A rejection, a malformed answer.
    const rejected = await probeCodecSupport({
        mediaCapabilities: { decodingInfo: () => Promise.reject(new Error('nope')) },
    });
    for (const [k, v] of Object.entries(ALL_FALSE_MC)) assert.equal(rejected[k], v, k);
    const odd = await probeCodecSupport({
        mediaCapabilities: { decodingInfo: () => Promise.resolve('yes') },
    });
    for (const [k, v] of Object.entries(ALL_FALSE_MC)) assert.equal(odd[k], v, k);

    // Throwing getters on the env itself.
    const hostile = {};
    for (const k of ['MediaSource', 'ManagedMediaSource', 'canPlayType', 'mediaCapabilities']) {
        Object.defineProperty(hostile, k, { get() { throw new Error('denied'); } });
    }
    const got2 = await probeCodecSupport(hostile);
    assert.equal(got2.mse, 'none');
    assert.equal(got2.mc, false);
});

test('a decodingInfo that never answers times out as false', { timeout: 2000 }, async () => {
    const got = await probeCodecSupport({
        mediaCapabilities: { decodingInfo: () => new Promise(() => {}) },
    }, { timeoutMs: 5 });
    assert.equal(got.mc, true);
    for (const [k, v] of Object.entries(ALL_FALSE_MC)) assert.equal(got[k], v, k);
});

test('envFromWindow survives a window whose getters throw', async () => {
    const win = {};
    for (const k of ['MediaSource', 'ManagedMediaSource', 'navigator', 'document']) {
        Object.defineProperty(win, k, { get() { throw new Error('denied'); } });
    }
    const env = envFromWindow(win);
    assert.equal(env.canPlayType, undefined);
    const got = await probeCodecSupport(env);
    assert.equal(got.mse, 'none');
    assert.equal(got.n_hls, false);

    // With a real element: its canPlayType is the one asked.
    const video = { canPlayType: (t) => (t === HLS ? 'maybe' : '') };
    assert.equal(probeStatic(envFromWindow({}, video)).n_hls, true);
});

// ---- what the page knows about the stream ---------------------------------

test('sourceCodec: the film stream, not the cover; absent probe is unknown', () => {
    assert.equal(sourceCodec('hevc '), 'hevc');
    assert.equal(sourceCodec('h264'), 'h264');
    assert.equal(sourceCodec('av1 '), 'av1');
    assert.equal(sourceCodec('mjpeg hevc '), 'hevc', 'a cover picture listed first is skipped');
    assert.equal(sourceCodec('png h264 '), 'h264');
    assert.equal(sourceCodec('vp9 '), 'other');
    assert.equal(sourceCodec('mpeg4'), 'other');
    assert.equal(sourceCodec('HEVC'), 'hevc');
    assert.equal(sourceCodec(''), 'unknown', 'a probe with no video stream');
    assert.equal(sourceCodec('mjpeg '), 'unknown', 'a cover alone is not a film');
    assert.equal(sourceCodec(undefined), 'unknown', 'no probe on the page');
    assert.equal(sourceCodec(null), 'unknown');
});

test('playbackPath: hls.js, native HLS, or a plain file', () => {
    assert.equal(playbackPath({}, 'https://x.test/index.m3u8'), 'hlsjs');
    assert.equal(playbackPath(null, 'https://x.test/s/index.m3u8?token=1'), 'native');
    assert.equal(playbackPath(null, 'https://x.test/movie.mp4'), 'direct');
    assert.equal(playbackPath(null, ''), 'direct');
    assert.equal(playbackPath(null, undefined), 'direct');
});

// ---- the reporter ----------------------------------------------------------

function memoryStorage() {
    const m = new Map();
    return {
        map: m,
        getItem: (k) => (m.has(k) ? m.get(k) : null),
        setItem: (k, v) => m.set(k, String(v)),
    };
}

// page is one page load: its own window (and so its own page flag), the
// browser's storage shared across pages, a clock, and a scheduler that holds
// the deferred work until run() — so "nothing yet" is observable.
function page({ storage = memoryStorage(), clock = { t: 1_000_000_000_000 }, umami = true } = {}) {
    const events = [];
    const win = umami ? { umami: { track: (name, data) => events.push({ name, data }) } } : {};
    const queue = [];
    const deps = {
        win,
        storage,
        now: () => clock.t,
        schedule: (fn) => queue.push(fn),
        probe: async () => ({ mse: 'mse', hvc: true }),
        env: () => ({}),
    };
    return {
        win, events, deps, storage, clock,
        report: (extra) => reportCodecSupport(extra, deps),
        run: async () => { while (queue.length) await queue.shift()(); },
        pending: () => queue.length,
    };
}

test('one event, with the probe and what the player knows, after the deferral', async () => {
    const p = page();
    assert.equal(p.report({ src: 'hevc', tc: true, pl: 'hlsjs', emb: false }), true);
    assert.equal(p.events.length, 0, 'deferred: nothing on the critical path');
    await p.run();
    assert.deepEqual(p.events, [{
        name: EVENT,
        data: { mse: 'mse', hvc: true, src: 'hevc', tc: true, pl: 'hlsjs', emb: false },
    }]);
    assert.equal(p.storage.getItem(STORAGE_KEY), String(p.clock.t));
});

test('once per browser per 7 days', async () => {
    const storage = memoryStorage();
    const clock = { t: 1_000_000_000_000 };
    const first = page({ storage, clock });
    first.report({});
    await first.run();
    assert.equal(first.events.length, 1);

    // Another page load, six days later: the browser has reported.
    clock.t += TTL_MS - 24 * 60 * 60 * 1000;
    const second = page({ storage, clock });
    assert.equal(second.report({}), false);
    await second.run();
    assert.equal(second.events.length, 0, 'within the week, nothing');

    // A week and a bit after the first: again.
    clock.t += 2 * 24 * 60 * 60 * 1000;
    const third = page({ storage, clock });
    assert.equal(third.report({}), true);
    await third.run();
    assert.equal(third.events.length, 1);
});

test('the same page reports once, whatever the storage does', async () => {
    const p = page();
    p.report({});
    // A second player on the same page (the next episode) before the
    // first report has even gone out.
    assert.equal(p.report({}), false);
    await p.run();
    assert.equal(p.events.length, 1);
    assert.equal(p.win[PAGE_FLAG], true);
});

test('no storage: once per page', async () => {
    const a = page({ storage: null });
    a.report({});
    a.report({});
    await a.run();
    assert.equal(a.events.length, 1);
    // Nothing to remember it by, so the next page load reports again.
    const b = page({ storage: null });
    b.report({});
    await b.run();
    assert.equal(b.events.length, 1);
});

test('a storage that throws is treated as none', async () => {
    const hostile = {
        getItem() { throw new Error('SecurityError'); },
        setItem() { throw new Error('QuotaExceededError'); },
    };
    const a = page({ storage: hostile });
    assert.doesNotThrow(() => a.report({}));
    a.report({});
    await a.run();
    assert.equal(a.events.length, 1);
});

test('a stamp from the future does not silence the browser', async () => {
    const storage = memoryStorage();
    const clock = { t: 1_000_000_000_000 };
    storage.setItem(STORAGE_KEY, String(clock.t + 3 * 24 * 60 * 60 * 1000));
    const p = page({ storage, clock });
    assert.equal(p.report({}), true);
    await p.run();
    assert.equal(p.events.length, 1);
});

test('a garbage stamp reads as never sent', async () => {
    const storage = memoryStorage();
    storage.setItem(STORAGE_KEY, 'not a number');
    const p = page({ storage });
    p.report({});
    await p.run();
    assert.equal(p.events.length, 1);
});

test('without window.umami: nothing sent, nothing remembered', async () => {
    const storage = memoryStorage();
    const p = page({ storage, umami: false });
    assert.equal(p.report({}), false);
    await p.run();
    assert.equal(p.pending(), 0);
    assert.equal(storage.getItem(STORAGE_KEY), null, 'no stamp: the week is not written off');
    assert.notEqual(p.win[PAGE_FLAG], true, 'no page flag either');

    // Analytics arrives later on the same page: it reports.
    const events = [];
    p.win.umami = { track: (name, data) => events.push({ name, data }) };
    assert.equal(p.report({}), true);
    await p.run();
    assert.equal(events.length, 1);
});

test('umami gone by the time the deferred work runs: nothing sent, no stamp', async () => {
    const p = page();
    p.report({});
    delete p.win.umami;
    await p.run();
    assert.equal(p.events.length, 0);
    assert.equal(p.storage.getItem(STORAGE_KEY), null);
});

test('a throwing umami.track does not escape', async () => {
    const p = page();
    p.win.umami.track = () => { throw new Error('network'); };
    p.report({});
    await p.run();
    assert.ok(true);
});

test('the default scheduler uses requestIdleCallback with the window as this', async () => {
    const events = [];
    let idleArgs = null;
    const win = {
        umami: { track: (name, data) => events.push({ name, data }) },
        requestIdleCallback(fn, opts) {
            assert.equal(this, win, 'an unbound requestIdleCallback throws Illegal invocation');
            idleArgs = { fn, opts };
        },
    };
    reportCodecSupport({ src: 'h264' }, { win, storage: null, probe: async () => ({}) });
    assert.ok(idleArgs, 'deferred to an idle callback');
    assert.ok(idleArgs.opts && idleArgs.opts.timeout > 0, 'with a deadline, so a busy page still reports');
    assert.equal(events.length, 0);
    await idleArgs.fn();
    assert.deepEqual(events, [{ name: EVENT, data: { src: 'h264' } }]);
});

test('a requestIdleCallback that throws: the timer instead, and no throw', async (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const events = [];
    const win = {
        umami: { track: (name, data) => events.push({ name, data }) },
        requestIdleCallback() { throw new TypeError('Illegal invocation'); },
    };
    let scheduled;
    assert.doesNotThrow(() => {
        scheduled = reportCodecSupport({ src: 'hevc' }, { win, storage: null, probe: async () => ({}) });
    });
    assert.equal(scheduled, true);
    assert.equal(events.length, 0);
    t.mock.timers.tick(2000);
    // The timer's callback awaits the probe: let it settle.
    for (let i = 0; i < 5; i++) await Promise.resolve();
    assert.deepEqual(events, [{ name: EVENT, data: { src: 'hevc' } }]);
});

// The player calls the reporter synchronously from a useEffect when the
// element already plays at mount; a throw there would cancel the player's
// later effects (Preact drops them), player_ready among them.
test('the reporter never throws, whatever its dependencies do', () => {
    const win = { umami: { track() {} } };
    const boom = () => { throw new Error('boom'); };
    assert.doesNotThrow(() => reportCodecSupport({}, { win, storage: null, schedule: boom }));
    assert.equal(reportCodecSupport({}, { win: { umami: { track() {} } }, storage: null, now: boom }), false);
});

// ---- the playback gate ---------------------------------------------------

class FakeVideo extends EventTarget {
    constructor({ paused = true, ended = false, readyState = 0 } = {}) {
        super();
        this.paused = paused;
        this.ended = ended;
        this.readyState = readyState;
    }
}

test('whenPlaying waits for the first playing, and fires once', () => {
    const v = new FakeVideo();
    let n = 0;
    whenPlaying(v, () => { n += 1; });
    for (const type of ['loadstart', 'loadedmetadata', 'canplay', 'play', 'waiting', 'canplaythrough']) {
        v.dispatchEvent(new Event(type));
    }
    assert.equal(n, 0, 'nothing before the first frame plays');
    v.dispatchEvent(new Event('playing'));
    assert.equal(n, 1);
    v.dispatchEvent(new Event('playing'));
    assert.equal(n, 1, 'a session seek replays `playing`; still one');
});

test('whenPlaying fires at once for an element already playing', () => {
    const v = new FakeVideo({ paused: false, readyState: 4 });
    let n = 0;
    whenPlaying(v, () => { n += 1; });
    assert.equal(n, 1);
    v.dispatchEvent(new Event('playing'));
    assert.equal(n, 1);
});

test('whenPlaying: loaded but paused, or not paused but starved, is not playing', () => {
    let n = 0;
    whenPlaying(new FakeVideo({ paused: true, readyState: 4 }), () => { n += 1; });
    whenPlaying(new FakeVideo({ paused: false, readyState: 2 }), () => { n += 1; });
    whenPlaying(new FakeVideo({ paused: false, ended: true, readyState: 4 }), () => { n += 1; });
    assert.equal(n, 0);
});

test('whenPlaying cleanup before playback: never fires', () => {
    const v = new FakeVideo();
    let n = 0;
    const stop = whenPlaying(v, () => { n += 1; });
    stop();
    v.dispatchEvent(new Event('playing'));
    assert.equal(n, 0);
});

test('gate and reporter together: no event until playing, then one', async () => {
    const p = page();
    const v = new FakeVideo();
    whenPlaying(v, () => p.report({ src: 'av1' }));
    v.dispatchEvent(new Event('canplay'));
    v.dispatchEvent(new Event('play'));
    await p.run();
    assert.equal(p.events.length, 0, 'loaded and asked to play is not watching');
    v.dispatchEvent(new Event('playing'));
    await p.run();
    assert.equal(p.events.length, 1);
    assert.equal(p.events[0].data.src, 'av1');
});

// ---- the declaration: `decode` tokens -------------------------------------

const UA = {
    chromeWin: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36',
    edgeWin: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0',
    firefoxWin: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:143.0) Gecko/20100101 Firefox/143.0',
    firefoxMac: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:143.0) Gecko/20100101 Firefox/143.0',
    firefoxLinux: 'Mozilla/5.0 (X11; Linux x86_64; rv:143.0) Gecko/20100101 Firefox/143.0',
    firefoxAndroid: 'Mozilla/5.0 (Android 14; Mobile; rv:143.0) Gecko/143.0 Firefox/143.0',
    safariMac: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15',
    iPhone: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
};

// What hls.js asks before it agrees to run (Hls.isSupported).
const H264 = 'video/mp4;codecs=avc1.42E01E,mp4a.40.2';
const hevcCodec = Object.fromEntries(DECODE_HEVC);
const mseYes = (...tokens) => [H264, ...tokens.map((t) => mseType(hevcCodec[t]))];
const nativeYes = (...tokens) => Object.fromEntries([
    [HLS, 'maybe'],
    ...tokens.map((t) => [fileType(hevcCodec[t]), 'probably']),
]);
const ALL_HEVC = DECODE_HEVC.map(([t]) => t);

// pqCapabilities answers decodingInfo: `pq` for a PQ question, `aac` for
// an audio one (the AAC 5.1 question; "no" unless the test says), a plain
// "yes, in hardware" for anything else, and records every question.
function pqCapabilities(pq, aac = { supported: false, smooth: false, powerEfficient: false }) {
    return {
        calls: [],
        decodingInfo(config) {
            this.calls.push(config);
            if (config.video && config.video.transferFunction === 'pq') {
                return typeof pq === 'function' ? pq(config) : Promise.resolve(pq);
            }
            if (config.audio) {
                return typeof aac === 'function' ? aac(config) : Promise.resolve(aac);
            }
            return Promise.resolve({ supported: true, smooth: true, powerEfficient: true });
        },
    };
}
const PQ_YES = { supported: true, smooth: true, powerEfficient: true };
const PQ_SOFTWARE = { supported: true, smooth: false, powerEfficient: false };
const PQ_NO = { supported: false, smooth: false, powerEfficient: false };
const AAC_YES = { supported: true, smooth: true, powerEfficient: true };
// The questions decodingInfo was asked about video, and about audio.
const videoCalls = (mc) => mc.calls.filter((c) => c.video);
const audioCalls = (mc) => mc.calls.filter((c) => c.audio);

test('the tokens and their codec strings are the protocol', () => {
    // The transcoder parses these by allowlist (plan §2.2): a rename here
    // is a silent "declares nothing" there. The audio tokens come after the
    // video ones, in the order web-ui's server allowlist keeps
    // (models.decodeTokens).
    assert.deepEqual(DECODE_TOKENS, ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160', 'hevc-high', 'hdr-pq', 'aac51', 'ac3', 'ec3']);
    assert.deepEqual(DECODE_VIDEO_TOKENS, ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160', 'hevc-high', 'hdr-pq']);
    assert.deepEqual(DECODE_AUDIO_TOKENS, ['aac51', 'ac3', 'ec3']);
    assert.equal(AAC51_TOKEN, 'aac51');
    assert.equal(AAC51_CODEC, 'mp4a.40.2');
    assert.deepEqual(AAC51_AUDIO, { channels: '6', bitrate: 384000, samplerate: 48000 });
    assert.deepEqual(DECODE_DOLBY, [['ac3', 'ac-3'], ['ec3', 'ec-3']]);
    assert.equal(audioMseType('ec-3'), 'audio/mp4;codecs=ec-3');
    assert.equal(audioFileType('ec-3'), 'audio/mp4; codecs="ec-3"');
    assert.deepEqual(DECODE_HEVC, [
        ['hevc8', 'hvc1.1.6.L123.90'],
        ['hevc10', 'hvc1.2.4.L123.90'],
        ['hevc8-2160', 'hvc1.1.6.L153.90'],
        ['hevc10-2160', 'hvc1.2.4.L153.90'],
        ['hevc-high', 'hvc1.2.4.H153.90'],
    ]);
    assert.equal(PQ_CODEC, 'hvc1.2.4.L153.90');
    assert.equal(mseType('hvc1.1.6.L123.90'), 'video/mp4;codecs=hvc1.1.6.L123.90');
    assert.equal(fileType('hvc1.1.6.L123.90'), 'video/mp4; codecs="hvc1.1.6.L123.90"');
});

test('Chrome on Windows, HEVC to 1080p only: the MSE answers decide, in token order', async () => {
    const mc = pqCapabilities(PQ_NO);
    const env = {
        userAgent: UA.chromeWin,
        MediaSource: mediaSource(mseYes('hevc10', 'hevc8')),
        canPlayType: canPlay({}),
        mediaCapabilities: mc,
    };
    assert.equal(decodePath(env), 'mse');
    assert.deepEqual(hevcDecodeTokens(env), ['hevc8', 'hevc10']);
    assert.deepEqual(await decodeTokens(env), ['hevc8', 'hevc10']);
    // Of video, only the PQ question goes to mediaCapabilities (the other
    // one is the AAC 5.1 question, audio).
    assert.equal(videoCalls(mc).length, 1);
    assert.equal(mc.calls.length, 2);
});

test('any support counts: software HEVC declares, and PQ without powerEfficient declares', async () => {
    // The owner's decision of 2026-09-27: no powerEfficient anywhere, and
    // no decodingInfo for the HEVC tokens at all. This mediaCapabilities
    // says HEVC is not supported — it must not be asked.
    const mc = {
        calls: [],
        decodingInfo(config) {
            this.calls.push(config);
            if (config.audio) return Promise.resolve(PQ_NO);
            if (config.video.transferFunction === 'pq') return Promise.resolve(PQ_SOFTWARE);
            return Promise.resolve(PQ_NO);
        },
    };
    const env = {
        userAgent: UA.chromeWin,
        MediaSource: mediaSource(mseYes(...ALL_HEVC)),
        mediaCapabilities: mc,
    };
    assert.deepEqual(await decodeTokens(env), DECODE_VIDEO_TOKENS);
    assert.equal(videoCalls(mc).length, 1, 'of video, decodingInfo is asked the PQ question and nothing else');
});

test('hdr-pq is asked as Main10 4K PQ in rec2020, through MSE, without hdrMetadataType', async () => {
    const mc = pqCapabilities(PQ_YES);
    const env = {
        userAgent: UA.chromeWin,
        MediaSource: mediaSource(mseYes('hevc10-2160')),
        mediaCapabilities: mc,
    };
    assert.deepEqual(await decodeTokens(env), ['hevc10-2160', 'hdr-pq']);
    assert.equal(videoCalls(mc).length, 1);
    const c = videoCalls(mc)[0];
    assert.equal(c.type, 'media-source');
    assert.equal(c.video.contentType, 'video/mp4;codecs=hvc1.2.4.L153.90');
    assert.equal(c.video.width, 3840);
    assert.equal(c.video.height, 2160);
    assert.equal(c.video.transferFunction, 'pq');
    assert.equal(c.video.colorGamut, 'rec2020');
    assert.ok(!('hdrMetadataType' in c.video), 'the question is decoding PQ, not its metadata');
    assert.ok(c.video.bitrate > 0 && c.video.framerate > 0, 'Firefox rejects a configuration without them');
});

test('hdr-pq: no decodingInfo, a rejection, a throw, garbage or a timeout is no token — the HEVC ones stay', { timeout: 2000 }, async () => {
    const base = { userAgent: UA.chromeWin, MediaSource: mediaSource(mseYes('hevc8')) };
    assert.deepEqual(await decodeTokens(base), ['hevc8'], 'no mediaCapabilities');
    assert.deepEqual(await decodeTokens({ ...base, mediaCapabilities: {} }), ['hevc8']);
    for (const pq of [
        () => Promise.reject(new Error('nope')),
        () => { throw new TypeError('bad config'); },
        () => Promise.resolve('yes'),
        () => Promise.resolve({ supported: 'true' }),
        () => new Promise(() => {}),
    ]) {
        assert.deepEqual(await decodeTokens({ ...base, mediaCapabilities: pqCapabilities(pq) }, { timeoutMs: 5 }), ['hevc8']);
    }
});

test('Firefox on Windows declares nothing, whatever it answers; Firefox elsewhere does', async () => {
    // hls.js overrides this browser's HEVC answers (issue 7046): a player
    // that does not believe the answer must not have it declared.
    const everything = (ua) => ({
        userAgent: ua,
        MediaSource: mediaSource(mseYes(...ALL_HEVC)),
        mediaCapabilities: pqCapabilities(PQ_YES),
    });
    const ffWin = everything(UA.firefoxWin);
    assert.equal(decodePath(ffWin), 'mse', 'it still plays through hls.js');
    assert.deepEqual(hevcDecodeTokens(ffWin), []);
    assert.deepEqual(await decodeTokens(ffWin), []);
    assert.equal(videoCalls(ffWin.mediaCapabilities).length, 0, 'hdr-pq is an HEVC question too: not asked');

    for (const ua of [UA.firefoxMac, UA.firefoxLinux, UA.firefoxAndroid, UA.chromeWin, UA.edgeWin]) {
        assert.deepEqual(await decodeTokens(everything(ua)), DECODE_VIDEO_TOKENS, ua);
    }
});

test('iPhone: native HLS even with a ManagedMediaSource; canPlayType decides; PQ never declared', async () => {
    const mc = pqCapabilities(PQ_YES);
    const env = {
        userAgent: UA.iPhone,
        // The MSE would say yes to everything; the player never uses it
        // on iOS (hls-manager.js), so it must not be the one asked.
        ManagedMediaSource: mediaSource(mseYes(...ALL_HEVC)),
        canPlayType: canPlay(nativeYes('hevc8', 'hevc10', 'hevc10-2160')),
        mediaCapabilities: mc,
    };
    assert.equal(decodePath(env), 'native');
    // decodingInfo would say yes; native HLS refuses a PQ variant without a
    // word (2026-09-29), so the question is not asked.
    assert.deepEqual(await decodeTokens(env), ['hevc8', 'hevc10', 'hevc10-2160']);
    assert.equal(videoCalls(mc).length, 0);
    assert.deepEqual(await settled(declarationSupport(env).pq), { v: false });

    // A "maybe" is a yes too: any support counts.
    const maybe = { ...env, canPlayType: canPlay({ [HLS]: 'maybe', [fileType(hevcCodec['hevc-high'])]: 'maybe' }) };
    assert.deepEqual(hevcDecodeTokens(maybe), ['hevc-high']);
});

test('an iPad in desktop mode is iOS; a Mac is not', () => {
    const ipad = { userAgent: UA.safariMac, platform: 'MacIntel', maxTouchPoints: 5 };
    const mac = { userAgent: UA.safariMac, platform: 'MacIntel', maxTouchPoints: 0 };
    assert.equal(isIOSLike(ipad), true);
    assert.equal(isIOSLike(mac), false);
    assert.equal(isIOSLike({ userAgent: UA.iPhone }), true);
    assert.equal(isIOSLike({}), false);

    const withMse = (e) => ({
        ...e,
        MediaSource: mediaSource(mseYes('hevc8')),
        canPlayType: canPlay(nativeYes('hevc10')),
    });
    assert.deepEqual(hevcDecodeTokens(withMse(ipad)), ['hevc10'], 'the iPad asks its element');
    assert.deepEqual(hevcDecodeTokens(withMse(mac)), ['hevc8'], 'the Mac asks hls.js\'s MediaSource');
});

test('Safari on a Mac: hls.js prefers the ManagedMediaSource, so it is the one asked', () => {
    const env = {
        userAgent: UA.safariMac,
        platform: 'MacIntel',
        maxTouchPoints: 0,
        MediaSource: mediaSource([H264]),
        ManagedMediaSource: mediaSource(mseYes('hevc8', 'hevc-high')),
    };
    assert.equal(decodePath(env), 'mse');
    assert.deepEqual(hevcDecodeTokens(env), ['hevc8', 'hevc-high']);
});

test('where hls.js will not run: native HLS if the element has it, else nothing', async () => {
    // An MSE that cannot play even H.264 is one hls.js refuses.
    const refused = {
        userAgent: UA.chromeWin,
        MediaSource: mediaSource(mseYes('hevc8').slice(1)),
        canPlayType: canPlay(nativeYes('hevc8')),
    };
    assert.equal(hlsJsSupported(refused), false);
    assert.equal(decodePath(refused), 'native');
    assert.deepEqual(hevcDecodeTokens(refused), ['hevc8']);

    const mc = pqCapabilities(PQ_YES);
    const nothing = { userAgent: UA.chromeWin, canPlayType: canPlay({ [fileType(hevcCodec.hevc8)]: 'probably' }), mediaCapabilities: mc };
    assert.equal(decodePath(nothing), 'none');
    assert.deepEqual(await decodeTokens(nothing), [], 'no HLS here: nothing to declare');
    assert.equal(mc.calls.length, 0);
});

test('the declaration never throws, whatever the browser does', async () => {
    const hostile = {};
    for (const k of ['MediaSource', 'ManagedMediaSource', 'WebKitMediaSource', 'SourceBuffer', 'WebKitSourceBuffer',
        'canPlayType', 'mediaCapabilities', 'userAgent', 'platform', 'maxTouchPoints', 'matchMedia']) {
        Object.defineProperty(hostile, k, { get() { throw new Error('denied'); } });
    }
    assert.equal(decodePath(hostile), 'none');
    assert.deepEqual(await decodeTokens(hostile), []);
    assert.equal(dynamicRange(hostile), 'unknown');

    const MS = function () {};
    MS.isTypeSupported = (t) => { if (t === H264) return true; throw new Error('boom'); };
    const throwing = { userAgent: UA.chromeWin, MediaSource: MS };
    assert.equal(decodePath(throwing), 'mse');
    assert.deepEqual(await decodeTokens(throwing), []);
    assert.deepEqual(await decodeTokens(), []);
});

test('the event carries the declaration exactly as decodeTokens computes it', async () => {
    const envs = [
        { userAgent: UA.chromeWin, MediaSource: mediaSource(mseYes('hevc8', 'hevc10')), mediaCapabilities: pqCapabilities(PQ_NO) },
        { userAgent: UA.chromeWin, MediaSource: mediaSource(mseYes(...ALL_HEVC)), mediaCapabilities: pqCapabilities(PQ_SOFTWARE) },
        { userAgent: UA.iPhone, canPlayType: canPlay(nativeYes('hevc10-2160')), mediaCapabilities: pqCapabilities(PQ_YES) },
        { userAgent: UA.firefoxWin, MediaSource: mediaSource(mseYes(...ALL_HEVC)) },
        {},
    ];
    for (const env of envs) {
        const tokens = await decodeTokens(env);
        const got = await probeCodecSupport(env);
        assert.equal(got.decode, tokens.join(','));
        assert.equal(got.decode_path, decodePath(env));
        for (const t of DECODE_TOKENS) assert.equal(got[t], tokens.includes(t), t);
    }
    const full = await probeCodecSupport(envs[1]);
    assert.equal(full.decode, 'hevc8,hevc10,hevc8-2160,hevc10-2160,hevc-high,hdr-pq');
    assert.equal(full.decode_path, 'mse');
    // The existing keys keep their meaning next to the new ones: hvc is
    // still the L120 question in its own spelling.
    assert.equal(full.hvc, false);
});

test('dynamic-range: high, standard, or unknown where the feature is missing', async () => {
    const mm = (matching) => (q) => ({ matches: q === matching });
    assert.equal(dynamicRange({ matchMedia: mm('(dynamic-range: high)') }), 'high');
    assert.equal(dynamicRange({ matchMedia: mm('(dynamic-range: standard)') }), 'standard');
    assert.equal(dynamicRange({ matchMedia: mm('nothing') }), 'unknown', 'an unknown media feature matches neither');
    assert.equal(dynamicRange({ matchMedia: () => { throw new Error('x'); } }), 'unknown');
    assert.equal(dynamicRange({}), 'unknown');
    assert.equal((await probeCodecSupport({ matchMedia: mm('(dynamic-range: high)') }))['dynamic-range'], 'high');
});

test('envFromWindow: navigator facts, and matchMedia called on the window', () => {
    const win = {
        navigator: { userAgent: UA.iPhone, platform: 'iPhone', maxTouchPoints: 5, mediaCapabilities: { decodingInfo() {} } },
        matchMedia(q) {
            assert.equal(this, win, 'an unbound matchMedia throws Illegal invocation');
            return { matches: q === '(dynamic-range: high)' };
        },
        document: { createElement: () => ({ canPlayType: () => '' }) },
    };
    const env = envFromWindow(win);
    assert.equal(env.userAgent, UA.iPhone);
    assert.equal(env.maxTouchPoints, 5);
    assert.equal(isIOSLike(env), true);
    assert.equal(dynamicRange(env), 'high');
});

// ---- pinned to the player: the path is the one it will take ----------------

const HERE = path.dirname(fileURLToPath(import.meta.url));

test('hlsJsSupported agrees with the installed Hls.isSupported()', (t) => {
    // Our copy against the real thing over the same fakes; hls.js reads
    // them off `self`. The two ManagedMediaSource cases are the ones that
    // tell "prefers the managed one" from "prefers MediaSource".
    const had = Object.prototype.hasOwnProperty.call(globalThis, 'self');
    const before = globalThis.self;
    t.after(() => { if (had) globalThis.self = before; else delete globalThis.self; });

    const goodSB = function () {};
    goodSB.prototype = { appendBuffer() {}, remove() {} };
    const cases = {
        nothing: {},
        'MediaSource with H.264': { MediaSource: mediaSource([H264]) },
        'MediaSource, nothing it can play': { MediaSource: mediaSource([]) },
        'MediaSource, audio only': { MediaSource: mediaSource(['audio/mp4;codecs=mp4a.40.2']) },
        'MediaSource, AV1 only': { MediaSource: mediaSource(['video/mp4;codecs=av01.0.01M.08']) },
        'managed yes, plain no': { ManagedMediaSource: mediaSource([H264]), MediaSource: mediaSource([]) },
        'managed no, plain yes': { ManagedMediaSource: mediaSource([]), MediaSource: mediaSource([H264]) },
        'WebKitMediaSource': { WebKitMediaSource: mediaSource([H264]) },
        'a broken SourceBuffer': { MediaSource: mediaSource([H264]), SourceBuffer: { prototype: {} } },
        'a good SourceBuffer': { MediaSource: mediaSource([H264]), SourceBuffer: goodSB },
        'a WebKitSourceBuffer without remove': { MediaSource: mediaSource([H264]), WebKitSourceBuffer: { prototype: { appendBuffer() {} } } },
        'isTypeSupported not a function': { MediaSource: Object.assign(function () {}, { isTypeSupported: 'yes' }) },
    };
    for (const [name, env] of Object.entries(cases)) {
        globalThis.self = env;
        assert.equal(hlsJsSupported(env), Hls.isSupported(), name);
    }
});

test('the Firefox-on-Windows rule is the one the installed hls.js applies', () => {
    const dist = readFileSync(createRequire(import.meta.url).resolve('hls.js/dist/hls.mjs'), 'utf8');
    assert.ok(dist.includes(`${HEVC_ANSWER_INACCURATE.source}/i.test(navigator.userAgent)`),
        'hls.js changed its rule for HEVC answers it does not trust; follow it here');
    assert.equal(HEVC_ANSWER_INACCURATE.test(UA.firefoxWin), true);
    assert.equal(HEVC_ANSWER_INACCURATE.test(UA.firefoxMac), false);
    assert.equal(HEVC_ANSWER_INACCURATE.test(UA.chromeWin), false);
});

test('isIOSLike is hls-manager.js\'s own iOS rule', () => {
    const src = readFileSync(path.join(HERE, 'hls-manager.js'), 'utf8');
    assert.ok(src.includes('/iPad|iPhone|iPod/.test(navigator.userAgent) ||\n    (navigator.platform === \'MacIntel\' && navigator.maxTouchPoints > 1)'),
        'the player changed when it plays HLS natively; change isIOSLike with it');
    assert.ok(src.includes('if (!Hls || !Hls.isSupported() || isIOS) {'),
        'the player changed how it picks hls.js over native HLS; change decodePath with it');
});

// ---- playback-quality ------------------------------------------------------

class PlayingVideo extends EventTarget {
    constructor({ quality = { droppedVideoFrames: 12, totalVideoFrames: 1440 }, height = 2160 } = {}) {
        super();
        this.currentTime = 0;
        this.videoHeight = height;
        this.playbackRate = 1;
        if (quality !== null) this.getVideoPlaybackQuality = () => quality;
    }

    // play advances the media by `seconds` in timeupdate steps of `step`.
    play(seconds, step = 0.25) {
        for (let t = 0; t < seconds - 1e-9; t += step) {
            this.currentTime += step;
            this.dispatchEvent(new Event('timeupdate'));
        }
    }

    seek(to) {
        this.dispatchEvent(new Event('seeking'));
        this.currentTime = to;
        this.dispatchEvent(new Event('timeupdate'));
    }
}

test('afterPlayed: once, at a minute of media played; not before', () => {
    const v = new PlayingVideo();
    const fired = [];
    afterPlayed(v, QUALITY_AFTER_S, (played, hidden) => fired.push({ played, hidden }));
    v.dispatchEvent(new Event('timeupdate'));
    v.play(59.5);
    assert.equal(fired.length, 0);
    v.play(1);
    assert.equal(fired.length, 1);
    assert.ok(fired[0].played >= 60 && fired[0].played < 60.5);
    assert.equal(fired[0].hidden, false);
    v.play(120);
    assert.equal(fired.length, 1, 'once');
    assert.equal(QUALITY_AFTER_S, 60);
});

test('afterPlayed: seeks and jumps are not playback', () => {
    const v = new PlayingVideo();
    let n = 0;
    afterPlayed(v, 60, () => { n += 1; });
    v.dispatchEvent(new Event('timeupdate'));
    v.play(30);
    // A seek forward by twenty minutes.
    v.seek(1230);
    // A jump of 2.5 s (a gap hls.js skipped).
    v.currentTime += 2.5;
    v.dispatchEvent(new Event('timeupdate'));
    // A seek back by a second, and a new source from 0.
    v.seek(v.currentTime - 1);
    v.seek(0);
    // Paused: time does not move, timeupdate may still fire.
    for (let i = 0; i < 10; i++) v.dispatchEvent(new Event('timeupdate'));
    v.play(29.5);
    assert.equal(n, 0, '59.5 s actually played');
    v.play(0.5);
    assert.equal(n, 1);
});

test('afterPlayed: a step taken while hidden marks the reading', () => {
    const v = new PlayingVideo();
    let hiddenNow = false;
    let got = null;
    afterPlayed(v, 60, (played, hidden) => { got = hidden; }, () => hiddenNow);
    v.dispatchEvent(new Event('timeupdate'));
    v.play(20);
    hiddenNow = true;
    v.play(5);
    hiddenNow = false;
    v.play(40);
    assert.equal(got, true);
});

test('afterPlayed: the cleanup before the mark means never', () => {
    const v = new PlayingVideo();
    let n = 0;
    const stop = afterPlayed(v, 60, () => { n += 1; });
    v.dispatchEvent(new Event('timeupdate'));
    v.play(30);
    stop();
    v.play(60);
    assert.equal(n, 0);
});

test('playbackQuality: a pair of counts or null', () => {
    assert.deepEqual(playbackQuality(new PlayingVideo()), { dropped: 12, total: 1440 });
    assert.equal(playbackQuality(new PlayingVideo({ quality: null })), null, 'no API');
    assert.equal(playbackQuality({ getVideoPlaybackQuality() { throw new Error('x'); } }), null);
    assert.equal(playbackQuality({ getVideoPlaybackQuality: () => ({ droppedVideoFrames: -1, totalVideoFrames: 3 }) }), null);
    assert.equal(playbackQuality({ getVideoPlaybackQuality: () => ({ totalVideoFrames: 3 }) }), null);
    assert.equal(playbackQuality({ getVideoPlaybackQuality: () => null }), null);
    assert.equal(playbackQuality(null), null);
});

// qualityPage is one page load for the playback-quality event.
function qualityPage({ umami = true } = {}) {
    const events = [];
    const win = umami ? { umami: { track: (name, data) => events.push({ name, data }) } } : {};
    const queue = [];
    const deps = {
        win,
        schedule: (fn) => queue.push(fn),
        tokens: async () => ['hevc8', 'hevc10'],
        hidden: () => false,
    };
    return {
        win, events, deps,
        run: async () => { while (queue.length) await queue.shift()(); },
    };
}

test('playback-quality: one event after a minute, with the counts, the stream and the declaration', async () => {
    const p = qualityPage();
    const v = new PlayingVideo();
    let asked = 0;
    watchPlaybackQuality(v, () => { asked += 1; return { src: 'hevc', tc: false, pl: 'direct', emb: false }; }, p.deps);
    v.dispatchEvent(new Event('timeupdate'));
    v.play(30);
    await p.run();
    assert.equal(p.events.length, 0);
    assert.equal(asked, 0, 'the stream is read at the mark, when the player has settled');
    v.play(30);
    assert.equal(p.events.length, 0, 'deferred: nothing on the timeupdate path');
    await p.run();
    assert.deepEqual(p.events, [{
        name: QUALITY_EVENT,
        data: {
            dropped: 12, total: 1440, drop_pct: 0.83, played: 60, height: 2160, rate: 1, hidden: false,
            src: 'hevc', tc: false, pl: 'direct', emb: false,
            decode: 'hevc8,hevc10',
        },
    }]);
    assert.equal(p.win[QUALITY_PAGE_FLAG], true);
});

test('playback-quality: once per page — the next episode on it does not report', async () => {
    const p = qualityPage();
    const first = new PlayingVideo();
    const second = new PlayingVideo();
    watchPlaybackQuality(first, {}, p.deps);
    first.dispatchEvent(new Event('timeupdate'));
    first.play(61);
    watchPlaybackQuality(second, {}, p.deps);
    second.dispatchEvent(new Event('timeupdate'));
    second.play(61);
    await p.run();
    assert.equal(p.events.length, 1);
});

test('playback-quality: no frames at all is reported, without a share', async () => {
    const p = qualityPage();
    const v = new PlayingVideo({ quality: { droppedVideoFrames: 0, totalVideoFrames: 0 } });
    watchPlaybackQuality(v, {}, p.deps);
    v.dispatchEvent(new Event('timeupdate'));
    v.play(61);
    await p.run();
    assert.equal(p.events.length, 1);
    assert.equal(p.events[0].data.total, 0);
    assert.ok(!('drop_pct' in p.events[0].data));
});

test('playback-quality: no API, no umami, or a failing declaration', async () => {
    // No getVideoPlaybackQuality: nothing, and the page stays unmarked.
    const a = qualityPage();
    const bare = new PlayingVideo({ quality: null });
    watchPlaybackQuality(bare, {}, a.deps);
    bare.dispatchEvent(new Event('timeupdate'));
    bare.play(61);
    await a.run();
    assert.equal(a.events.length, 0);
    assert.notEqual(a.win[QUALITY_PAGE_FLAG], true);

    // No umami at the mark: nothing sent, nothing marked.
    const b = qualityPage({ umami: false });
    const v = new PlayingVideo();
    watchPlaybackQuality(v, {}, b.deps);
    v.dispatchEvent(new Event('timeupdate'));
    v.play(61);
    await b.run();
    assert.notEqual(b.win[QUALITY_PAGE_FLAG], true);

    // The declaration throws: the frames still go, with decode ''.
    const c = qualityPage();
    c.deps.tokens = async () => { throw new Error('boom'); };
    const w = new PlayingVideo();
    watchPlaybackQuality(w, { src: 'h264' }, c.deps);
    w.dispatchEvent(new Event('timeupdate'));
    w.play(61);
    await c.run();
    assert.equal(c.events.length, 1);
    assert.equal(c.events[0].data.decode, '');
    assert.equal(c.events[0].data.src, 'h264');
});

test('playback-quality never throws', () => {
    assert.doesNotThrow(() => watchPlaybackQuality(null, {}, { win: {} })());
    assert.doesNotThrow(() => watchPlaybackQuality({}, {}, { win: {} })(), 'an element without listeners');
    const p = qualityPage();
    const v = new PlayingVideo();
    watchPlaybackQuality(v, () => { throw new Error('extra'); }, { ...p.deps, schedule: () => { throw new Error('schedule'); } });
    v.dispatchEvent(new Event('timeupdate'));
    assert.doesNotThrow(() => v.play(61));
});

// ---- declarationSupport: what the page declares ---------------------------

const settled = (p) => Promise.race([p.then((v) => ({ v })), new Promise((r) => setTimeout(() => r('pending'), 30))]);

test('declarationSupport: the HEVC tokens at once, the same ones decodeTokens finds', async () => {
    const env = { userAgent: UA.chromeWin, MediaSource: mediaSource(mseYes(...ALL_HEVC)), mediaCapabilities: pqCapabilities(PQ_YES) };
    const d = declarationSupport(env);
    assert.equal(d.path, 'mse');
    assert.deepEqual(d.hevc, (await decodeTokens(env)).filter((t) => ALL_HEVC.includes(t)));
    assert.deepEqual(await settled(d.pq), { v: true });
});

// The event gives decodingInfo 3 s and counts silence as "no"; the
// declaration must not -- a check that did not answer is not a browser
// that cannot decode. Silence stays pending; every real answer settles.
test('declarationSupport: hdr-pq has no deadline -- silence stays pending, answers settle', async () => {
    const base = { userAgent: UA.chromeWin, MediaSource: mediaSource(mseYes('hevc10-2160')) };
    assert.equal(await settled(declarationSupport({ ...base, mediaCapabilities: pqCapabilities(() => new Promise(() => {})) }).pq), 'pending');
    for (const [name, pq, want] of [
        ['yes', PQ_YES, true],
        ['no', PQ_NO, false],
        ['a rejection', () => Promise.reject(new Error('nope')), false],
        ['a throw', () => { throw new TypeError('bad config'); }, false],
        ['garbage', () => Promise.resolve('yes'), false],
    ]) {
        assert.deepEqual(await settled(declarationSupport({ ...base, mediaCapabilities: pqCapabilities(pq) }).pq), { v: want }, name);
    }
    assert.deepEqual(await settled(declarationSupport(base).pq), { v: false }, 'no mediaCapabilities: never declared');
    const mc = pqCapabilities(PQ_YES);
    const none = declarationSupport({ userAgent: UA.chromeWin, MediaSource: mediaSource(mseYes()), mediaCapabilities: mc });
    assert.deepEqual(none.hevc, []);
    assert.deepEqual(await settled(none.pq), { v: false });
    assert.equal(videoCalls(mc).length, 0, 'without an HEVC token PQ is not asked');
});

// ---- the audio tokens: aac51, ac3, ec3 --------------------------------------

const dolbyYes = (...codecs) => codecs.map((c) => audioMseType(c));

test('audio on the MSE path: AAC 5.1 through decodingInfo with six channels, Dolby through isTypeSupported', async () => {
    const mc = pqCapabilities(PQ_NO, AAC_YES);
    const env = {
        userAgent: UA.chromeWin,
        MediaSource: mediaSource([...mseYes(), ...dolbyYes('ec-3')]),
        mediaCapabilities: mc,
    };
    assert.deepEqual(dolbyDecodeTokens(env), ['ec3']);
    assert.deepEqual(await decodeTokens(env), ['aac51', 'ec3'], 'no HEVC here, the audio all the same');
    // The one audio question, as hls.js asks it for a level whose audio
    // has more than two CHANNELS: media-source, its spelling, a channel count.
    const [c] = audioCalls(mc);
    assert.equal(audioCalls(mc).length, 1);
    assert.equal(c.type, 'media-source');
    assert.deepEqual(c.audio, { contentType: 'audio/mp4;codecs=mp4a.40.2', channels: '6', bitrate: 384000, samplerate: 48000 });
    assert.ok(!('video' in c), 'an audio question only');
    // isTypeSupported has no word for channels: an MSE that says yes to AAC
    // declares no aac51 when decodingInfo says no.
    const no = { ...env, MediaSource: mediaSource([...mseYes(), audioMseType('mp4a.40.2')]), mediaCapabilities: pqCapabilities(PQ_NO) };
    assert.deepEqual(await decodeTokens(no), []);
});

test('any support counts for audio too: a software AAC 5.1 decoder declares', async () => {
    const env = {
        userAgent: UA.chromeWin,
        MediaSource: mediaSource(mseYes()),
        mediaCapabilities: pqCapabilities(PQ_NO, { supported: true, smooth: false, powerEfficient: false }),
    };
    assert.deepEqual(await decodeTokens(env), ['aac51']);
});

// hls.js distrusts Firefox-on-Windows's HEVC answers only (codecs.ts,
// mediacapabilities-helper.ts): its audio answers are what the player
// goes by, and they are declared.
test('audio is independent of HEVC: Firefox on Windows declares its audio decoders', async () => {
    const env = (ua) => ({
        userAgent: ua,
        MediaSource: mediaSource([...mseYes(...ALL_HEVC), ...dolbyYes('ac-3', 'ec-3')]),
        mediaCapabilities: pqCapabilities(PQ_YES, AAC_YES),
    });
    const ffWin = env(UA.firefoxWin);
    assert.deepEqual(hevcDecodeTokens(ffWin), [], 'no HEVC, as before');
    assert.deepEqual(dolbyDecodeTokens(ffWin), ['ac3', 'ec3']);
    assert.deepEqual(await decodeTokens(ffWin), ['aac51', 'ac3', 'ec3']);
    assert.deepEqual(await settled(declarationSupport(ffWin).audio), { v: ['aac51', 'ac3', 'ec3'] });
    // And a browser with every HEVC token gets them all, video first.
    assert.deepEqual(await decodeTokens(env(UA.chromeWin)), DECODE_TOKENS);
});

test('Safari on a Mac: Dolby is asked of the ManagedMediaSource hls.js prefers', () => {
    const env = {
        userAgent: UA.safariMac,
        platform: 'MacIntel',
        maxTouchPoints: 0,
        MediaSource: mediaSource([H264]),
        ManagedMediaSource: mediaSource([H264, ...dolbyYes('ac-3', 'ec-3')]),
    };
    assert.equal(decodePath(env), 'mse');
    assert.deepEqual(dolbyDecodeTokens(env), ['ac3', 'ec3']);
    const plain = { ...env, MediaSource: mediaSource([H264, ...dolbyYes('ec-3')]), ManagedMediaSource: mediaSource([H264]) };
    assert.deepEqual(dolbyDecodeTokens(plain), [], 'the plain MediaSource is not the one hls.js asks');
});

test('iPhone: the element answers every audio token; decodingInfo is not asked about AAC', async () => {
    const mc = pqCapabilities(PQ_NO, AAC_YES);
    const env = {
        userAgent: UA.iPhone,
        ManagedMediaSource: mediaSource([...mseYes(), ...dolbyYes('ac-3', 'ec-3')]),
        canPlayType: canPlay({ [HLS]: 'maybe', [audioFileType('mp4a.40.2')]: 'maybe', [audioFileType('ec-3')]: 'probably' }),
        mediaCapabilities: mc,
    };
    assert.equal(decodePath(env), 'native');
    assert.deepEqual(await decodeTokens(env), ['aac51', 'ec3'], 'a "maybe" is a yes; the MSE is not asked');
    assert.equal(audioCalls(mc).length, 0);
    assert.deepEqual(await settled(declarationSupport(env).audio), { v: ['aac51', 'ec3'] });
});

test('no HLS here: no audio token, no question', async () => {
    const mc = pqCapabilities(PQ_YES, AAC_YES);
    const env = {
        userAgent: UA.chromeWin,
        canPlayType: canPlay({ [audioFileType('mp4a.40.2')]: 'probably', [audioFileType('ec-3')]: 'probably' }),
        mediaCapabilities: mc,
    };
    assert.equal(decodePath(env), 'none');
    assert.deepEqual(await decodeTokens(env), []);
    assert.deepEqual(await settled(declarationSupport(env).audio), { v: [] });
    assert.equal(mc.calls.length, 0);
});

test('aac51 for the event: no decodingInfo, a rejection, a throw, garbage or a timeout is no token -- Dolby and HEVC stay', { timeout: 2000 }, async () => {
    const base = { userAgent: UA.chromeWin, MediaSource: mediaSource([...mseYes('hevc8'), ...dolbyYes('ec-3')]) };
    assert.deepEqual(await decodeTokens(base), ['hevc8', 'ec3'], 'no mediaCapabilities');
    assert.deepEqual(await decodeTokens({ ...base, mediaCapabilities: {} }), ['hevc8', 'ec3']);
    for (const aac of [
        () => Promise.reject(new Error('nope')),
        () => { throw new TypeError('bad config'); },
        () => Promise.resolve('yes'),
        () => Promise.resolve({ supported: 'true' }),
        () => new Promise(() => {}),
    ]) {
        assert.deepEqual(await decodeTokens({ ...base, mediaCapabilities: pqCapabilities(PQ_NO, aac) }, { timeoutMs: 5 }), ['hevc8', 'ec3']);
    }
});

// The event gives decodingInfo 3 s; the declaration gives it all the time
// it takes -- the rule of hdr-pq. Silence keeps the audio part pending, and
// neither part waits for the other.
test('declarationSupport: the audio part has no deadline, and does not wait for PQ', async () => {
    const base = { userAgent: UA.chromeWin, MediaSource: mediaSource([...mseYes('hevc10-2160'), ...dolbyYes('ac-3')]) };
    const silentAac = declarationSupport({ ...base, mediaCapabilities: pqCapabilities(PQ_YES, () => new Promise(() => {})) });
    assert.equal(await settled(silentAac.audio), 'pending', 'no answer is not "no"');
    assert.deepEqual(await settled(silentAac.pq), { v: true }, 'PQ answers without the audio');
    const silentPq = declarationSupport({ ...base, mediaCapabilities: pqCapabilities(() => new Promise(() => {}), AAC_YES) });
    assert.deepEqual(await settled(silentPq.audio), { v: ['aac51', 'ac3'] }, 'the audio answers without PQ');
    assert.equal(await settled(silentPq.pq), 'pending');
    for (const [name, aac, want] of [
        ['yes', AAC_YES, ['aac51', 'ac3']],
        ['no', PQ_NO, ['ac3']],
        ['a rejection', () => Promise.reject(new Error('nope')), ['ac3']],
        ['a throw', () => { throw new TypeError('bad config'); }, ['ac3']],
        ['garbage', () => Promise.resolve('yes'), ['ac3']],
    ]) {
        assert.deepEqual(await settled(declarationSupport({ ...base, mediaCapabilities: pqCapabilities(PQ_NO, aac) }).audio), { v: want }, name);
    }
    assert.deepEqual(await settled(declarationSupport(base).audio), { v: ['ac3'] }, 'no mediaCapabilities: aac51 never declared');
});

test('the event carries one boolean per audio token and the audio in `decode`', async () => {
    const env = {
        userAgent: UA.firefoxWin,
        MediaSource: mediaSource([...mseYes(...ALL_HEVC), ...dolbyYes('ec-3')]),
        mediaCapabilities: pqCapabilities(PQ_YES, AAC_YES),
    };
    const got = await probeCodecSupport(env);
    assert.equal(got.aac51, true);
    assert.equal(got.ac3, false);
    assert.equal(got.ec3, true);
    assert.equal(got.hevc8, false, 'the HEVC ones as before');
    assert.equal(got.decode, 'aac51,ec3');
    assert.equal(got.decode_path, 'mse');
});

// ---- pinned to the installed hls.js: what it asks about audio --------------

test('hls.js asks about an audio codec in this spelling, of the MediaSource it prefers', () => {
    const dist = readFileSync(createRequire(import.meta.url).resolve('hls.js/dist/hls.mjs'), 'utf8');
    assert.ok(dist.includes('function mimeTypeForCodec(codec, type) {\n  return `${type}/mp4;codecs=${codec}`;\n}'),
        'hls.js changed how it spells a codec for isTypeSupported; change audioMseType with it');
    assert.equal(audioMseType('ac-3'), 'audio/mp4;codecs=ac-3');
    assert.ok(dist.includes("isAudioSupported(codec) {\n    return areCodecsMediaSourceSupported(codec, 'audio', this.hls.config.preferManagedMediaSource);"),
        'hls.js changed which MediaSource it asks about audio codecs; change dolbyDecodeTokens with it');
    // Its own AAC 5.1 question (a rendition with CHANNELS over 2): the same
    // media-source audio configuration, a channel count as a string.
    assert.ok(dist.includes("contentType: mimeTypeForCodec(audioCodec, 'audio'),"));
    assert.ok(dist.includes("audioConfiguration.channels = '' + channelsNumber;"));
});

test('hls.js distrusts the HEVC answers of Firefox on Windows, and no audio answer', () => {
    const dist = readFileSync(createRequire(import.meta.url).resolve('hls.js/dist/hls.mjs'), 'utf8');
    const uses = dist.split('userAgentHevcSupportIsInaccurate()').length - 1;
    assert.equal(uses, 2, 'hls.js distrusts a browser\'s answer in a new place: if it is an audio one, follow it here');
    assert.ok(dist.includes('const lowerPriority = limitedHevcSupport && isHEVC(fourCC);'));
    assert.ok(dist.includes("videoCodecs.split(',').some(videoCodec => isHEVC(videoCodec)) && userAgentHevcSupportIsInaccurate()"));
    // E-AC-3 cannot come in MPEG-TS at all: the transcoder copies it only
    // into fMP4 (a passthrough session), so ec3 is safe to declare on any
    // route.
    assert.ok(dist.includes("new Error('Unsupported EC-3 in M2TS found')"));
});
