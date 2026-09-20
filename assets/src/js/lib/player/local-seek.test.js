import test from 'node:test';
import assert from 'node:assert/strict';
import { localSeekTarget, producedEnd, EDGE_S } from './local-seek.js';

test('inside the run: a position, not a restart', () => {
    // The run started at 30:00 of the film and ten minutes of it exist.
    assert.equal(localSeekTarget(1815, 1800, 600), 15, '+15 s by arrow key');
    assert.equal(localSeekTarget(1800, 1800, 600), 0, 'back to the very start of the run');
    assert.equal(localSeekTarget(2300, 1800, 600), 500);
});

test('outside the run: the transcoder has to be asked', () => {
    assert.equal(localSeekTarget(1790, 1800, 600), null, 'before the run began');
    assert.equal(localSeekTarget(2500, 1800, 600), null, 'past what FFmpeg has written');
    assert.equal(localSeekTarget(1800 + 600 - EDGE_S + 1, 1800, 600), null, 'the edge of the playlist is no place to aim for');
    assert.equal(localSeekTarget(1800 + 600 - EDGE_S, 1800, 600), 600 - EDGE_S);
});

test('nothing known, nothing local', () => {
    assert.equal(localSeekTarget(10, 0, 0), null, 'the playlist has not been read yet');
    assert.equal(localSeekTarget(NaN, 0, 600), null);
    assert.equal(localSeekTarget(10, undefined, 600), null);
});

test('how far the run has been written: the playlist first, the element second', () => {
    const hls = { currentLevel: 0, levels: [{ details: { totalduration: 412.5 } }] };
    assert.equal(producedEnd({}, hls), 412.5);
    assert.equal(producedEnd({}, { currentLevel: -1, levels: [{ details: { totalduration: 90 } }] }), 90, 'auto level: the first');
    const video = { seekable: { length: 1, end: () => 300 } };
    assert.equal(producedEnd(video, null), 300, 'native HLS');
    assert.equal(producedEnd(video, { levels: [] }), 300);
    assert.equal(producedEnd({ seekable: { length: 0 } }, null), 0);
    assert.equal(producedEnd(null, null), 0);
});
