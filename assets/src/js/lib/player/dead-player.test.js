import { test } from 'node:test';
import assert from 'node:assert/strict';
import Hls from 'hls.js';
import { createDeadPlayerWatch, sourceKind, DEAD_AFTER_MS, CHECK_EVERY_MS, DEAD_EVENT, REVIVED_EVENT, RECOVERY_STORM } from './dead-player.js';

// The element, as far as the watch reads it.
function fakeVideo({ src = 'https://x.test/s/index.m3u8', route = '', decode, audioClass } = {}) {
    const handlers = new Map();
    return {
        paused: true,
        autoplay: false,
        currentTime: 0,
        readyState: 0,
        networkState: 1,
        error: null,
        currentSrc: src,
        buf: [],
        dataset: { videoRoute: route, ...(decode !== undefined ? { decode } : {}), ...(audioClass !== undefined ? { audioClass } : {}) },
        get buffered() {
            const b = this.buf;
            return { length: b.length, start: (i) => b[i][0], end: (i) => b[i][1] };
        },
        getAttribute(n) { return n === 'src' ? this.currentSrc : null; },
        addEventListener(ev, fn) {
            if (!handlers.has(ev)) handlers.set(ev, new Set());
            handlers.get(ev).add(fn);
        },
        removeEventListener(ev, fn) { (handlers.get(ev) || new Set()).delete(fn); },
        fire(ev) { for (const fn of [...(handlers.get(ev) || [])]) fn({ type: ev }); },
        listeners() { let n = 0; for (const s of handlers.values()) n += s.size; return n; },
        play() { this.paused = false; this.fire('play'); },
        pause() { this.paused = true; this.fire('pause'); },
    };
}

function fakeHls(media) {
    const handlers = new Map();
    return {
        media,
        inFlightFragments: { main: { frag: null, state: 'IDLE' } },
        on(ev, fn) {
            if (!handlers.has(ev)) handlers.set(ev, []);
            handlers.get(ev).push(fn);
        },
        off(ev, fn) {
            const a = handlers.get(ev) || [];
            const i = a.indexOf(fn);
            if (i >= 0) a.splice(i, 1);
        },
        trigger(ev, data = {}) { for (const fn of [...(handlers.get(ev) || [])]) fn(ev, data); },
        listeners() { let n = 0; for (const a of handlers.values()) n += a.length; return n; },
    };
}

function fakeDoc() {
    const handlers = new Set();
    return {
        hidden: false,
        addEventListener(ev, fn) { handlers.add(fn); },
        removeEventListener(ev, fn) { handlers.delete(fn); },
        setHidden(h) { this.hidden = h; for (const fn of [...handlers]) fn(); },
    };
}

// A watch on a fake clock: advance(ms) runs the interval as the browser
// would; `events` is what went to Umami.
function setup({ video = fakeVideo(), hls = null, handled = () => false } = {}) {
    let t = 1000000;
    let tick = null;
    const events = [];
    const doc = fakeDoc();
    const ref = { hls };
    const watch = createDeadPlayerWatch({
        video, Hls, doc, handled,
        getHls: () => ref.hls,
        track: (name, data) => events.push({ name, data }),
        now: () => t,
        setInterval: (fn) => { tick = fn; return 1; },
        clearInterval: () => { tick = null; },
    });
    const advance = (ms, each = () => {}) => {
        for (let d = 0; d < ms; d += CHECK_EVERY_MS) {
            t += CHECK_EVERY_MS;
            each(t);
            if (tick) tick();
        }
    };
    return { video, ref, doc, watch, events, advance, now: () => t, ticking: () => tick !== null };
}

const dead = (events) => events.filter((e) => e.name === DEAD_EVENT);

