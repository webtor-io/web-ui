import { test } from 'node:test';
import assert from 'node:assert/strict';
import { reloadSubtitleTrack, dropDeletedTracks, markUnsnapshottedTracksStale, refreshStaleTrack } from './subtitle-track-reload.js';

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

// A <video> whose <track> children can actually be removed — dropDeletedTracks
// is about the elements disappearing, so the fake has to let them.
function videoTree(...tracks) {
    const kids = tracks.slice();
    const video = { querySelectorAll: () => kids.slice() };
    for (const t of kids) t.remove = () => { const i = kids.indexOf(t); if (i >= 0) kids.splice(i, 1); };
    video.ids = () => kids.map((t) => t.id);
    return video;
}

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

// Deleting an upload swaps #my-subtitles, which takes the chip away; the
// <track> lives in <video> and survives, so the deleted file's subtitles
// kept showing until a reload — with no chip marked and a blank "Now:".
test('a deleted upload takes its <track> with it and reports that it was showing', () => {
    const gone = makeTrack('us-deleted');
    const kept = makeTrack('os-1');
    kept.track.mode = 'disabled';
    const video = videoTree(gone, kept);

    assert.equal(dropDeletedTracks(video, ['none', 'os-1', 'mp-0']), 'us-deleted');
    assert.deepEqual(video.ids(), ['os-1']);
});

// The orphan test is against EVERY chip in the dialog. Measured against the
// uploads alone, the preloaded OpenSubtitles and sidecar tracks look like
// orphans too — and the viewer loses the track they are actually watching.
test('tracks whose chips are still there are left alone', () => {
    const os = makeTrack('os-1');
    const em = makeTrack('us-kept');
    const video = videoTree(os, em);

    assert.equal(dropDeletedTracks(video, ['none', 'os-1', 'us-kept']), '');
    assert.deepEqual(video.ids(), ['os-1', 'us-kept']);
});

// A deleted upload that was not the one playing goes just as quietly, and
// the caller is told nothing happened to playback.
test('a deleted upload that was not playing reports no showing track', () => {
    const gone = makeTrack('us-deleted');
    gone.track.mode = 'disabled';
    const video = videoTree(gone, makeTrack('os-1'));

    assert.equal(dropDeletedTracks(video, ['os-1']), '');
    assert.deepEqual(video.ids(), ['os-1']);
});

// ---- tracks a session seek emptied ------------------------------------
//
// hls.js's loadSource clears the cues of EVERY text track on the element
// (TimelineController._cleanTracks), ours included. The seeker snapshots
// cues first, but a disabled track reports `cues === null`, so only the
// track that was on screen gets its cues back. Every other <track> is left
// loaded (readyState 2), empty, and never refetched: the browser does not
// reload a src it already loaded. Picking it after the seek showed nothing
// (reproduced on stage 2026-09-17: an OpenSubtitles track at readyState 2,
// mode showing, 0 cues; 353 after a forced reload).

const LOADED = 2;

function loadedTrack(id, { cues = [], readyState = LOADED, src = `https://x/${id}.vtt?token=t` } = {}) {
    const el = makeTrack(id, cues);
    el.readyState = readyState;
    el.setAttribute('src', src);
    return el;
}

test('a track the seek snapshot did not cover is marked; the one it restores is not', () => {
    const playing = loadedTrack('os-ru', { cues: [{ startTime: 1, endTime: 2 }] });
    const other = loadedTrack('os-en');
    const saved = [{ track: playing.track, mode: 'showing', cues: [...playing.track.cues] }, { track: other.track, mode: 'disabled', cues: [] }];
    const marked = markUnsnapshottedTracksStale([playing, other], saved);
    assert.deepEqual(marked.map((el) => el.id), ['os-en']);
});

test('activating a stale, loaded, empty track refetches it once, under a new URL', () => {
    const el = loadedTrack('os-en');
    markUnsnapshottedTracksStale([el], []);
    assert.equal(refreshStaleTrack(el), true);
    const first = el.getAttribute('src');
    assert.match(first, /^https:\/\/x\/os-en\.vtt\?token=t&wt-rf=\d+$/);
    // Unmarked by the refetch: a legitimately empty file is fetched once,
    // not on every re-assertion of the selection.
    assert.equal(refreshStaleTrack(el), false);
    assert.equal(el.getAttribute('src'), first);
});

test('a second refetch replaces the refresh parameter instead of piling them up', () => {
    const el = loadedTrack('os-en');
    markUnsnapshottedTracksStale([el], []);
    refreshStaleTrack(el);
    markUnsnapshottedTracksStale([el], []);
    refreshStaleTrack(el);
    assert.equal((el.getAttribute('src').match(/wt-rf=/g) || []).length, 1);
});

test('nothing to refetch: not stale, still has cues, never loaded, or a reload already on its way', () => {
    const fresh = loadedTrack('a');
    assert.equal(refreshStaleTrack(fresh), false, 'never marked');

    const withCues = loadedTrack('b', { cues: [{ startTime: 1, endTime: 2 }] });
    markUnsnapshottedTracksStale([withCues], []);
    assert.equal(refreshStaleTrack(withCues), false, 'cues are there — whatever emptied it did not');
    withCues.track.cues.length = 0;
    assert.equal(refreshStaleTrack(withCues), false, 'and the mark is gone with that answer');

    // Not loaded yet: turning it on starts its first load, which brings the
    // cues on its own.
    const unloaded = loadedTrack('c', { readyState: 0 });
    markUnsnapshottedTracksStale([unloaded], []);
    assert.equal(refreshStaleTrack(unloaded), false);

    // An AI track with a revision swap in flight: that load is the refetch.
    const video = videoWith(loadedTrack('tr-pt'));
    const ai = video.querySelectorAll()[0];
    markUnsnapshottedTracksStale([ai], []);
    reloadSubtitleTrack(video, 'tr-pt', 'https://x/tr-pt.vtt?rev=3');
    assert.equal(refreshStaleTrack(ai), false);
    assert.equal(ai.getAttribute('src'), 'https://x/tr-pt.vtt?rev=3');

    assert.equal(refreshStaleTrack(null), false);
});
