import { test } from 'node:test';
import assert from 'node:assert/strict';
import { reloadSubtitleTrack } from './subtitle-track-reload.js';

// A <track> element stand-in with faithful add/removeEventListener
// semantics — the whole point of the test is which listeners are attached
// at any moment, so the fake must not be cleverer than the DOM.
function makeTrack(id, cues = []) {
    const listeners = { load: [], error: [] };
    const attrs = { src: '' };
    return {
        id,
        listeners,
        track: {
            mode: 'showing',
            cues,
            addCue(c) { this.cues.push(c); },
        },
        getAttribute: (n) => (n in attrs ? attrs[n] : null),
        setAttribute: (n, v) => { attrs[n] = v; },
        addEventListener(type, fn) { listeners[type].push(fn); },
        removeEventListener(type, fn) {
            const i = listeners[type].indexOf(fn);
            if (i >= 0) listeners[type].splice(i, 1);
        },
        fire(type) { for (const fn of [...listeners[type]]) fn(); },
    };
}

const videoWith = (...tracks) => ({ querySelectorAll: () => tracks });

test('a reload swaps src and reports that it happened', () => {
    const el = makeTrack('tr-pt');
    const video = videoWith(el);
    assert.equal(reloadSubtitleTrack(video, 'tr-pt', 'https://x/a.vtt?rev=1'), true);
    assert.equal(el.getAttribute('src'), 'https://x/a.vtt?rev=1');
});

test('nothing to do reports false so the caller does not stamp a throttle it never spent', () => {
    const el = makeTrack('tr-pt');
    const video = videoWith(el);
    reloadSubtitleTrack(video, 'tr-pt', 'https://x/a.vtt?rev=1');
    // Same revision again: no swap, no listeners, no reload.
    assert.equal(reloadSubtitleTrack(video, 'tr-pt', 'https://x/a.vtt?rev=1'), false);
    assert.equal(reloadSubtitleTrack(video, 'missing', 'https://x/a.vtt?rev=2'), false);
    assert.equal(reloadSubtitleTrack(null, 'tr-pt', 'https://x/a.vtt?rev=2'), false);
    assert.equal(reloadSubtitleTrack(video, 'tr-pt', ''), false);
});

test('one listener pair per track, however many revisions arrive before one settles', () => {
    // A translation reloads every 15 s for minutes. With {once:true} pairs
    // added per revision and nothing removing them, the handlers pile up on
    // the element for the life of the page.
    const el = makeTrack('tr-pt');
    const video = videoWith(el);
    for (let rev = 1; rev <= 5; rev++) {
        reloadSubtitleTrack(video, 'tr-pt', `https://x/a.vtt?rev=${rev}`);
    }
    assert.equal(el.listeners.load.length, 1);
    assert.equal(el.listeners.error.length, 1);
});

test('a settled reload leaves no listeners behind', () => {
    const el = makeTrack('tr-pt');
    const video = videoWith(el);
    reloadSubtitleTrack(video, 'tr-pt', 'https://x/a.vtt?rev=1');
    el.fire('load');
    assert.equal(el.listeners.load.length, 0);
    assert.equal(el.listeners.error.length, 0);
});

test('a failure restores the latest snapshot, not the oldest', () => {
    // The bug this pins: with a pair of listeners left over per revision,
    // the first (oldest) error handler ran and put back the cues as they
    // were several revisions ago — the viewer's subtitles jumped backwards.
    const cueA = { id: 'A' };
    const cueB = { id: 'B' };
    const el = makeTrack('tr-pt', [cueA]);
    const video = videoWith(el);

    reloadSubtitleTrack(video, 'tr-pt', 'https://x/a.vtt?rev=1'); // snapshot: [A]
    el.track.cues = [cueA, cueB];                                 // rev 1 loaded
    reloadSubtitleTrack(video, 'tr-pt', 'https://x/a.vtt?rev=2'); // snapshot: [A, B]
    el.track.cues = [];                                           // rev 2 failed to parse
    el.fire('error');

    assert.deepEqual(el.track.cues, [cueA, cueB]);
});

test('the error is reported to the caller once the cues are back', () => {
    const el = makeTrack('tr-pt', [{ id: 'A' }]);
    const video = videoWith(el);
    let reported = 0;
    reloadSubtitleTrack(video, 'tr-pt', 'https://x/a.vtt?rev=1', () => { reported++; });
    el.track.cues = [];
    el.fire('error');
    assert.equal(reported, 1);
    // And the settled run is gone: a second error on the same element
    // (from a later revision that was never started) reports nothing.
    el.fire('error');
    assert.equal(reported, 1);
});
