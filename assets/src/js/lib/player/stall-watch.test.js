import test from 'node:test';
import assert from 'node:assert/strict';
import { createStallWatch } from './stall-watch.js';

test('a clock that moves is never a stall', () => {
    const w = createStallWatch({ stallMs: 300 });
    for (let i = 0; i < 100; i++) assert.equal(w.sample(i * 0.016, i * 16), null);
    assert.equal(w.isStalled(), false);
});

test('a clock that stands still, film not paused, is a stall -- reported once, and once when it ends', () => {
    const w = createStallWatch({ stallMs: 300 });
    w.sample(10, 0);
    // The seek: currentTime jumps once, then stands.
    assert.equal(w.sample(20, 16), null);
    assert.equal(w.sample(20, 200), null, 'not yet');
    assert.equal(w.sample(20, 320), 'stalled');
    assert.equal(w.sample(20, 2000), null, 'one report per stall, not one per frame');
    assert.equal(w.isStalled(), true, 'still stalled whatever canplay says meanwhile');
    // No frame counter: the clock has to keep advancing for recoverMs.
    assert.equal(w.sample(20.03, 2016), null, 'one changed sample is not playback');
    let verdict = null;
    for (let i = 2; i <= 20 && !verdict; i++) verdict = w.sample(20 + i * 0.016, 2000 + i * 16);
    assert.equal(verdict, 'moving');
    assert.equal(w.isStalled(), false);
});

test('a nudge in a buffer hole does not end the stall', () => {
    // hls.js pushes currentTime forward by a fraction of a second while it
    // waits for data; the picture stays frozen. With a frame counter:
    const w = createStallWatch({ stallMs: 300 });
    w.sample(20, 0, { frames: 500 });
    assert.equal(w.sample(20, 320, { frames: 500 }), 'stalled');
    assert.equal(w.sample(20.1, 1000, { frames: 500 }), null, 'nudge: the number changed, no frame was shown');
    assert.equal(w.sample(20.2, 2000, { frames: 501 }), null, 'the seek target frame is painted, then it stands');
    assert.equal(w.isStalled(), true);
    assert.equal(w.sample(20.3, 2100, { frames: 502 }), null);
    assert.equal(w.sample(20.35, 2140, { frames: 503 }), 'moving', 'three frames since the stall: that is playback');

    // Without one (audio): a nudge is a single jump, not a run.
    const a = createStallWatch({ stallMs: 300, recoverMs: 250 });
    a.sample(20, 0);
    assert.equal(a.sample(20, 320), 'stalled');
    assert.equal(a.sample(20.1, 1000), null);
    assert.equal(a.sample(20.1, 1016), null, 'the run is broken at once');
    assert.equal(a.sample(20.2, 2000), null);
    assert.equal(a.sample(20.2, 2300), null, 'nudges far apart never add up to a run');
    assert.equal(a.isStalled(), true);
});

test('a pause is not a stall, and its length does not count afterwards', () => {
    const w = createStallWatch({ stallMs: 300 });
    w.sample(5, 0);
    assert.equal(w.sample(5, 5000, { idle: true }), null, 'paused for five seconds');
    assert.equal(w.sample(5, 5016), null, 'resumed: the 300 ms start now');
    assert.equal(w.sample(5, 5200), null);
    assert.equal(w.sample(5, 5400), 'stalled', 'a real stall after the resume is still caught');
});

test('pausing during a stall ends it quietly', () => {
    const w = createStallWatch({ stallMs: 300 });
    w.sample(5, 0);
    assert.equal(w.sample(5, 400), 'stalled');
    assert.equal(w.sample(5, 500, { idle: true }), null, 'no "moving": nothing moved');
    assert.equal(w.isStalled(), false);
});