test('the 29.09 death: hls.js gone, a revoked blob, MediaError 4 -- one player-dead after the quiet', () => {
    const s = setup();
    s.video.play();
    s.video.currentSrc = 'blob:https://webtor.io/8a61e4c8';
    s.video.error = { code: 4 };
    s.video.networkState = 3;
    s.advance(DEAD_AFTER_MS - CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 0);
    s.advance(DEAD_AFTER_MS * 3);
    assert.equal(dead(s.events).length, 1);
    assert.deepEqual(dead(s.events)[0].data, {
        why: 'quiet', path: 'blob', hls: 'none', route: '', audio: 'none', err: 4, rs: 0, ns: 3, inflight: 0, got: 'none', recoveries: 0, waited_s: 30,
    });
});

test('a fragment arriving at the cap for five minutes is not dead', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const stats = { loaded: 0, loading: { start: 1, first: 2 } };
    hls.inFlightFragments = { main: { frag: { sn: 0, stats }, state: 'FRAG_LOADING' } };
    const s = setup({ video, hls });
    s.video.play();
    s.advance(5 * 60000, () => { stats.loaded += 1000; });
    assert.equal(dead(s.events).length, 0);
    // The bytes stop: dead after the quiet.
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.inflight, 1);
    assert.equal(dead(s.events)[0].data.hls, 'on');
});

test('the manifest held by the server is waiting, not dead; answered and then nothing is', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS); // the watch binds the instance
    hls.trigger(Hls.Events.MANIFEST_LOADING);
    s.advance(90000);
    assert.equal(dead(s.events).length, 0);
    hls.trigger(Hls.Events.MANIFEST_LOADED);
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.got, 'playlist');
});

test('before the first fragment, playlist reloads are hls.js waiting for the first segment', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS);
    s.advance(3 * 60000, (t) => {
        if (t % 6000 === 0) { hls.trigger(Hls.Events.LEVEL_LOADING); hls.trigger(Hls.Events.LEVEL_LOADED); }
    });
    assert.equal(dead(s.events).length, 0);
});

// Chrome, a load() on the element hls.js is attached to: fragments were in,
// hls.js goes on polling the live playlist, nothing reaches the element.
test('after the first fragment, playlist reloads alone are not progress', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS);
    hls.trigger(Hls.Events.FRAG_LOADED);
    s.advance(DEAD_AFTER_MS + 2 * CHECK_EVERY_MS, (t) => {
        if (t % 6000 === 0) { hls.trigger(Hls.Events.LEVEL_LOADING); hls.trigger(Hls.Events.LEVEL_LOADED); }
    });
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.got, 'frags');
});

test('a new manifest (a session seek before the start) waits for its first segment again', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS);
    hls.trigger(Hls.Events.FRAG_LOADED);
    hls.trigger(Hls.Events.MANIFEST_LOADING);
    hls.trigger(Hls.Events.MANIFEST_LOADED);
    s.advance(2 * 60000, (t) => {
        if (t % 6000 === 0) { hls.trigger(Hls.Events.LEVEL_LOADING); hls.trigger(Hls.Events.LEVEL_LOADED); }
    });
    assert.equal(dead(s.events).length, 0);
});

