import test from 'node:test';
import assert from 'node:assert/strict';
import { loadPrefs, savePrefs, stepRate, rateLabel, RATES, DEFAULTS } from './player-prefs.js';

function mem(initial) {
    const m = new Map(Object.entries(initial || {}));
    return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)), _m: m };
}

test('what was saved comes back', () => {
    const s = mem();
    assert.deepEqual(loadPrefs(s), DEFAULTS);
    assert.equal(savePrefs({ volume: 0.4 }, s), true);
    assert.equal(savePrefs({ rate: 1.5 }, s), true);
    assert.deepEqual(loadPrefs(s), { volume: 0.4, muted: false, rate: 1.5 }, 'a patch keeps the other fields');
});

test('a stored value that is not ours reads as the default, field by field', () => {
    assert.deepEqual(loadPrefs(mem({ 'wt-player-prefs': 'not json' })), DEFAULTS);
    assert.deepEqual(loadPrefs(mem({ 'wt-player-prefs': '[1,2]' })), DEFAULTS);
    const s = mem({ 'wt-player-prefs': JSON.stringify({ volume: 7, muted: 'yes', rate: 16 }) });
    assert.deepEqual(loadPrefs(s), { volume: 1, muted: false, rate: 1 }, 'clamped / ignored, never trusted');
    const ok = mem({ 'wt-player-prefs': JSON.stringify({ volume: 0.25, muted: true, rate: 3 }) });
    assert.deepEqual(loadPrefs(ok), { volume: 0.25, muted: true, rate: 1 }, 'one bad field does not drop the good ones');
});

test('no storage, or a storage that throws, never breaks the player', () => {
    assert.deepEqual(loadPrefs(null), DEFAULTS);
    assert.equal(savePrefs({ volume: 0.5 }, null), false);
    const hostile = { getItem() { throw new Error('denied'); }, setItem() { throw new Error('quota'); } };
    assert.deepEqual(loadPrefs(hostile), DEFAULTS);
    assert.equal(savePrefs({ volume: 0.5 }, hostile), false);
});

test('speed steps along the scale and stops at the ends', () => {
    assert.equal(stepRate(1, +1), 1.25);
    assert.equal(stepRate(1, -1), 0.75);
    assert.equal(stepRate(2, +1), 2);
    assert.equal(stepRate(0.5, -1), 0.5);
    assert.equal(stepRate(1.1, +1), 1.25, 'off-scale snaps to the nearest notch first');
    assert.equal(rateLabel(1.25), '1.25×');
    assert.equal(RATES.includes(1), true);
});
