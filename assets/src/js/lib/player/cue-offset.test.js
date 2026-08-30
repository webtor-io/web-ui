import { test } from 'node:test';
import assert from 'node:assert/strict';
import { applyCueOffset, captureTrackState, restoreTrackState } from './cue-offset.js';

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
    // Fully before the session: parked at [0,0], never active.
    assert.equal(track.cues[0].startTime, 0);
    assert.equal(track.cues[0].endTime, 0);
    // Straddling the session start: clipped to begin at 0.
    assert.equal(track.cues[1].startTime, 0);
    assert.equal(track.cues[1].endTime, 20);
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
