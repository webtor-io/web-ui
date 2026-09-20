import test from 'node:test';
import assert from 'node:assert/strict';
import { loadPrefs, savePrefs, stepRate, rateLabel, RATES, DEFAULTS, loadSubtitleDelay, saveSubtitleDelay } from './player-prefs.js';

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

test('the subtitle delay is remembered per file, zero is forgotten, and the map stays small', () => {
    const s = mem();
    assert.equal(loadSubtitleDelay('res:a.mkv', s), 0);
    saveSubtitleDelay('res:a.mkv', 1.5, s);
    saveSubtitleDelay('res:b.mkv', -0.75, s);
    assert.equal(loadSubtitleDelay('res:a.mkv', s), 1.5);
    assert.equal(loadSubtitleDelay('res:b.mkv', s), -0.75);
    saveSubtitleDelay('res:a.mkv', 0, s);
    assert.equal(loadSubtitleDelay('res:a.mkv', s), 0);
    assert.equal(JSON.parse(s.getItem('wt-sub-delay'))['res:a.mkv'], undefined, 'a reset leaves no entry behind');
    for (let i = 0; i < 60; i++) saveSubtitleDelay('f' + i, 1, s);
    const kept = Object.keys(JSON.parse(s.getItem('wt-sub-delay')));
    assert.equal(kept.length, 50);
    assert.equal(kept.includes('res:b.mkv'), false, 'the oldest go first');
    assert.equal(kept.includes('f59'), true);
    assert.equal(loadSubtitleDelay('x', null), 0);
    assert.equal(loadSubtitleDelay('res:a.mkv', mem({ 'wt-sub-delay': '"junk"' })), 0);
});
