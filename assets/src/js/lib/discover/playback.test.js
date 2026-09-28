import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    transcoderAnswer, browserDecodesHevc, browserDecodesPq, uhdPlaysHere, uhdWarningKey, uhdReleasePlays,
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
    // Where 4K HEVC plays the switch holds only releases that take the old
    // route (uhdReleasePlays): the old text is true for them.
    assert.equal(uhdWarningKey('on', true, EVERYTHING), 'discover.warning4kBody');
    assert.equal(uhdWarningKey('on', false, null), 'discover.warning4kBody', 'not declaring: its sessions take the old route');
    assert.equal(uhdWarningKey('off', true, EVERYTHING), 'discover.warning4kBody');
    assert.equal(uhdWarningKey('off', false, null), 'discover.warning4kBody');
});

const uhdRow = (codec = 'hevc', hdr = null, dv5 = false) => ({ video: { codec, hdr, dv5 }, uhd: true });

test('the 4K switch: where 4K plays, 4K that plays is shown and no switch is about it; elsewhere every 4K release is behind it, hidden by default', () => {
    const playing = [uhdRow('hevc', 'pq'), uhdRow('avc'), uhdRow('unknown')];
    let s = states({ caps: 'on', part: true, declared: EVERYTHING });
    assert.equal(s.uhd.plays, true);
    assert.deepEqual(playing.map((r) => hiddenBy(r, s)), [[], [], []]);
    assert.equal(switchCounts(playing, s).uhd, 0, 'no release for the switch: it does not stand');
    // show4k answered "show 4K that will not play here?": an old "no" hides
    // nothing that plays.
    s = states({ caps: 'on', part: true, declared: EVERYTHING, prefs: { show4k: false } });
    assert.deepEqual(playing.map((r) => hiddenBy(r, s)), [[], [], []]);

    for (const over of [
        { caps: 'off', part: true, declared: EVERYTHING },
        { caps: 'unknown', part: true, declared: EVERYTHING },
        { caps: 'on', part: false, declared: null },
        { caps: 'on', part: true, declared: null },
        { caps: 'on', part: true, declared: ['hevc8', 'hevc8-2160', 'hdr-pq'] },
    ]) {
        s = states(over);
        assert.equal(s.uhd.plays, false, JSON.stringify(over));
        assert.deepEqual(playing.map((r) => hiddenBy(r, s)), [['uhd'], ['uhd'], ['uhd']], JSON.stringify(over));
        assert.equal(switchCounts(playing, s).uhd, 3, JSON.stringify(over));
        s = states({ ...over, prefs: { show4k: true } });
        assert.deepEqual(playing.map((r) => hiddenBy(r, s)), [[], [], []], `${JSON.stringify(over)}: the viewer's "show 4K" is kept`);
    }
});

// The transcoder's own rules (content-transcoder route.go), read off the
// name: what it sends to the old route, which converts nothing over 1080p.
test('uhdReleasePlays: AV1, Dolby Vision 5, HLG, and HDR without hdr-pq do not play as 4K; the rest does', () => {
    const noPq = HEVC; // 4K Main10, no hdr-pq
    assert.equal(uhdReleasePlays({ codec: 'hevc', hdr: null, dv5: false }, EVERYTHING), true);
    assert.equal(uhdReleasePlays({ codec: 'hevc', hdr: 'pq', dv5: false }, EVERYTHING), true);
    assert.equal(uhdReleasePlays({ codec: 'hevc', hdr: 'dv', dv5: false }, EVERYTHING), true, 'DV with an HDR10 layer, PQ declared');
    assert.equal(uhdReleasePlays({ codec: 'avc', hdr: null, dv5: false }, EVERYTHING), true, '4K H.264: the old route copies it');
    assert.equal(uhdReleasePlays({ codec: 'unknown', hdr: null, dv5: false }, EVERYTHING), true, 'no codec named: taken as playing');
    assert.equal(uhdReleasePlays({ codec: 'av1', hdr: null, dv5: false }, EVERYTHING), false, 'not HEVC (not_hevc)');
    assert.equal(uhdReleasePlays({ codec: 'hevc', hdr: 'dv', dv5: true }, EVERYTHING), false, 'dv5');
    assert.equal(uhdReleasePlays({ codec: 'unknown', hdr: 'pq', dv5: true }, EVERYTHING), false, 'P5 said outright next to HDR');
    assert.equal(uhdReleasePlays({ codec: 'hevc', hdr: 'hlg', dv5: false }, EVERYTHING), false, 'HLG is not passed through yet (hlg_later)');
    assert.equal(uhdReleasePlays({ codec: 'hevc', hdr: 'pq', dv5: false }, noPq), false, 'PQ without hdr-pq (needs_pq)');
    assert.equal(uhdReleasePlays({ codec: 'hevc', hdr: 'dv', dv5: false }, noPq), false, 'an HDR10 layer without hdr-pq');
    assert.equal(uhdReleasePlays({ codec: 'hevc', hdr: null, dv5: false }, noPq), true, 'SDR needs no hdr-pq');
});

