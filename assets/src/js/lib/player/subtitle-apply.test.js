// The invariant, on its own: one selection in, one state of the two
// renderers out. No DOM is needed for this half — a text track is an id
// and a mode, and hls.js is two properties — so these run as plain node
// tests and the wiring test above them only has to prove that the player
// calls this with the right selection at the right moments.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    applySubtitleSelection,
    isEmbedded,
    readSelection,
    selectionFor,
    selectionHolds,
} from './subtitle-apply.js';

// A text track is an id (empty for the ones hls.js creates from the
// manifest — those have no <track> element) and a mode.
const track = (id, mode = 'disabled') => ({ id, mode });
const video = (...tracks) => ({ textTracks: tracks });

// The two properties of hls.js this module writes, with a log: the order of
// the writes matters (a subtitleDisplay change toggles modes only while a
// track is selected), so a test can assert on it.
function makeHls({ subtitleTrack = -1, subtitleDisplay = true } = {}) {
    const writes = [];
    let t = subtitleTrack;
    let d = subtitleDisplay;
    return {
        writes,
        get subtitleTrack() { return t; },
        set subtitleTrack(v) { t = v; writes.push(['subtitleTrack', v]); },
        get subtitleDisplay() { return d; },
        set subtitleDisplay(v) { d = v; writes.push(['subtitleDisplay', v]); },
    };
}

const sideLoaded = (id) => ({ id, provider: 'OpenSubtitles', mpId: '' });
const embedded = (mpId) => ({ id: `mp-${mpId}`, provider: 'MediaProbe', mpId: String(mpId) });
const none = () => ({ id: 'none', provider: '', mpId: null });

// ---- the side-loaded selection ---------------------------------------

test('a side-loaded selection turns hls.js off and disables its tracks — never hides them', () => {
    // A hidden manifest track is the latch: hls.js's onTextTracksChanged
    // remembers the last 'hidden' subtitle track and hands it back to
    // setSubtitleTrack, which starts loading its cues.
    const rus = track('', 'showing');
    const eng = track('', 'hidden');
    const ai = track('tr-ca');
    const v = video(rus, eng, ai);
    const hls = makeHls({ subtitleTrack: 0, subtitleDisplay: true });

    applySubtitleSelection(v, hls, sideLoaded('tr-ca'));

    assert.equal(hls.subtitleTrack, -1);
    assert.equal(hls.subtitleDisplay, false);
    assert.equal(ai.mode, 'showing');
    assert.equal(rus.mode, 'disabled');
    assert.equal(eng.mode, 'disabled');
    assert.equal([rus, eng].filter((t) => t.mode === 'hidden').length, 0,
        'no hls.js-managed track is left in the mode onTextTracksChanged latches onto');
    // -1 before display:false — the other order leaves the controller with
    // a track selected while display flips, which toggles modes back on.
    assert.deepEqual(hls.writes, [['subtitleTrack', -1], ['subtitleDisplay', false]]);
});

test('"None" is the same state with nothing showing', () => {
    const rus = track('', 'showing');
    const ai = track('tr-ca', 'showing');
    const hls = makeHls({ subtitleTrack: 1 });

    applySubtitleSelection(video(rus, ai), hls, none());

    assert.equal(hls.subtitleTrack, -1);
    assert.equal(hls.subtitleDisplay, false);
    assert.equal(rus.mode, 'disabled');
    assert.equal(ai.mode, 'disabled');
});

test('an embedded selection goes to hls.js and takes the element tracks off screen', () => {
    const rus = track('');
    const ai = track('tr-ca', 'showing');
    const hls = makeHls({ subtitleTrack: -1, subtitleDisplay: false });

    applySubtitleSelection(video(rus, ai), hls, embedded(1));

    assert.equal(hls.subtitleTrack, 1);
    assert.equal(hls.subtitleDisplay, true);
    assert.equal(ai.mode, 'disabled', 'the side-loaded track stops being fetched and drawn');
    // hls.js's own toggleTrackModes owns the manifest tracks here.
    assert.equal(rus.mode, 'disabled');
    assert.deepEqual(hls.writes, [['subtitleDisplay', true], ['subtitleTrack', 1]]);
});

test('a MediaProbe chip with no index is not an embedded selection', () => {
    // parseInt(null) is NaN, and hls.js answers an invalid id with a
    // warning and no change — which left whatever was playing on screen
    // under a chip that said something else.
    const rus = track('');
    const hls = makeHls({ subtitleTrack: 0, subtitleDisplay: true });
    const selection = { id: 'mp-0', provider: 'MediaProbe', mpId: '' };

    assert.equal(isEmbedded(selection), false);
    applySubtitleSelection(video(rus), hls, selection);

    assert.equal(hls.subtitleTrack, -1);
    assert.equal(rus.mode, 'disabled');
});

test('no hls.js instance: the element modes still land', () => {
    // The ordinary mount race — activateSubtitle runs before useHls has
    // created the instance — and native HLS on iOS, which never has one.
    const ai = track('tr-ca');
    const other = track('os-os-ru', 'showing');
    applySubtitleSelection(video(ai, other), null, sideLoaded('tr-ca'));
    assert.equal(ai.mode, 'showing');
    assert.equal(other.mode, 'disabled');
});

