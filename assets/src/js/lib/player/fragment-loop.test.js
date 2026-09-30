import { test } from 'node:test';
import assert from 'node:assert/strict';
import Hls from 'hls.js';
import { createFragmentLoopWatch, loopGivesUp, LOOP_LOADS, LOOP_WINDOW_MS } from './fragment-loop.js';
import { setupHlsEvents } from './hls-manager.js';

// bus is an hls.js instance as far as the watch sees one: on/off/trigger.
function bus() {
    const handlers = new Map();
    return {
        levels: [], media: null, inFlightFragments: {},
        on(ev, fn) { handlers.set(ev, [...(handlers.get(ev) || []), fn]); },
        off(ev, fn) { handlers.set(ev, (handlers.get(ev) || []).filter((f) => f !== fn)); },
        trigger(ev, data) { for (const fn of [...(handlers.get(ev) || [])]) fn(ev, data); },
        count(ev) { return (handlers.get(ev) || []).length; },
        recoverMediaError() {}, startLoad() {}, destroy() {},
    };
}
const loaded = (sn, { type = 'main', level = 0 } = {}) => ({ frag: { sn, type, level } });

// watch: a watch on a fresh bus with its own clock; load(sn, at) loads sn at
// that time.
function watch(opts = {}) {
    const hls = bus();
    let t = 0;
    const loops = [];
    const w = createFragmentLoopWatch(hls, Hls, { onLoop: (x) => loops.push(x), now: () => t, ...opts });
    return {
        hls, w, loops,
        load(sn, at, frag = {}) { t = at; hls.trigger(Hls.Events.FRAG_LOADED, loaded(sn, frag)); },
    };
}

// The loop as it is (2026-09-30): N and N+1 again and again, ~2.7 a second.
test('two neighbours loaded again and again: told once, with what looped', () => {
    const x = watch();
    for (let i = 0; i < 40; i++) { x.load(2, i * 370); x.load(3, i * 370 + 185); }
    assert.equal(x.loops.length, 1);
    assert.deepEqual(x.loops[0], { type: 'main', sn: 2, level: 0, loads: LOOP_LOADS, windowMs: LOOP_WINDOW_MS });
    assert.equal(x.w.fired, true);
});

// Playing through: every fragment once -- nothing, however long.
test('a film played through: never', () => {
    const x = watch();
    for (let sn = 0; sn < 2000; sn++) x.load(sn, sn * 4000);
    assert.equal(x.loops.length, 0);
});

// One under the count is not a loop; the count is.
test('LOOP_LOADS - 1 loads: nothing; LOOP_LOADS: a loop', () => {
    const x = watch();
    for (let i = 0; i < LOOP_LOADS - 1; i++) x.load(5, i * 1000);
    assert.equal(x.loops.length, 0);
    x.load(5, LOOP_LOADS * 1000);
    assert.equal(x.loops.length, 1);
});

// A viewer seeking back to the same place now and then: the loads fall out
// of the window before they add up.
test('the same fragment, loaded once in each window: never', () => {
    const x = watch();
    for (let i = 0; i < 50; i++) x.load(7, i * (LOOP_WINDOW_MS / (LOOP_LOADS - 1)));
    assert.equal(x.loops.length, 0);
});

// The key is type, level and sn: the audio's fragment 2 and the video's are
// two fragments, and so are one sn on two levels (an ABR switch back and
// forth).
test('the same sn in two types or two levels: counted apart', () => {
    const x = watch();
    for (let i = 0; i < LOOP_LOADS - 1; i++) {
        x.load(2, i * 100, { type: 'main' });
        x.load(2, i * 100 + 10, { type: 'audio' });
        x.load(2, i * 100 + 20, { level: 1 });
    }
    assert.equal(x.loops.length, 0);
    x.load(2, 1000, { type: 'audio' });
    assert.deepEqual(x.loops.map((l) => [l.type, l.level, l.sn]), [['audio', 0, 2]]);
});

// A new manifest -- a session seek, a restart -- numbers its fragments from
// 0 again: what the old one loaded does not count against the new one.
test('MANIFEST_LOADING starts the count again', () => {
    const x = watch();
    for (let i = 0; i < LOOP_LOADS - 1; i++) x.load(0, i * 100);
    x.hls.trigger(Hls.Events.MANIFEST_LOADING, {});
    x.load(0, 1000);
    assert.equal(x.loops.length, 0);
});

// hls.js's init segment has no number (sn 'initSegment'): never counted.
test('init segments are not counted', () => {
    const x = watch();
    for (let i = 0; i < 20; i++) x.hls.trigger(Hls.Events.FRAG_LOADED, { frag: { sn: 'initSegment', type: 'main', level: 0 } });
    assert.equal(x.loops.length, 0);
});

// The player's answer throwing leaves hls.js's event loop alone.
test('onLoop throwing: swallowed', () => {
    const x = watch({ onLoop: () => { throw new Error('boom'); } });
    assert.doesNotThrow(() => { for (let i = 0; i < LOOP_LOADS; i++) x.load(1, i); });
    assert.equal(x.w.fired, true);
});

// DESTROYING takes every listener off; stop() does the same by hand.
test('DESTROYING: every listener off', () => {
    const x = watch();
    assert.equal(x.hls.count(Hls.Events.FRAG_LOADED), 1);
    x.hls.trigger(Hls.Events.DESTROYING, {});
    for (const ev of [Hls.Events.FRAG_LOADED, Hls.Events.MANIFEST_LOADING, Hls.Events.DESTROYING]) {
        assert.equal(x.hls.count(ev), 0, ev);
    }
});

// ---- the wiring: setupHlsEvents ------------------------------------------

const restartOpts = { now: () => 0, setInterval: () => 0, clearInterval: () => {} };

test('setupHlsEvents: the loop watch where the player asks for one', () => {
    const hls = bus();
    const loops = [];
    const restart = setupHlsEvents(hls, restartOpts, null, {}, { onLoop: (l) => loops.push(l), now: () => 0 });
    for (let i = 0; i < LOOP_LOADS; i++) hls.trigger(Hls.Events.FRAG_LOADED, loaded(4));
    assert.equal(loops.length, 1);
    assert.equal(restart.loop.fired, true);
});

test('setupHlsEvents: no loop answer, no watch', () => {
    const hls = bus();
    const restart = setupHlsEvents(hls, restartOpts);
    assert.equal(restart.loop, undefined);
});

// ---- where a loop gives the file up --------------------------------------

const video = (route, cls) => ({ dataset: { videoRoute: route, videoClass: cls } });

test('loopGivesUp: a passthrough the old route plays', () => {
    assert.equal(loopGivesUp(video('passthrough', 'hevc10')), true);
    assert.equal(loopGivesUp(video('passthrough', 'hevc8')), true);
    // A class the page did not name: the old route is tried, as the guard's
    // own fallback does (cls 'unknown').
    assert.equal(loopGivesUp(video('passthrough', '')), true);
});

test('loopGivesUp: not over 1080, not off passthrough', () => {
    assert.equal(loopGivesUp(video('passthrough', 'hevc10-2160')), false);
    assert.equal(loopGivesUp(video('passthrough', 'hevc8-2160')), false);
    assert.equal(loopGivesUp(video('', 'hevc10')), false);
    assert.equal(loopGivesUp(null), false);
});
