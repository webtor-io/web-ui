import { test } from 'node:test';
import assert from 'node:assert/strict';
import { applyCueOffset, captureTrackState, restoreTrackState, setTrackDelay, normalizeDelay } from './cue-offset.js';

function makeTrack(ranges, mode = 'showing') {
    return {
        mode,
        cues: ranges.map(([s, e]) => ({ startTime: s, endTime: e })),
        addCue(cue) { this.cues.push(cue); },
    };
}

test('applyCueOffset shifts cues onto the session-relative timeline', () => {
    const track = makeTrack([[600, 605], [630, 640]]);
    applyCueOffset(track, 600);
    assert.equal(track.cues[0].startTime, 0);
    assert.equal(track.cues[0].endTime, 5);
    assert.equal(track.cues[1].startTime, 30);
    assert.equal(track.cues[1].endTime, 40);
});

test('applyCueOffset is idempotent and re-applies from authored times', () => {
    const track = makeTrack([[600, 605]]);
    applyCueOffset(track, 600);
    applyCueOffset(track, 600);
    assert.equal(track.cues[0].startTime, 0);
    assert.equal(track.cues[0].endTime, 5);
    // A later seek back to the start restores authored times.
    applyCueOffset(track, 0);
    assert.equal(track.cues[0].startTime, 600);
    assert.equal(track.cues[0].endTime, 605);
});

test('applyCueOffset neutralises cues that end before the session start', () => {
    const track = makeTrack([[10, 20], [590, 620]]);
    applyCueOffset(track, 600);
    // Fully before the session: parked below zero, where the playhead of a
    // session never is. Not at [0,0]: a session always starts at media time
    // 0, Chrome's cue interval test is inclusive at both ends, and a [0,0]
    // cue was drawn from the first frame until the next cue boundary —
    // every line from the start of the film up to the seek point at once
    // (reproduced on stage 2026-09-17).
    assert.ok(track.cues[0].endTime < 0, `parked end must be below 0, got ${track.cues[0].endTime}`);
    assert.ok(track.cues[0].startTime <= track.cues[0].endTime);
    // Straddling the session start: clipped to begin at 0.
    assert.equal(track.cues[1].startTime, 0);
    assert.equal(track.cues[1].endTime, 20);
});

test('a parked cue comes back when a later seek moves the session before it', () => {
    const track = makeTrack([[10, 20]]);
    applyCueOffset(track, 600);
    assert.ok(track.cues[0].endTime < 0);
    applyCueOffset(track, 0);
    assert.equal(track.cues[0].startTime, 10);
    assert.equal(track.cues[0].endTime, 20);
});

test('applyCueOffset tolerates tracks without cues', () => {
    assert.doesNotThrow(() => applyCueOffset(null, 30));
    assert.doesNotThrow(() => applyCueOffset({ cues: null }, 30));
});

test('capture/restoreTrackState round-trips modes across a reload', () => {
    const a = makeTrack([[0, 1]], 'showing');
    const b = makeTrack([[0, 1]], 'hidden');
    const saved = captureTrackState([a, b]);
    // hls.js disables element-backed tracks when the source is reloaded.
    a.mode = 'disabled';
    b.mode = 'disabled';
    restoreTrackState(saved);
    assert.equal(a.mode, 'showing');
    assert.equal(b.mode, 'hidden');
});

test('restoreTrackState re-adds cues that hls.js cleared during reload', () => {
    const a = makeTrack([[600, 605], [630, 640]], 'showing');
    const original = [...a.cues];
    const saved = captureTrackState([a]);
    // hls.js clears the cue list and disables the track on loadSource().
    a.cues = [];
    a.mode = 'disabled';
    restoreTrackState(saved);
    assert.equal(a.mode, 'showing');
    assert.deepEqual(a.cues, original);
});

test('restoreTrackState does not duplicate surviving cues', () => {
    const a = makeTrack([[600, 605]], 'showing');
    const saved = captureTrackState([a]);
    restoreTrackState(saved);
    assert.equal(a.cues.length, 1);
});

test('seek sequence: capture → hls wipe → restore → re-offset lands on new timeline', () => {
    // Track authored at absolute 600–605, already shifted for a session at 570.
    const a = makeTrack([[600, 605]], 'showing');
    applyCueOffset(a, 570);
    assert.equal(a.cues[0].startTime, 30);
    // User seeks again: capture, hls.js wipes cues and disables the track.
    const saved = captureTrackState([a]);
    a.cues = [];
    a.mode = 'disabled';
    // New session starts at 600 — restore and re-shift from authored times.
    restoreTrackState(saved);
    for (const { track } of saved) applyCueOffset(track, 600);
    assert.equal(a.mode, 'showing');
    assert.equal(a.cues[0].startTime, 0);
    assert.equal(a.cues[0].endTime, 5);
});

test('captureTrackState skips null entries and cue-less tracks', () => {
    const a = makeTrack([[0, 1]], 'showing');
    const bare = { mode: 'hidden', cues: null, addCue() { throw new Error('must not add'); } };
    const saved = captureTrackState([null, a, undefined, bare]);
    a.mode = 'disabled';
    restoreTrackState(saved);
    assert.equal(a.mode, 'showing');
    assert.equal(bare.mode, 'hidden');
});

test("the viewer's delay rides on the track and survives every later re-shift", () => {
    const track = makeTrack([[600, 605], [630, 640]]);
    setTrackDelay(track, 1.5);
    applyCueOffset(track, 600);
    assert.equal(track.cues[0].startTime, 1.5, 'later by the delay');
    assert.equal(track.cues[0].endTime, 6.5);
    // A seek re-shifts from the authored times with nobody passing the
    // delay along -- that is the point of keeping it on the track.
    applyCueOffset(track, 630);
    assert.equal(track.cues[1].startTime, 1.5);
    assert.equal(track.cues[0].startTime, -1, 'the first line is behind the new run: parked');
    // Earlier, and back to none: always from the authored times, no drift.
    setTrackDelay(track, -2);
    applyCueOffset(track, 600);
    assert.equal(track.cues[1].startTime, 28);
    setTrackDelay(track, 0);
    applyCueOffset(track, 600);
    assert.equal(track.cues[1].startTime, 30);
});

test('a delay works without a session too (offset 0)', () => {
    const track = makeTrack([[1, 3]]);
    setTrackDelay(track, -2);
    applyCueOffset(track, 0);
    assert.equal(track.cues[0].startTime, 0, 'clamped at the start of the film');
    assert.equal(track.cues[0].endTime, 1);
});

test('normalizeDelay: quarter-second steps, a sane range, nothing that is not a number', () => {
    assert.equal(normalizeDelay(0.3), 0.25);
    assert.equal(normalizeDelay(-0.4), -0.5);
    assert.equal(normalizeDelay(1e9), 60);
    assert.equal(normalizeDelay(-1e9), -60);
    assert.equal(normalizeDelay('2'), 0);
    assert.equal(normalizeDelay(NaN), 0);
});
