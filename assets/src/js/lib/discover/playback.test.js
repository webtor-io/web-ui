import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    transcoderAnswer, browserDecodesHevc, browserDecodesPq, uhdPlaysHere, uhdWarningKey,
    isHdrRelease, switchStates, hiddenBy, switchCounts, emptyStateKey, playbackContext,
} from './playback.js';

const HEVC = ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160'];
const EVERYTHING = [...HEVC, 'hevc-high', 'hdr-pq'];

// The browser's answer, as decodedTokens gives it.
const NOT_ANSWERED = null;
const DECODES_NONE = [];

const states = (over = {}) => switchStates({ caps: 'off', decodes: NOT_ANSWERED, part: false, declared: null, prefs: {}, ...over });

test('transcoderAnswer: on and off as said; anything else is unknown, never off', () => {
    assert.equal(transcoderAnswer({ _passthrough: { hevc: 'on' } }), 'on');
    assert.equal(transcoderAnswer({ _passthrough: { hevc: 'off' } }), 'off');
    assert.equal(transcoderAnswer({ _passthrough: { hevc: 'unknown' } }), 'unknown');
    assert.equal(transcoderAnswer({ _passthrough: { hevc: 'yes' } }), 'unknown');
    assert.equal(transcoderAnswer({}), 'unknown', 'a page without the value');
    const throwing = {};
    Object.defineProperty(throwing, '_passthrough', { get() { throw new Error('no'); } });
    assert.equal(transcoderAnswer(throwing), 'unknown');
});

test('browserDecodesHevc: any HEVC token counts (Main10 and 4K cover Main 1080p); none is no; no answer is null', () => {
    assert.equal(browserDecodesHevc(['hevc8']), true);
    assert.equal(browserDecodesHevc(['hevc10']), true, 'Main10 covers Main');
    assert.equal(browserDecodesHevc(['hevc8-2160']), true, '4K covers 1080p');
    assert.equal(browserDecodesHevc(['hdr-pq']), false, 'PQ alone is not HEVC');
    assert.equal(browserDecodesHevc(DECODES_NONE), false);
    assert.equal(browserDecodesHevc(NOT_ANSWERED), null);
    assert.equal(browserDecodesHevc(undefined), null);
});

test('browserDecodesPq: hdr-pq, or not; null without an answer', () => {
    assert.equal(browserDecodesPq(EVERYTHING), true);
    assert.equal(browserDecodesPq(HEVC), false);
    assert.equal(browserDecodesPq(DECODES_NONE), false);
    assert.equal(browserDecodesPq(NOT_ANSWERED), null);
});

// ---- the gate: does 4K HEVC play here -------------------------------------

test('uhdPlaysHere: only the transcoder passing HEVC through AND a declaration with 4K Main10', () => {
    assert.equal(uhdPlaysHere('on', EVERYTHING), true);
    assert.equal(uhdPlaysHere('on', ['hevc8', 'hevc10', 'hevc10-2160']), true);
    assert.equal(uhdPlaysHere('off', EVERYTHING), false, 'the transcoder converts');
    assert.equal(uhdPlaysHere('unknown', EVERYTHING), false, 'not known is not on');
    assert.equal(uhdPlaysHere('on', null), false, 'this page declares nothing');
    // A device with 4K Main but not Main10 would be turned away from 9 in
    // 10 4K HEVC sources; one with 1080p HEVC only, from all of them.
    assert.equal(uhdPlaysHere('on', ['hevc8', 'hevc8-2160']), false);
    assert.equal(uhdPlaysHere('on', ['hevc8', 'hevc10']), false);
    assert.equal(uhdPlaysHere('on', ['hevc8']), false);
});

