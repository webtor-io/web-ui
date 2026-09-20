import test from 'node:test';
import assert from 'node:assert/strict';
import { createTapSeek, zoneOf } from './tap-seek.js';

// Manual timers: the test decides when the window has passed.
function harness() {
    const log = [];
    let timers = [];
    const ts = createTapSeek({
        onSingle: () => log.push('single'),
        onSeek: (dir, streak) => log.push(`seek${dir > 0 ? '+' : '-'}x${streak}`),
        windowMs: 300,
        setTimer: (fn) => { const t = { fn }; timers.push(t); return t; },
        clearTimer: (t) => { timers = timers.filter((x) => x !== t); },
    });
    return { ts, log, fire: () => { const due = timers; timers = []; due.forEach((t) => t.fn()); }, pending: () => timers.length };
}

test('zones: left half back, right half forward', () => {
    assert.equal(zoneOf(10, 400), 'left');
    assert.equal(zoneOf(190, 400), 'left', 'just left of the middle is still a side');
    assert.equal(zoneOf(210, 400), 'right');
    assert.equal(zoneOf(390, 400), 'right');
    assert.equal(zoneOf(10, 0), 'centre', 'an unmeasured player never seeks');
});

test('a double tap near the middle seeks -- that is where a double tap lands', () => {
    const h = harness();
    h.ts.tap(215, 400, 1000);
    assert.equal(h.ts.tap(220, 400, 1200), 'seek');
    h.fire();
    assert.deepEqual(h.log, ['seek+x1'], 'no pause-then-play');
});

test('a tap on an unmeasured player toggles at once', () => {
    const h = harness();
    assert.equal(h.ts.tap(200, 0, 1000), 'single');
    assert.deepEqual(h.log, ['single']);
    assert.equal(h.pending(), 0);
});

test('a lone side tap toggles, but only after the window', () => {
    const h = harness();
    assert.equal(h.ts.tap(390, 400, 1000), 'wait');
    assert.deepEqual(h.log, [], 'nothing yet: it may be the first of two');
    h.fire();
    assert.deepEqual(h.log, ['single']);
});

test('two quick taps on a side seek and never toggle; the streak goes on', () => {
    const h = harness();
    h.ts.tap(390, 400, 1000);
    assert.equal(h.ts.tap(385, 400, 1200), 'seek');
    assert.equal(h.pending(), 0, 'the waiting single is withdrawn');
    h.ts.tap(380, 400, 1400);
    h.fire();
    assert.deepEqual(h.log, ['seek+x1', 'seek+x2'], 'no play/pause stutter around the seeks');
});

test('left seeks back', () => {
    const h = harness();
    h.ts.tap(10, 400, 1000);
    h.ts.tap(12, 400, 1100);
    assert.deepEqual(h.log, ['seek-x1']);
});

test('slow taps, or taps on opposite sides, are two singles', () => {
    const h = harness();
    h.ts.tap(390, 400, 1000);
    h.fire();
    h.ts.tap(390, 400, 1500);
    h.fire();
    assert.deepEqual(h.log, ['single', 'single'], 'too slow to be a double');

    const g = harness();
    g.ts.tap(10, 400, 1000);
    g.ts.tap(390, 400, 1100);
    g.fire();
    assert.deepEqual(g.log, ['single', 'single'], 'left then right: the first was a real tap and is delivered, not dropped');
});

test('cancel withdraws a waiting single', () => {
    const h = harness();
    h.ts.tap(390, 400, 1000);
    h.ts.cancel();
    h.fire();
    assert.deepEqual(h.log, []);
});