test('no selection is not a selection of "None"', () => {
    const rus = track('', 'showing');
    const hls = makeHls({ subtitleTrack: 0 });
    applySubtitleSelection(video(rus), hls, null);
    assert.equal(rus.mode, 'showing', 'a page without a picker keeps what the manifest declared');
    assert.deepEqual(hls.writes, []);
    assert.equal(selectionHolds(video(rus), hls, null), true);
});

// ---- the disagreement check ------------------------------------------

test('selectionHolds catches every shape of the overlap', () => {
    const hlsOff = () => makeHls({ subtitleTrack: -1, subtitleDisplay: false });

    // The state applySubtitleSelection leaves behind.
    assert.equal(
        selectionHolds(video(track(''), track('tr-ca', 'showing')), hlsOff(), sideLoaded('tr-ca')),
        true);

    // hls.js latched onto a track of its own.
    assert.equal(
        selectionHolds(video(track('', 'showing'), track('tr-ca', 'showing')), makeHls({ subtitleTrack: 0 }), sideLoaded('tr-ca')),
        false);
    // ... even with subtitleTrack still reading -1: the mode is the
    // measured symptom (stage, 2026-09-16).
    assert.equal(
        selectionHolds(video(track('', 'showing'), track('tr-ca', 'showing')), hlsOff(), sideLoaded('tr-ca')),
        false);
    // A hidden manifest track is a disagreement too — it is the latch, not
    // yet the overlap.
    assert.equal(
        selectionHolds(video(track('', 'hidden'), track('tr-ca', 'showing')), hlsOff(), sideLoaded('tr-ca')),
        false);
    // The chosen track not being on screen at all.
    assert.equal(
        selectionHolds(video(track(''), track('tr-ca', 'disabled')), hlsOff(), sideLoaded('tr-ca')),
        false);
    // "None" with an element track still showing.
    assert.equal(
        selectionHolds(video(track('tr-ca', 'showing')), hlsOff(), none()),
        false);
    // An embedded selection holds while hls.js is on it and nothing
    // element-backed is drawn over it.
    assert.equal(
        selectionHolds(video(track(''), track('tr-ca')), makeHls({ subtitleTrack: 1, subtitleDisplay: true }), embedded(1)),
        true);
    assert.equal(
        selectionHolds(video(track(''), track('tr-ca', 'showing')), makeHls({ subtitleTrack: 1, subtitleDisplay: true }), embedded(1)),
        false);
    assert.equal(
        selectionHolds(video(track('')), makeHls({ subtitleTrack: 1, subtitleDisplay: false }), embedded(1)),
        false, 'selected but not displayed is not what the chip claims');
});

test('applying twice is a fixed point', () => {
    // What makes the re-assertion terminate: the second pass finds nothing
    // to disagree with.
    const v = video(track('', 'hidden'), track('tr-ca'));
    const hls = makeHls({ subtitleTrack: 0, subtitleDisplay: true });
    applySubtitleSelection(v, hls, sideLoaded('tr-ca'));
    assert.equal(selectionHolds(v, hls, sideLoaded('tr-ca')), true);
    const before = hls.writes.length;
    if (!selectionHolds(v, hls, sideLoaded('tr-ca'))) applySubtitleSelection(v, hls, sideLoaded('tr-ca'));
    assert.equal(hls.writes.length, before);
});

// ---- reading the chip -------------------------------------------------

test('selectionFor and readSelection read the marked chip, and nothing when there is none', () => {
    const chip = (attrs) => ({ getAttribute: (n) => (n in attrs ? attrs[n] : null) });
    assert.deepEqual(
        selectionFor(chip({ 'data-id': 'tr-ca', 'data-provider': 'Translated', 'data-mp-id': '' })),
        { id: 'tr-ca', provider: 'Translated', mpId: '' });
    assert.equal(selectionFor(null), null);

    const found = chip({ 'data-id': 'mp-0', 'data-provider': 'MediaProbe', 'data-mp-id': '0' });
    assert.deepEqual(
        readSelection({ querySelector: (sel) => (sel === '.subtitle[data-default="true"]' ? found : null) }),
        { id: 'mp-0', provider: 'MediaProbe', mpId: '0' });
    assert.equal(readSelection({ querySelector: () => null }), null);
    assert.equal(readSelection(null), null);
});

// ---- a track a seek emptied is refetched when it is picked -------------

test('picking a side-loaded track a seek emptied refetches it; the ones left off are not touched', async () => {
    const { markUnsnapshottedTracksStale } = await import('./subtitle-track-reload.js');
    const element = (id) => {
        const attrs = { src: `https://x/${id}.vtt` };
        const t = { id, mode: 'disabled', cues: [] };
        return { id, readyState: 2, track: t, getAttribute: (n) => attrs[n] ?? null, setAttribute: (n, v) => { attrs[n] = v; } };
    };
    const picked = element('os-ru');
    const other = element('os-en');
    const v = {
        textTracks: [picked.track, other.track],
        querySelectorAll: () => [picked, other],
    };
    markUnsnapshottedTracksStale([picked, other], []);

    applySubtitleSelection(v, makeHls(), sideLoaded('os-ru'));

    assert.equal(picked.track.mode, 'showing');
    assert.match(picked.getAttribute('src'), /wt-rf=\d+$/, 'the emptied track the viewer picked is fetched again');
    assert.equal(other.getAttribute('src'), 'https://x/os-en.vtt', 'a track left off costs no request');
});