test('uhdWarningKey: unknown says the check did not happen; a declaring browser without 4K says so; else the old text', () => {
    // A page that does not declare takes the old route whatever the
    // transcoder says: its text does not wait on the transcoder's answer
    // (before the transcoder has GET /capabilities, every page reads unknown).
    assert.equal(uhdWarningKey('unknown', false, null), 'discover.warning4kBody', 'not declaring: the answer is not asked');
    assert.equal(uhdWarningKey('unknown', true, EVERYTHING), 'discover.warning4kBodyUnchecked');
    assert.equal(uhdWarningKey('unknown', true, null), 'discover.warning4kBodyUnchecked');
    assert.equal(uhdWarningKey('on', true, null), 'discover.warning4kBodyUnchecked', 'taking part, browser not answered');
    assert.equal(uhdWarningKey('on', true, ['hevc8', 'hevc8-2160']), 'discover.warning4kBodyNoHevc');
    assert.equal(uhdWarningKey('on', true, []), 'discover.warning4kBodyNoHevc');
    assert.equal(uhdWarningKey('on', false, null), 'discover.warning4kBody', 'not declaring: its sessions take the old route');
    assert.equal(uhdWarningKey('off', true, EVERYTHING), 'discover.warning4kBody');
    assert.equal(uhdWarningKey('off', false, null), 'discover.warning4kBody');
});

test('the 4K switch: gone and 4K shown where 4K plays; kept, hidden by default, elsewhere', () => {
    let s = states({ caps: 'on', part: true, declared: EVERYTHING });
    assert.equal(s.uhd.switchable, false);
    assert.equal(s.uhd.shown, true);
    // show4k answered "show 4K that will not play here?": where it plays the
    // question does not exist, and an old "no" hides nothing without a switch.
    s = states({ caps: 'on', part: true, declared: EVERYTHING, prefs: { show4k: false } });
    assert.equal(s.uhd.switchable, false);
    assert.equal(s.uhd.shown, true);

    for (const over of [
        { caps: 'off', part: true, declared: EVERYTHING },
        { caps: 'unknown', part: true, declared: EVERYTHING },
        { caps: 'on', part: false, declared: null },
        { caps: 'on', part: true, declared: null },
        { caps: 'on', part: true, declared: ['hevc8', 'hevc8-2160', 'hdr-pq'] },
    ]) {
        s = states(over);
        assert.equal(s.uhd.switchable, true, JSON.stringify(over));
        assert.equal(s.uhd.shown, false, JSON.stringify(over));
        s = states({ ...over, prefs: { show4k: true } });
        assert.equal(s.uhd.shown, true, `${JSON.stringify(over)}: the viewer's "show 4K" is kept`);
    }
});

// ---- the defaults: what the browser decodes --------------------------------

test('HEVC and HDR defaults follow the browser\'s own answer, whatever the transcoder says', () => {
    for (const caps of ['on', 'off', 'unknown']) {
        let s = states({ caps, decodes: EVERYTHING });
        assert.deepEqual([s.hevc.shown, s.hevc.warns, s.hdr.shown, s.hdr.warns], [true, false, true, false], `${caps}: decodes both`);
        s = states({ caps, decodes: HEVC });
        assert.deepEqual([s.hevc.shown, s.hevc.warns, s.hdr.shown, s.hdr.warns], [true, false, false, true], `${caps}: HEVC without PQ`);
        s = states({ caps, decodes: ['hevc8'] });
        assert.deepEqual([s.hevc.shown, s.hdr.shown], [true, false], `${caps}: 1080p Main only`);
        s = states({ caps, decodes: DECODES_NONE });
        assert.deepEqual([s.hevc.shown, s.hevc.warns, s.hdr.shown, s.hdr.warns], [false, true, false, true], `${caps}: decodes none`);
    }
});

// The rule the switches are built around: a check that has not answered is
// not a browser that cannot decode.
test('a browser that has not answered keeps HEVC and HDR shown and is not warned', () => {
    const s = states({ decodes: NOT_ANSWERED });
    assert.deepEqual([s.hevc.shown, s.hevc.warns, s.hdr.shown, s.hdr.warns], [true, false, true, false]);
});

test('the viewer\'s choice wins over the default, both ways', () => {
    let s = states({ decodes: EVERYTHING, prefs: { showHevc: false, showHdr: false } });
    assert.deepEqual([s.hevc.shown, s.hdr.shown], [false, false]);
    s = states({ decodes: DECODES_NONE, prefs: { showHevc: true, showHdr: true } });
    assert.deepEqual([s.hevc.shown, s.hdr.shown], [true, true]);
    assert.equal(s.hevc.warns, true, 'shown by choice, still a browser that would be warned');
    // Only booleans are choices.
    s = states({ decodes: DECODES_NONE, prefs: { showHevc: 'yes', showHdr: 1 } });
    assert.deepEqual([s.hevc.shown, s.hdr.shown], [false, false]);
});