test('a paused player is not watched; play again watches afresh', () => {
    const s = setup();
    s.video.play();
    s.advance(10000);
    s.video.pause();
    s.advance(5 * 60000);
    assert.equal(dead(s.events).length, 0);
    s.video.play();
    s.advance(DEAD_AFTER_MS - CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 0);
    s.advance(2 * CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
});

test('a hidden tab is not watched: the quiet counts from when it is visible', () => {
    const s = setup();
    s.video.play();
    s.advance(10000);
    s.doc.setHidden(true);
    s.advance(5 * 60000);
    assert.equal(dead(s.events).length, 0);
    s.doc.setHidden(false);
    s.advance(DEAD_AFTER_MS - CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 0);
    s.advance(2 * CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
});

test('a restart a guard has begun is its business: no event, and the watch lets go', () => {
    let done = false;
    const s = setup({ handled: () => done });
    s.video.play();
    s.video.error = { code: 4 };
    s.advance(10000);
    done = true;
    s.advance(5 * 60000);
    assert.equal(s.events.length, 0);
    assert.equal(s.video.listeners(), 0);
});

test('it played after all: player-revived with the whole wait', () => {
    const s = setup();
    s.video.play();
    s.advance(40000);
    assert.equal(dead(s.events).length, 1);
    s.video.fire('playing');
    assert.deepEqual(s.events.map((e) => e.name), [DEAD_EVENT, REVIVED_EVENT]);
    assert.equal(s.events[1].data.waited_s, 40);
    assert.equal(s.video.listeners(), 0);
});

// The start's audio class on both events (passthrough.js startAudioClass):
// the master's Dolby or AAC 5.1 where the start declared an audio token, else
// none -- dead players and their revivals counted per class, as stream-start
// and the fallbacks are.
for (const [name, decode, audioClass, want] of [
    ['Dolby declared and made', 'hevc10,hevc10-2160,aac51,ac3,ec3', 'dolby', 'dolby'],
    ['AAC 5.1 declared and made', 'hevc8,aac51', 'aac51', 'aac51'],
    ['audio declared, the stereo of old made', 'hevc8,aac51,ac3,ec3', undefined, 'none'],
    ['no audio token declared (?audio=off)', 'hevc8,hevc10', 'dolby', 'none'],
    ['no declaration', undefined, 'dolby', 'none'],
]) {
    test(`player-dead and player-revived carry the start's audio class: ${name}`, () => {
        const s = setup({ video: fakeVideo({ route: 'passthrough', decode, audioClass }) });
        s.video.play();
        s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
        assert.equal(dead(s.events).length, 1);
        assert.equal(dead(s.events)[0].data.audio, want);
        assert.equal(dead(s.events)[0].data.route, 'passthrough');
        s.video.fire('playing');
        assert.equal(s.events[1].name, REVIVED_EVENT);
        assert.equal(s.events[1].data.audio, want);
    });
}

test('after player-dead no timer runs; the clock moving later is a revival', () => {
    const s = setup();
    s.video.play();
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
    assert.equal(s.ticking(), false, 'no timer left behind');
    s.advance(10 * 60000);
    assert.equal(s.events.length, 1, 'nothing more while it stays dead');
    s.video.currentTime = 3;
    s.video.fire('timeupdate');
    assert.deepEqual(s.events.map((e) => e.name), [DEAD_EVENT, REVIVED_EVENT]);
    s.video.fire('playing');
    assert.equal(s.events.length, 2, 'one revival');
});

test('an element taken off the page lets the watch go', () => {
    const s = setup();
    s.video.isConnected = true;
    s.video.play();
    s.advance(10000);
    s.video.isConnected = false;
    s.advance(5 * 60000);
    assert.equal(s.events.length, 0);
    assert.equal(s.video.listeners(), 0);
});

test('the clock moving is a start, `playing` or not', () => {
    const s = setup();
    s.video.play();
    s.advance(10000);
    s.video.currentTime = 1.2;
    s.advance(5 * 60000);
    assert.equal(s.events.length, 0);
    assert.equal(s.video.listeners(), 0);
});

test('a seek before the start is not a start', () => {
    const s = setup();
    s.video.play();
    s.advance(4000);
    s.video.currentTime = 120;
    s.video.fire('seeking');
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
});

test('native HLS: the element loading is waiting; an element that stopped is not', () => {
    const s = setup();
    s.video.play();
    s.video.networkState = 2;
    s.advance(3 * 60000);
    assert.equal(dead(s.events).length, 0);
    s.video.networkState = 1;
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.path, 'native');
});

test('its own loading events keep an element alive', () => {
    const s = setup();
    s.video.play();
    s.advance(3 * 60000, (t) => { if (t % 10000 === 0) s.video.fire('progress'); });
    assert.equal(dead(s.events).length, 0);
});

test('autoplay that asked before the player mounted is watched', () => {
    const video = fakeVideo();
    video.paused = false;
    const s = setup({ video });
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
});

test('dispose lets go of the element and of hls.js', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS);
    assert.ok(hls.listeners() > 0);
    s.watch.dispose();
    assert.equal(hls.listeners(), 0);
    assert.equal(video.listeners(), 0);
    s.advance(5 * 60000);
    assert.equal(s.events.length, 0);
});

