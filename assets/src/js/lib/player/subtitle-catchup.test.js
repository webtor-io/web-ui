import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    CATCHUP_TRAIL_MARGIN_S,
    CATCHUP_CLEAR_MARGIN_S,
    trailing,
    caughtUp,
    remaining,
} from './subtitle-catchup.js';

test('the margins are the shipped ones, and ordered', () => {
    // Under 2 s the answer would flip on the ~1.7 s a transcoder run's own
    // offset can shift by, and with the two equal there is no band left to
    // hold an answer steady.
    assert.equal(CATCHUP_TRAIL_MARGIN_S, 2);
    assert.equal(CATCHUP_CLEAR_MARGIN_S, 5);
    assert.ok(CATCHUP_CLEAR_MARGIN_S > CATCHUP_TRAIL_MARGIN_S);
});

test('no header is no banner, whatever the previous answer was', () => {
    // Absent on an older service, on a batch source, and on a live run
    // with nothing pending ahead. All three mean "say nothing".
    assert.equal(trailing(false, null, 100), false);
    assert.equal(trailing(true, null, 100), false);
    assert.equal(trailing(true, undefined, 100), false);
});

test('no header is caught up: a wait must end when the header stops coming', () => {
    assert.equal(caughtUp(null, 100), true);
    assert.equal(caughtUp(undefined, 100), true);
});

test('trailing: behind the playhead, and exactly at the margin', () => {
    // The frontier is where the untranslated part starts, so a frontier at
    // or before the playhead means the viewer is already past it.
    assert.equal(trailing(false, 90, 100), true);
    assert.equal(trailing(false, 100, 100), true);
    // The margin itself counts as trailing: the viewer reaches it in 2 s.
    assert.equal(trailing(false, 102, 100), true);
    // A frontier comfortably ahead is not.
    assert.equal(trailing(false, 105, 100), false);
    assert.equal(trailing(false, 400, 100), false);
});

test('caughtUp: the clear margin is what resumes, and it is inclusive', () => {
    assert.equal(caughtUp(105, 100), true);
    assert.equal(caughtUp(104.9, 100), false);
    assert.equal(caughtUp(100, 100), false);
    assert.equal(caughtUp(0, 100), false);
});

test('the band between the margins keeps the previous answer, both ways', () => {
    // 100 + 2 < 103 < 100 + 5: neither answer is wrong here, so the one
    // already on screen wins. Without this a healthy live run — which
    // hovers a few seconds ahead of the playhead — would show and hide the
    // banner every three seconds.
    assert.equal(trailing(true, 103, 100), true, 'a shown banner stays shown');
    assert.equal(trailing(false, 103, 100), false, 'a hidden one stays hidden');
    // And at both edges of the band the thresholds still decide.
    assert.equal(trailing(true, 105, 100), false, 'the clear margin ends it');
    assert.equal(trailing(false, 102, 100), true, 'the trail margin starts it');
});

test('a video with no time yet never pauses itself', () => {
    // currentTime on an element with no timeline is NaN, and NaN
    // comparisons are all false — read as a position it would say "the
    // translation is ahead" or "behind" at random. A film that has not
    // started must never be paused by this feature, and a viewer waiting
    // on one must never be stuck.
    assert.equal(trailing(false, 90, NaN), false);
    assert.equal(trailing(true, 90, NaN), false);
    assert.equal(caughtUp(90, NaN), true);
    // Same for a playhead that is not a number at all.
    assert.equal(trailing(true, 90, undefined), false);
    assert.equal(caughtUp(90, undefined), true);
    assert.equal(trailing(true, 90, Infinity), false);
    assert.equal(caughtUp(90, Infinity), true);
});

test('a zero frontier is a position, not a missing answer', () => {
    // The earliest untranslated cue starting at the top of the film is a
    // real claim, and a viewer anywhere past the opening is behind it.
    assert.equal(trailing(false, 0, 100), true);
    assert.equal(caughtUp(0, 100), false);
    // And at the very start of the film it is the ordinary comparison.
    assert.equal(trailing(false, 0, 0), true);
    assert.equal(caughtUp(6, 0), true);
});

test('remaining is the gap, clamped, and 0 for nothing', () => {
    assert.equal(remaining({ done: 12, total: 400 }), 388);
    assert.equal(remaining({ done: 400, total: 400 }), 0);
    // `total` is a snapshot on a live source and can lag `done`.
    assert.equal(remaining({ done: 410, total: 400 }), 0);
    assert.equal(remaining(null), 0);
    assert.equal(remaining(undefined), 0);
    assert.equal(remaining({}), 0);
});