// ---- what each switch hides ------------------------------------------------

const row = (codec, hdr = null, { dv5 = false, uhd = false } = {}) => ({ video: { codec, hdr, dv5 }, uhd });

test('isHdrRelease: PQ and Dolby Vision with an HDR10 layer; not profile 5, not HLG', () => {
    assert.equal(isHdrRelease({ codec: 'hevc', hdr: 'pq', dv5: false }), true);
    assert.equal(isHdrRelease({ codec: 'hevc', hdr: 'dv', dv5: false }), true);
    assert.equal(isHdrRelease({ codec: 'hevc', hdr: 'dv', dv5: true }), false);
    assert.equal(isHdrRelease({ codec: 'hevc', hdr: 'pq', dv5: true }), false, 'P5 said outright next to HDR');
    assert.equal(isHdrRelease({ codec: 'hevc', hdr: 'hlg', dv5: false }), false);
    assert.equal(isHdrRelease({ codec: 'hevc', hdr: null, dv5: false }), false);
});

test('hiddenBy: HEVC by name, HDR by name, 4K by its label; a release of unknown codec is never hidden as HEVC', () => {
    const off = states({ decodes: DECODES_NONE });
    assert.deepEqual(hiddenBy(row('hevc'), off), ['hevc']);
    assert.deepEqual(hiddenBy(row('unknown'), off), []);
    assert.deepEqual(hiddenBy(row('avc'), off), []);
    assert.deepEqual(hiddenBy(row('hevc', 'pq', { uhd: true }), off), ['hevc', 'hdr', 'uhd']);
    assert.deepEqual(hiddenBy(row('unknown', 'dv', { dv5: true }), off), [], 'a DV5 release is badged, not hidden by HDR');
    const on = states({ caps: 'on', part: true, declared: EVERYTHING, decodes: EVERYTHING });
    assert.deepEqual(hiddenBy(row('hevc', 'pq', { uhd: true }), on), []);
});

test('switchCounts: the number in each switch, before filtering', () => {
    assert.deepEqual(switchCounts([
        row('hevc', 'pq', { uhd: true }), row('hevc'), row('avc', null, { uhd: true }), row('unknown', 'dv', { dv5: true }), row('hevc', 'dv'),
    ]), { hevc: 3, hdr: 2, uhd: 2 });
});

test('emptyStateKey: the one switch that hides everything is named; several are named together; anything left, no text', () => {
    const none = states({ decodes: DECODES_NONE });
    assert.equal(emptyStateKey([row('hevc'), row('hevc')], none), 'discover.allHevcStreams');
    assert.equal(emptyStateKey([row('avc', null, { uhd: true })], none), 'discover.all4kStreams');
    const noPq = states({ decodes: HEVC });
    assert.equal(emptyStateKey([row('hevc', 'pq'), row('unknown', 'dv')], noPq), 'discover.allHdrStreams');
    assert.equal(emptyStateKey([row('hevc'), row('avc', null, { uhd: true })], none), 'discover.allHiddenStreams');
    assert.equal(emptyStateKey([row('hevc', null, { uhd: true })], none), 'discover.allHiddenStreams', 'one release, two switches');
    assert.equal(emptyStateKey([row('hevc'), row('avc')], none), null);
    assert.equal(emptyStateKey([], none), null);
});

// ---- gathering the inputs --------------------------------------------------

test('playbackContext: the page\'s answers; whatever throws is an unanswered check', () => {
    const win = { _passthrough: { hevc: 'on' } };
    assert.deepEqual(playbackContext(win, {
        decodedTokens: () => EVERYTHING, takesPart: () => true, declaredTokens: () => HEVC,
    }), { caps: 'on', decodes: EVERYTHING, part: true, declared: HEVC });
    const boom = () => { throw new Error('storage'); };
    assert.deepEqual(playbackContext({}, { decodedTokens: boom, takesPart: boom, declaredTokens: boom }),
        { caps: 'unknown', decodes: null, part: false, declared: null });
});
