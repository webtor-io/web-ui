import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { creditsStart, parseVttTimings, cuesOfLoadedTracks, timingSourceURL, creditsFromElement } from './credits.js';

// A 45-minute episode with a line every ~10 s up to `until`.
function transcript(until, from = 5) {
    const cues = [];
    for (let t = from; t + 3 <= until; t += 10) cues.push({ start: t, end: t + 3 });
    return cues;
}
const D = 2700;

test('dialogue ends, credits begin: three seconds after the last line', () => {
    const cues = transcript(2550);
    const lastEnd = cues.at(-1).end;
    assert.equal(creditsStart(cues, D), lastEnd + 3);
});

test("a translator's signature inside the credits is not dialogue", () => {
    const cues = transcript(2550);
    const lastEnd = cues.at(-1).end;
    cues.push({ start: 2640, end: 2645 }); // "Subtitles by ..." after 90 s of silence
    assert.equal(creditsStart(cues, D), lastEnd + 3);
});

test('a scene after the credits is dialogue: the guess lands at the end and is dropped', () => {
    const cues = transcript(2550);
    for (let t = 2660; t < 2690; t += 8) cues.push({ start: t, end: t + 3 }); // four lines
    assert.equal(creditsStart(cues, D), null, 'the card keeps its 25-second rule, nothing is cut');
});

test('subtitles that do not fit the film say nothing', () => {
    assert.equal(creditsStart(transcript(1500), D), null, 'last line 20 minutes before the end: another cut, or truncated');
    assert.equal(creditsStart(transcript(2690), D), null, 'talking to the very end: no gain over the 25-second card');
    assert.equal(creditsStart(transcript(3000), D), null, 'lines past the end of the file');
    assert.equal(creditsStart(transcript(2550).slice(0, 10), D), null, 'ten cues are not a transcript');
    assert.equal(creditsStart(transcript(2550), 0), null, 'unknown duration');
    assert.equal(creditsStart(null, D), null);
});

test('cue order does not matter', () => {
    const cues = transcript(2550);
    const want = creditsStart(cues, D);
    assert.equal(creditsStart([...cues].reverse(), D), want);
});

test('timing lines of VTT and SRT, and nothing else', () => {
    const text = [
        'WEBVTT', '', 'NOTE 00:00:01.000 --> not a cue', '',
        '1', '00:00:05.000 --> 00:00:08.500 align:middle', 'Hello --> world', '',
        '00:01:02,250 --> 00:01:04,000', 'SRT comma', '',
        '01:02.000 --> 01:03.000', 'short form', '',
        '00:00:09.000 --> 00:00:08.000', 'backwards: dropped',
    ].join('\n');
    assert.deepEqual(parseVttTimings(text), [
        { start: 5, end: 8.5 }, { start: 62.25, end: 64 }, { start: 62, end: 63 },
    ]);
    assert.deepEqual(parseVttTimings(undefined), []);
});

test('loaded cues are read in film time, from the fullest track', () => {
    const track = (cues) => ({ track: { cues } });
    const video = { querySelectorAll: () => [
        track([{ startTime: 1, endTime: 2 }]),
        // Shifted onto a session timeline by cue-offset.js: the authored times are stashed.
        track([{ startTime: 0, endTime: 3, __absStart: 1800, __absEnd: 1803 }, { startTime: 10, endTime: 12, __absStart: 1810, __absEnd: 1812 }]),
        track(null),
    ] };
    assert.deepEqual(cuesOfLoadedTracks(video), [{ start: 1800, end: 1803 }, { start: 1810, end: 1812 }]);
    assert.deepEqual(cuesOfLoadedTracks(null), []);
});

test('the timing source is a whole-file track, never a translation or a muxed one', () => {
    const d = new JSDOM(`<div id="m">
        <button class="subtitle" data-id="none"></button>
        <button class="subtitle" data-provider="MediaProbe" data-src="hls-managed"></button>
        <button class="subtitle" data-provider="Translated" data-src="https://t/ai.vtt"></button>
        <button class="subtitle" data-provider="OpenSubtitles" data-locked="true" data-src="https://t/locked.vtt"></button>
        <button class="subtitle" data-provider="OpenSubtitles" data-src="https://t/os.vtt"></button>
    </div>`).window.document;
    assert.equal(timingSourceURL(d.getElementById('m')), 'https://t/os.vtt');
    assert.equal(timingSourceURL(null), '');
});

test("the container's chapters answer first, inside the same bounds", () => {
    const el = (v) => ({ dataset: v === undefined ? {} : { creditsAt: String(v) } });
    assert.equal(creditsFromElement(el(2467.5), 2700), 2467.5);
    assert.equal(creditsFromElement(el(undefined), 2700), null, 'no chapters, or an older cached probe');
    assert.equal(creditsFromElement(el(1000), 2700), null, 'too early to be credits');
    assert.equal(creditsFromElement(el(2690), 2700), null, 'nothing gained');
    assert.equal(creditsFromElement(el(2467.5), 0), null, 'unknown duration');
    assert.equal(creditsFromElement(null, 2700), null);
});
