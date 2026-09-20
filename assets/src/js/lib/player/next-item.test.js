import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { readNext, readCarry, advancePlan, atEnd, resumeAt, readStreak, writeStreak, STILL_WATCHING_AFTER } from './next-item.js';

const dom = (html) => new JSDOM(`<!doctype html><body>${html}</body>`).window.document;

test('readNext: both the id and the path, or nothing', () => {
    const d = dom('<video id="a" data-next-item-id="i2" data-next-path="S01/e02.mkv" data-next-kind="episode" data-next-label="S01E02 · Two"></video><video id="b" data-next-item-id="i2"></video><video id="c"></video>');
    assert.deepEqual(readNext(d.getElementById('a')), { itemId: 'i2', path: 'S01/e02.mkv', kind: 'episode', label: 'S01E02 · Two' });
    assert.equal(readNext(d.getElementById('b')), null, 'half a description starts nothing');
    assert.equal(readNext(d.getElementById('c')), null);
    assert.equal(readNext(null), null);
});

test('readCarry reads what is playing now, as intent', () => {
    const d = dom(`
        <button class="audio" data-id="mp-0" data-srclang="ru" data-label="Dub"></button>
        <button class="audio" data-default="true" data-id="mp-1" data-srclang="en" data-label="Original (5.1)"></button>
        <button class="subtitle" data-id="none"></button>
        <button class="subtitle" data-default="true" data-id="os-7" data-srclang="ru" data-provider="OpenSubtitles"></button>`);
    assert.deepEqual(readCarry(d), {
        'carry-audio-lang': 'en', 'carry-audio-label': 'Original (5.1)',
        'carry-sub': 'on', 'carry-sub-lang': 'ru', 'carry-sub-provider': 'OpenSubtitles',
    });
});

test('readCarry: subtitles off is an intent too; a track with no language is not', () => {
    const off = dom('<button class="subtitle" data-default="true" data-id="none" data-srclang=""></button>');
    assert.deepEqual(readCarry(off), { 'carry-sub': 'off' });
    const und = dom('<button class="audio" data-default="true" data-id="mp-0" data-srclang=""></button><button class="subtitle" data-default="true" data-id="u1" data-srclang=""></button>');
    assert.deepEqual(readCarry(und), {}, 'nothing to match in the next file');
    assert.deepEqual(readCarry(null), {});
});

test('prewarm at 90%, but not more than five minutes early, and only while watched', () => {
    const base = { duration: 1200, playing: true, hidden: false, prewarmed: false, kind: 'episode' };
    assert.equal(advancePlan({ ...base, currentTime: 1000 }).prewarm, false, '83%');
    assert.equal(advancePlan({ ...base, currentTime: 1085 }).prewarm, true, '90%');
    assert.equal(advancePlan({ ...base, currentTime: 1085, playing: false }).prewarm, false, 'paused');
    assert.equal(advancePlan({ ...base, currentTime: 1085, hidden: true }).prewarm, false, 'tab in the background');
    assert.equal(advancePlan({ ...base, currentTime: 1085, prewarmed: true }).prewarm, false, 'once');
    // A two-hour file: 90% is twelve minutes before the end -- longer than a
    // prepared render lives. Five minutes it is.
    const long = { ...base, duration: 7200 };
    assert.equal(advancePlan({ ...long, currentTime: 6600 }).prewarm, false, '91%, but ten minutes to go');
    assert.equal(advancePlan({ ...long, currentTime: 6905 }).prewarm, true);
    assert.equal(advancePlan({ ...base, duration: 0, currentTime: 5 }).prewarm, false, 'unknown duration');
});

test('the card is for video, in the last 25 seconds', () => {
    const base = { duration: 1200, playing: true, hidden: false, prewarmed: true, kind: 'episode' };
    assert.equal(advancePlan({ ...base, currentTime: 1100 }).card, false);
    assert.equal(advancePlan({ ...base, currentTime: 1180 }).card, true);
    assert.equal(advancePlan({ ...base, currentTime: 1180, kind: 'track' }).card, false, 'music just plays on');
    assert.equal(advancePlan({ ...base, duration: 40, currentTime: 30 }).card, false, 'a clip shorter than the card itself');
});

test('what `ended` means', () => {
    assert.equal(atEnd({ autoplay: true, autoStreak: 0, cancelled: false }), 'go');
    assert.equal(atEnd({ autoplay: true, autoStreak: 0, cancelled: true }), 'stay');
    assert.equal(atEnd({ autoplay: false, autoStreak: 0, cancelled: false }), 'offer');
    assert.equal(atEnd({ autoplay: true, autoStreak: STILL_WATCHING_AFTER, cancelled: false }), 'ask', 'a sleeper does not transcode a season');
});

test('an automatic transition resumes quietly, or from the top if it was finished', () => {
    assert.equal(resumeAt(600, 2400), 600);
    assert.equal(resumeAt(2300, 2400), 0);
    assert.equal(resumeAt(0, 2400), 0);
    assert.equal(resumeAt(600, 0), 600, 'an unknown duration cannot say finished');
});

test('the streak survives a transition and a storage that throws', () => {
    const m = new Map();
    const s = { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, v) };
    assert.equal(readStreak(s), 0);
    writeStreak(s, 2);
    assert.equal(readStreak(s), 2);
    m.set('wt-next-auto-streak', 'junk');
    assert.equal(readStreak(s), 0);
    const bad = { getItem() { throw new Error('x'); }, setItem() { throw new Error('x'); } };
    assert.equal(readStreak(bad), 0);
    writeStreak(bad, 1);
});
