import test from 'node:test';
import assert from 'node:assert/strict';
import { settled, track } from './player-telemetry.js';

function timers() {
    let list = [];
    return {
        setTimer: (fn) => { const t = { fn }; list.push(t); return t; },
        clearTimer: (t) => { list = list.filter((x) => x !== t); },
        fire: () => { const due = list; list = []; due.forEach((t) => t.fn()); },
        count: () => list.length,
    };
}

test('six presses are one correction: only the value they settled on is sent', () => {
    const t = timers(); const sent = [];
    const s = settled((v) => sent.push(v), 2000, t);
    for (const d of [0.25, 0.5, 0.75, 1, 1.25, 1.5]) s.push(d);
    assert.equal(t.count(), 1, 'one timer, re-armed');
    assert.deepEqual(sent, []);
    t.fire();
    assert.deepEqual(sent, [1.5]);
    t.fire();
    assert.deepEqual(sent, [1.5], 'nothing twice');
});

test('flush sends what is pending at once, and only if something is', () => {
    const t = timers(); const sent = [];
    const s = settled((v) => sent.push(v), 2000, t);
    s.flush();
    assert.deepEqual(sent, []);
    s.push({ delay: -0.5 });
    s.flush();
    assert.deepEqual(sent, [{ delay: -0.5 }]);
    assert.equal(t.count(), 0, 'the timer is gone with it');
});

test('track never throws: no umami, or an umami that does', () => {
    global.window = {};
    track('x', {});
    global.window = { umami: { track() { throw new Error('blocked'); } } };
    track('x', {});
    const got = [];
    global.window = { umami: { track: (n, d) => got.push([n, d]) } };
    track('player-speed', { rate: 1.5 });
    assert.deepEqual(got, [['player-speed', { rate: 1.5 }]]);
    delete global.window;
});
