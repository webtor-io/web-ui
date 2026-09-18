import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    CATCHUP_TRAIL_MARGIN_S,
    CATCHUP_CLEAR_MARGIN_S,
    trailing,
    caughtUp,
    resumeRewind,
    shouldBrake,
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

test('nothing counted yet is 0/0 on a run that is not final, and nothing else', async () => {
    const { nothingCountedYet } = await import('./subtitle-catchup.js');
    assert.equal(nothingCountedYet({ done: 0, total: 0, final: false }), true);
    assert.equal(nothingCountedYet({ done: 0, total: 400, final: false }), false, 'counted, none done: the frontier speaks');
    assert.equal(nothingCountedYet({ done: 12, total: 400, final: false }), false);
    assert.equal(nothingCountedYet({ done: 0, total: 0, final: true }), false);
    assert.equal(nothingCountedYet(null), false);
});

test('the viewer’s frontier is the service’s while the track has everything the service has', async () => {
    const { viewerFrontier } = await import('./subtitle-catchup.js');
    assert.equal(viewerFrontier({ serviceFrontier: 400, serviceDone: 40, loaded: { done: 40, frontier: 120 } }), 400);
    assert.equal(viewerFrontier({ serviceFrontier: null, serviceDone: 40, loaded: { done: 40, frontier: 120 } }), null);
    assert.equal(viewerFrontier({ serviceFrontier: 400, serviceDone: 40, loaded: null }), 400, 'before the first swap there is nothing to compare');
});

test('once the service is ahead of the track, the track’s edge is the frontier', async () => {
    const { viewerFrontier } = await import('./subtitle-catchup.js');
    // Swapped in while the service was pending from 120; it has since moved to 400.
    assert.equal(viewerFrontier({ serviceFrontier: 400, serviceDone: 80, loaded: { done: 40, frontier: 120 } }), 120);
    // The service says nothing is pending at all -- the viewer still only has up to 120.
    assert.equal(viewerFrontier({ serviceFrontier: null, serviceDone: 80, loaded: { done: 40, frontier: 120 } }), 120);
    // Swapped in with nothing pending: the edge is where the loaded cues end.
    assert.equal(viewerFrontier({ serviceFrontier: 400, serviceDone: 80, loaded: { done: 40, frontier: null }, coverageEnd: 150 }), 150);
    // ...and when that cannot be read, the service's word is all there is.
    assert.equal(viewerFrontier({ serviceFrontier: 400, serviceDone: 80, loaded: { done: 40, frontier: null }, coverageEnd: null }), 400);
    // The service can also be the nearer of the two.
    assert.equal(viewerFrontier({ serviceFrontier: 100, serviceDone: 80, loaded: { done: 40, frontier: 120 } }), 100);
});

test('a reload is needed when the viewer nears the edge of what is loaded and there is more', async () => {
    const { needsReload } = await import('./subtitle-catchup.js');
    const loaded = { done: 40, frontier: 120 };
    assert.equal(needsReload({ serviceDone: 80, loaded, frontier: 120, playhead: 116 }), true, 'within 5 s of the edge');
    assert.equal(needsReload({ serviceDone: 80, loaded, frontier: 120, playhead: 100 }), false, 'plenty loaded ahead');
    assert.equal(needsReload({ serviceDone: 40, loaded, frontier: 120, playhead: 119 }), false, 'nothing new to fetch');
    assert.equal(needsReload({ serviceDone: 80, loaded: null, frontier: 120, playhead: 119 }), false);
    assert.equal(needsReload({ serviceDone: 80, loaded, frontier: null, playhead: 119 }), false);
});

test('shouldBrake: a second before the first untranslated line, never without a frontier', () => {
    assert.equal(shouldBrake(100, 98.9), false);
    assert.equal(shouldBrake(100, 99), true);
    assert.equal(shouldBrake(100, 130), true, 'already past it');
    assert.equal(shouldBrake(null, 99.5), false, 'no frontier, nothing to stop for');
    assert.equal(shouldBrake(undefined, 99.5), false);
    assert.equal(shouldBrake(100, NaN), false, 'a NaN playhead never pauses the film');
});

test('resumeRewind: back to the missed line plus a lead-in, nothing when nothing was missed', () => {
    assert.equal(resumeRewind(100, 99), 0, 'stopped before the line: no replay');
    assert.equal(resumeRewind(100, 100), 0);
    assert.equal(resumeRewind(100, 101.5), 3.5, 'missed 1.5 s + 2 s lead-in');
    assert.equal(resumeRewind(100, 140), 10, 'capped');
    assert.equal(resumeRewind(null, 140), 0);
    assert.equal(resumeRewind(100, NaN), 0);
});