test('sourceKind names the source without its URL', () => {
    const v = fakeVideo();
    assert.equal(sourceKind(v, true), 'hlsjs');
    assert.equal(sourceKind(v, false), 'native');
    v.currentSrc = 'blob:https://webtor.io/1';
    assert.equal(sourceKind(v, false), 'blob');
    v.currentSrc = 'https://x.test/movie.mp4';
    assert.equal(sourceKind(v, false), 'direct');
    v.currentSrc = '';
    assert.equal(sourceKind(v, false), 'none');
});

// The AAC 5.1 session's benches (2026-09-29, headless Chrome 154 + hls.js
// 1.6.14): two deaths during which hls.js keeps busy.

// A fragment whose bytes keep coming: hls.js at work, never quiet.
function busyHls(video) {
    const hls = fakeHls(video);
    const stats = { loaded: 0, loading: { start: 1, first: 2 } };
    hls.inFlightFragments = { main: { frag: { sn: 0, stats }, state: 'FRAG_LOADING' } };
    return { hls, grow: () => { stats.loaded += 1000; } };
}

test('an element error that holds for the quiet is dead, whatever hls.js loads', () => {
    const video = fakeVideo();
    const { hls, grow } = busyHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.video.error = { code: 4 };
    s.advance(DEAD_AFTER_MS, grow);
    assert.equal(dead(s.events).length, 0, 'the error is seen at the first check, not at the request');
    s.advance(2 * CHECK_EVERY_MS, grow);
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.why, 'error');
    assert.equal(dead(s.events)[0].data.err, 4);
    assert.equal(dead(s.events)[0].data.hls, 'on');
});

test('a recovery resets the element and the error clock', () => {
    const video = fakeVideo();
    const { hls, grow } = busyHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.video.error = { code: 3 };
    s.advance(20000, grow);
    s.video.fire('emptied');
    s.advance(20000, grow);
    assert.equal(dead(s.events).length, 0, '20 s before the reset, 20 s after');
    s.advance(12000, grow);
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.why, 'error');
});

test('a recovery storm: re-attachments and no start is dead, busy as it is', () => {
    const video = fakeVideo();
    const { hls, grow } = busyHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(DEAD_AFTER_MS - CHECK_EVERY_MS, () => { grow(); s.video.fire('emptied'); });
    assert.equal(dead(s.events).length, 0, 'not before the quiet');
    s.advance(CHECK_EVERY_MS, grow);
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.why, 'recovering');
    assert.equal(dead(s.events)[0].data.recoveries, 14);
});

test('fewer re-attachments than a storm keep a busy player alive', () => {
    const video = fakeVideo();
    const { hls, grow } = busyHls(video);
    const s = setup({ video, hls });
    s.video.play();
    for (let i = 0; i < RECOVERY_STORM - 1; i++) s.video.fire('emptied');
    s.advance(3 * 60000, grow);
    assert.equal(dead(s.events).length, 0);
});

test('hls.js errors are not progress', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS);
    hls.trigger(Hls.Events.FRAG_LOADED);
    s.advance(DEAD_AFTER_MS + 2 * CHECK_EVERY_MS, () => {
        hls.trigger(Hls.Events.ERROR, { type: 'mediaError', details: 'bufferAppendError', fatal: false });
    });
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.why, 'quiet');
});