test('where 4K plays, the 4K switch holds only the 4K releases the transcoder turns away', () => {
    const hevc1080 = { video: { codec: 'hevc', hdr: null, dv5: false }, uhd: false };
    const list = [uhdRow('hevc', 'pq'), uhdRow('avc'), uhdRow('av1'), uhdRow('hevc', 'dv', true), hevc1080];
    const plays = states({ caps: 'on', part: true, declared: EVERYTHING, decodes: EVERYTHING });
    assert.deepEqual(switchCounts(list, plays), { hevc: 3, hdr: 1, uhd: 2 });
    assert.deepEqual(list.map((r) => hiddenBy(r, plays)), [[], [], ['uhd'], ['uhd'], []]);
    assert.equal(plays.uhd.warningKey, 'discover.warning4kBody', 'they take the old route, where 4K is converted');
    const shown = states({ caps: 'on', part: true, declared: EVERYTHING, decodes: EVERYTHING, prefs: { show4k: true } });
    assert.deepEqual(list.map((r) => hiddenBy(r, shown)), [[], [], [], [], []], 'the viewer\'s "show 4K" shows them');
    // 4K Main10 without hdr-pq, HDR turned on by the viewer: its 4K HDR is
    // still 4K that will not play here.
    const noPq = states({ caps: 'on', part: true, declared: HEVC, decodes: HEVC, prefs: { showHdr: true } });
    assert.deepEqual(hiddenBy(uhdRow('hevc', 'pq'), noPq), ['uhd']);
    assert.deepEqual(hiddenBy(uhdRow('hevc'), noPq), []);
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
    ], states()), { hevc: 3, hdr: 2, uhd: 2 });
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

// ---- the audio tokens are not Discover's business ---------------------------

// decode-declaration.js gives Discover the video part only (decodedTokens,
// declaredTokens); should audio tokens ever reach these arrays, no switch
// may move: the questions here are HEVC, PQ and 4K Main10.
test('audio tokens change no switch, no warning and no count', () => {
    const AUDIO = ['aac51', 'ac3', 'ec3'];
    const rows = [row('hevc', 'pq', { uhd: true }), row('hevc'), row('avc', null, { uhd: true }), row('hevc', 'dv'), row('av1', null, { uhd: true })];
    for (const caps of ['on', 'off', 'unknown']) {
        for (const part of [true, false]) {
            for (const [decodes, declared] of [[DECODES_NONE, DECODES_NONE], [HEVC, HEVC], [EVERYTHING, EVERYTHING], [EVERYTHING, null]]) {
                const plain = states({ caps, part, decodes, declared });
                const withAudio = states({ caps, part, decodes: [...decodes, ...AUDIO], declared: declared && [...declared, ...AUDIO] });
                const name = `${caps}/${part}/${decodes.join(',')}`;
                assert.deepEqual(
                    { ...withAudio, uhd: { ...withAudio.uhd, declared: null } },
                    { ...plain, uhd: { ...plain.uhd, declared: null } }, name);
                assert.deepEqual(switchCounts(rows, withAudio), switchCounts(rows, plain), name);
                assert.equal(emptyStateKey(rows, withAudio), emptyStateKey(rows, plain), name);
                for (const r of rows) assert.deepEqual(hiddenBy(r, withAudio), hiddenBy(r, plain), name);
            }
        }
    }
    assert.equal(browserDecodesHevc(AUDIO), false, 'audio alone: answered, no HEVC');
    assert.equal(browserDecodesPq(AUDIO), false);
    assert.equal(uhdPlaysHere('on', AUDIO), false);
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
