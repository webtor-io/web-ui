import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { readNext, readCarry, advancePlan, atEnd, resumeAt, readStreak, writeStreak, countdown, STILL_WATCHING_AFTER } from './next-item.js';

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
    // Music: no card in any state -- it plays on or it does not.
    assert.equal(atEnd({ autoplay: true, autoStreak: 99, cancelled: false, kind: 'track' }), 'go', 'an album is more than three tracks');
    assert.equal(atEnd({ autoplay: false, autoStreak: 0, cancelled: false, kind: 'track' }), 'stay');
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

test('known credits move the card and the prewarm earlier, never later', () => {
    const base = { duration: 2700, playing: true, hidden: false, prewarmed: false, kind: 'episode' };
    // Credits at 2400 (five minutes of them). Without the hint: prewarm at
    // 2430 (90% = 2430, d-300 = 2400 -> max), card at 2675.
    assert.equal(advancePlan({ ...base, currentTime: 2350 }).prewarm, false);
    assert.equal(advancePlan({ ...base, currentTime: 2350, creditsAt: 2400 }).prewarm, true, 'a minute before the card can offer it');
    assert.equal(advancePlan({ ...base, currentTime: 2330, creditsAt: 2400 }).prewarm, false);
    assert.equal(advancePlan({ ...base, currentTime: 2410, prewarmed: true }).card, false);
    assert.equal(advancePlan({ ...base, currentTime: 2410, prewarmed: true, creditsAt: 2400 }).card, true, 'the talking has stopped');
    assert.equal(advancePlan({ ...base, currentTime: 2390, prewarmed: true, creditsAt: 2400 }).card, false);
    // Never earlier than a prepared render can live.
    assert.equal(advancePlan({ ...base, currentTime: 2150, creditsAt: 2160 }).prewarm, false, 'eight minutes is the floor');
    assert.equal(advancePlan({ ...base, currentTime: 2225, creditsAt: 2160 }).prewarm, true);
    // A hint that makes no sense is no hint.
    assert.equal(advancePlan({ ...base, currentTime: 2000, prewarmed: true, creditsAt: 9999 }).card, false);
    assert.equal(advancePlan({ ...base, currentTime: 2000, prewarmed: true, creditsAt: 0 }).card, false);
    // The 25-second rule still stands on its own.
    assert.equal(advancePlan({ ...base, currentTime: 2680, prewarmed: true, creditsAt: null }).card, true);
    // Music has no credits card.
    assert.equal(advancePlan({ ...base, currentTime: 2410, kind: 'track', creditsAt: 2400 }).card, false);
});

test('the countdown runs in film time: ten seconds into the credits, or to the end', () => {
    assert.deepEqual(countdown({ currentTime: 2400, duration: 2700, creditsAt: 2400 }), { goAt: 2410, early: true, left: 10 });
    assert.equal(countdown({ currentTime: 2406.2, duration: 2700, creditsAt: 2400 }).left, 4);
    assert.equal(countdown({ currentTime: 2415, duration: 2700, creditsAt: 2400 }).left, 0, 'time to go');
    // No credits known: the number is simply what is left of the file.
    assert.deepEqual(countdown({ currentTime: 2680, duration: 2700 }), { goAt: 2700, early: false, left: 20 });
    // Credits in the last seconds never push the move past the end.
    assert.equal(countdown({ currentTime: 2695, duration: 2700, creditsAt: 2696 }).goAt, 2700);
    assert.equal(countdown({ currentTime: 100, duration: 2700, creditsAt: 9999 }).early, false, 'a hint that makes no sense is no hint');
});

test('a viewer who seeks into the credits still gets their ten seconds', () => {
    // Credits at 2400; the viewer lands at 2500 and the card comes up there.
    const cd = countdown({ currentTime: 2500, duration: 2700, creditsAt: 2400, shownAt: 2500 });
    assert.equal(cd.left, 10, 'not "next in 0 s"');
    assert.equal(cd.goAt, 2510);
    // Arriving with the credits, as usual: unchanged.
    assert.equal(countdown({ currentTime: 2400, duration: 2700, creditsAt: 2400, shownAt: 2400 }).goAt, 2410);
    // Seven seconds from the end there are only seven to give.
    assert.equal(countdown({ currentTime: 2693, duration: 2700, creditsAt: 2400, shownAt: 2693 }).left, 7);
});