test('an error ends the pending request it names, and no other', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS);
    hls.trigger(Hls.Events.LEVEL_LOADING);
    hls.trigger(Hls.Events.ERROR, { type: 'networkError', details: 'fragLoadError', fatal: false });
    s.advance(90000);
    assert.equal(dead(s.events).length, 0, 'the video playlist is still asked for');
    hls.trigger(Hls.Events.ERROR, { type: 'networkError', details: 'levelLoadTimeOut', fatal: false });
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
});

test('loadstart and durationchange are not progress', () => {
    const s = setup();
    s.video.play();
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS, () => { s.video.fire('loadstart'); s.video.fire('durationchange'); });
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.why, 'quiet');
});

test('re-attachments before the play request are not a storm', () => {
    const video = fakeVideo();
    const { hls, grow } = busyHls(video);
    const s = setup({ video, hls });
    for (let i = 0; i < RECOVERY_STORM; i++) s.video.fire('emptied');
    s.video.play();
    s.advance(3 * 60000, grow);
    assert.equal(dead(s.events).length, 0);
});

// The review of 2026-09-29: what arms the watch, and what is not progress.

test('a lost session: the video playlist asked again and again, answered 404, is dead', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS);
    s.advance(DEAD_AFTER_MS + 2 * CHECK_EVERY_MS, (t) => {
        if (t % 4000 === 0) {
            hls.trigger(Hls.Events.LEVEL_LOADING);
            hls.trigger(Hls.Events.ERROR, { type: 'networkError', details: 'levelLoadError', fatal: false, response: { code: 404 } });
        }
    });
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.why, 'quiet');
});

test('fragments asked again and again after the first, never answered, is dead', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(CHECK_EVERY_MS);
    hls.trigger(Hls.Events.FRAG_LOADED);
    s.advance(DEAD_AFTER_MS + 2 * CHECK_EVERY_MS, () => {
        hls.trigger(Hls.Events.FRAG_LOADING);
        hls.trigger(Hls.Events.ERROR, { type: 'networkError', details: 'fragLoadError', fatal: true, response: { code: 404 } });
    });
    assert.equal(dead(s.events).length, 1);
});

test('autoplay in the markup: a player that never gets data is watched without a play event', () => {
    const video = fakeVideo();
    video.autoplay = true;
    const s = setup({ video });
    s.advance(DEAD_AFTER_MS - CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 0);
    s.advance(2 * CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
});

test('autoplay in the markup: a MediaError before any press is still reported', () => {
    const video = fakeVideo();
    video.autoplay = true;
    const { hls, grow } = busyHls(video);
    const s = setup({ video, hls });
    s.advance(4000, grow);
    s.video.error = { code: 4 };
    s.advance(DEAD_AFTER_MS + 2 * CHECK_EVERY_MS, grow);
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.why, 'error');
});

test('autoplay refused or held: data in and paused is not dead; the viewer\'s Play arms it again', () => {
    const video = fakeVideo();
    video.autoplay = true;
    const s = setup({ video });
    s.advance(4000);
    s.video.readyState = 4;
    s.advance(5 * 60000);
    assert.equal(s.events.length, 0);
    s.video.play();
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1, 'played, nothing moved: a dead decoder');
});

test('the resume prompt\'s playing on a held (paused) element is not a start', () => {
    const s = setup();
    s.video.play();
    s.video.pause();
    s.video.fire('playing');
    s.video.play();
    s.advance(DEAD_AFTER_MS + CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1, 'the resumed start is still watched');
});

test('an error and a pause in the same moment (Chrome, an append failure before metadata) keep the watch', () => {
    const video = fakeVideo();
    const hls = fakeHls(video);
    const s = setup({ video, hls });
    s.video.play();
    s.advance(4000);
    s.video.error = { code: 4 };
    s.video.pause();
    s.advance(DEAD_AFTER_MS + 2 * CHECK_EVERY_MS);
    assert.equal(dead(s.events).length, 1);
    assert.equal(dead(s.events)[0].data.err, 4);
});
