import { test } from 'node:test';
import assert from 'node:assert/strict';
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

// ---- the probe -----------------------------------------------------------

test('Chrome-like: MSE says HEVC and AV1, the hardware answers per codec', async () => {
    const mc = capabilities({
        [MSE_TYPES.hvc]: { supported: true, smooth: true, powerEfficient: true },
        [MSE_TYPES.av1]: { supported: true, smooth: true, powerEfficient: false },
    });
    const got = await probeCodecSupport({
        MediaSource: mediaSource([MSE_TYPES.hvc, MSE_TYPES.hev, MSE_TYPES.hvc10, MSE_TYPES.av1, MSE_TYPES.av1_10, MSE_TYPES.av1_4k]),
        canPlayType: canPlay({ [MSE_TYPES.hvc]: 'probably' }),
        mediaCapabilities: mc,
    });
    assert.deepEqual(got, {
        mse: 'mse',
        hvc: true, hev: true, hvc10: true, hvc4k: false,
        av1: true, av1_10: true, av1_4k: true,
        n_hls: false, n_hvc: true,
        mc: true,
        mc_hvc: true, mc_hvc_sm: true, mc_hvc_pe: true,
        mc_av1: true, mc_av1_sm: true, mc_av1_pe: false,
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
        canPlayType: canPlay({}),
        mediaCapabilities: capabilities({
            [MSE_TYPES.av1]: { supported: true, smooth: true, powerEfficient: true },
        }),
    });
    assert.deepEqual(got, {
        mse: 'mse',
        hvc: false, hev: false, hvc10: false, hvc4k: false,
        av1: true, av1_10: true, av1_4k: false,
        n_hls: false, n_hvc: false,
        mc: true,
        mc_hvc: false, mc_hvc_sm: false, mc_hvc_pe: false,
        mc_av1: true, mc_av1_sm: true, mc_av1_pe: true,
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
        n_hls: false, n_hvc: false,
        mc: false,
        ...ALL_FALSE_MC,
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
