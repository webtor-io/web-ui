import { test } from 'node:test';
import assert from 'node:assert/strict';
import { remapTrackGroup } from './hls-manager.js';

// Fake DOM element: records every setAttribute so a test can tell an
// untouched element from one that was re-pointed to the same value.
function makeEl({ mpId = '', srclang = '', label = '', dataLabel = null } = {}) {
    const attrs = { 'data-mp-id': mpId, 'data-srclang': srclang };
    if (dataLabel !== null) attrs['data-label'] = dataLabel;
    const writes = [];
    return {
        textContent: label,
        getAttribute: (n) => (n in attrs ? attrs[n] : null),
        setAttribute: (n, v) => {
            attrs[n] = v;
            writes.push([n, v]);
        },
        get mpId() {
            return attrs['data-mp-id'];
        },
        writes,
    };
}

// Since GetSubtitles started hiding bitmap/forced embedded tracks, a
// hidden track still occupies an HLS index, so elements.length !==
// hlsTracks.length is normal and the server-rendered data-mp-id is
// authoritative. A lang-only fallback would hand the visible element
// the hidden forced track's index — the exact bug this guards.
test('counts differ: a weak lang-only match must not steal the hidden track index', () => {
    const el = makeEl({ mpId: '1', srclang: 'en', label: 'Subtitle #2' });
    const hlsTracks = [
        { lang: 'en', name: 'Forced' },
        { lang: 'en', name: 'English' },
    ];
    remapTrackGroup([el], hlsTracks);
    assert.equal(el.mpId, '1');
    assert.deepEqual(el.writes, []);
});

test('counts differ: a name-only match must not override the server id either', () => {
    const el = makeEl({ mpId: '2', srclang: 'ru', label: 'English' });
    const hlsTracks = [
        { lang: 'en', name: 'Forced' },
        { lang: 'en', name: 'English' },
        { lang: 'ru', name: '' },
    ];
    remapTrackGroup([el], hlsTracks);
    assert.equal(el.mpId, '2');
    assert.deepEqual(el.writes, []);
});

test('counts differ: the exact lang+name pass still runs', () => {
    const el = makeEl({ mpId: '0', srclang: 'en', label: 'English' });
    const hlsTracks = [
        { lang: 'en', name: 'Forced' },
        { lang: 'en', name: 'English' },
    ];
    remapTrackGroup([el], hlsTracks);
    assert.equal(el.mpId, '1');
});

test('counts equal: sequential assignment is unchanged', () => {
    const els = [
        makeEl({ mpId: '', srclang: 'en', label: 'English' }),
        makeEl({ mpId: '', srclang: 'ru', label: 'Russian' }),
    ];
    remapTrackGroup(els, [
        { lang: 'ru', name: 'Russian' },
        { lang: 'en', name: 'English' },
    ]);
    assert.deepEqual(els.map((e) => e.mpId), ['0', '1']);
});

test('no elements or no tracks is a no-op', () => {
    const el = makeEl({ mpId: '3', srclang: 'en', label: 'English' });
    remapTrackGroup([el], []);
    remapTrackGroup([], [{ lang: 'en', name: 'English' }]);
    assert.equal(el.mpId, '3');
    assert.deepEqual(el.writes, []);
});

// The picker chip's text is no longer the track name: it carries the
// origin code, any property tag and the source suffix around the label.
// data-label (controller ruling R7) is what still equals the manifest's
// track name, and the exact lang+name pass is the only thing that may
// refine a server-rendered id — reading textContent here would make that
// pass dead code on every chip.
test('counts differ: the exact match reads data-label, not the chip\'s decorated text', () => {
    const el = makeEl({
        mpId: '0',
        srclang: 'en',
        dataLabel: 'English',
        label: 'EM English forced \u00b7 hash',
    });
    remapTrackGroup([el], [
        { lang: 'en', name: 'Forced' },
        { lang: 'en', name: 'English' },
    ]);
    assert.equal(el.mpId, '1');
});

// ---- a passthrough stream's errors -------------------------------------------

import { setupHlsEvents } from './hls-manager.js';
import Hls from 'hls.js';

function busHls() {
    const handlers = new Map();
    return {
        levels: [],
        recovered: 0,
        startLoads: 0,
        media: null,
        inFlightFragments: {},
        on(ev, fn) { handlers.set(ev, [...(handlers.get(ev) || []), fn]); },
        off() {},
        trigger(ev, data) { for (const fn of handlers.get(ev) || []) fn(ev, data); },
        recoverMediaError() { this.recovered++; },
        startLoad() { this.startLoads++; },
        destroy() {},
    };
}
const fatalMedia = { type: Hls.ErrorTypes.MEDIA_ERROR, details: Hls.ErrorDetails.BUFFER_APPEND_ERROR, fatal: true };
// A fatal network error is answered after the backoff's first step, not at
// once (network-recovery.js): these tests turn that timer by hand.
function netTimers() {
    const due = [];
    return {
        opts: { setTimer: (fn, ms) => { due.push({ fn, ms }); return due.length; }, clearTimer: () => {}, log: () => {} },
        fire() { const d = due.splice(0); for (const x of d) x.fn(); return d.map((x) => x.ms); },
    };
}

// The old route is exactly what it was: every fatal media error recovers,
// however many (the passthrough's "once" is its own).
test('old route: three fatal media errors, three recoveries, nothing else', () => {
    const hls = busHls();
    setupHlsEvents(hls, { now: () => 0, setInterval: () => 0, clearInterval: () => {} });
    for (let i = 0; i < 3; i++) hls.trigger(Hls.Events.ERROR, fatalMedia);
    assert.equal(hls.recovered, 3);
    hls.trigger(Hls.Events.ERROR, { type: Hls.ErrorTypes.MEDIA_ERROR, details: Hls.ErrorDetails.MANIFEST_INCOMPATIBLE_CODECS_ERROR, fatal: true });
    assert.equal(hls.recovered, 4, 'an incompatible codec string recovers as ever');
});

// A passthrough's guard sees an error first; what it handles goes no
// further, and what it does not (network) is handled as on every route.
test('passthrough: the guard is first, and the rest is the old handling', () => {
    const hls = busHls();
    const seen = [];
    const guard = { onHlsError: (h, data) => { seen.push(data.details); return data.type === Hls.ErrorTypes.MEDIA_ERROR; } };
    const net = netTimers();
    setupHlsEvents(hls, { now: () => 0, setInterval: () => 0, clearInterval: () => {} }, guard, net.opts);
    hls.trigger(Hls.Events.ERROR, fatalMedia);
    assert.equal(hls.recovered, 0, 'the guard decides on media errors');
    hls.trigger(Hls.Events.ERROR, { type: Hls.ErrorTypes.NETWORK_ERROR, details: Hls.ErrorDetails.FRAG_LOAD_TIMEOUT, fatal: true });
    assert.equal(hls.startLoads, 0, 'not at once');
    assert.deepEqual(net.fire(), [1000]);
    assert.equal(hls.startLoads, 1, 'a network error restarts loading as on every route, after the backoff\'s first step');
    assert.deepEqual(seen, [Hls.ErrorDetails.BUFFER_APPEND_ERROR, Hls.ErrorDetails.FRAG_LOAD_TIMEOUT]);
});

// ---- a start that declared multichannel audio, on the old route ------------

import { createAudioGuard, SAME_INCIDENT_MS } from './passthrough.js';

// Every stream without a guard -- every browser that has not opted in --
// registers exactly what it registered before multichannel audio (recorded
// off the code at 1bea6ac8): no BUFFER_CODECS listener. The last three are
// the fatal network errors' handler (network-recovery.js, 2026-09-29): a
// fragment of the film resets its backoff, a new source is a new start,
// destroy clears its timer.
test('no guard: the listeners are exactly today\'s', () => {
    const names = [];
    const hls = { ...busHls(), on(ev) { names.push(ev); } };
    setupHlsEvents(hls, { now: () => 0, setInterval: () => 0, clearInterval: () => {} });
    assert.deepEqual(names, ['hlsManifestParsed', 'hlsError', 'hlsError', 'hlsStallResolved', 'hlsManifestLoading', 'hlsMediaDetaching', 'hlsDestroying',
        'hlsFragLoaded', 'hlsManifestLoading', 'hlsDestroying']);
    const withGuard = [];
    setupHlsEvents({ ...busHls(), on(ev) { withGuard.push(ev); } }, { now: () => 0, setInterval: () => 0, clearInterval: () => {} }, { onHlsError: () => false, onBufferCodecs() {} });
    assert.deepEqual(withGuard.filter((n) => !names.includes(n)), [Hls.Events.BUFFER_CODECS], 'a guard is told what audio hls.js buffers');
});

function audioVideo(dataset) {
    let err = null;
    const listeners = {};
    return {
        dataset,
        get error() { return err; },
        setError(code) { err = code ? { code, message: '' } : null; },
        addEventListener(ev, fn) { listeners[ev] = fn; },
        removeEventListener() {},
    };
}

// The audio SourceBuffer's own append failed (hls.js buffer-controller.ts
// onSBUpdateError: non-fatal, named after the buffer) -- what pins a
// failure on the audio.
const audioAppending = { type: Hls.ErrorTypes.MEDIA_ERROR, details: Hls.ErrorDetails.BUFFER_APPENDING_ERROR, sourceBufferName: 'audio', fatal: false };

test('old route, multichannel audio declared: the guard recovers once and gives the file up; hls-manager does not recover it again', () => {
    const hls = busHls();
    const video = audioVideo({ decode: 'aac51', videoRoute: 'reencode' });
    let t = 0;
    const fired = [];
    const guard = createAudioGuard({ video, fallback: (...a) => fired.push(a), now: () => t, setTimer: () => 0, clearTimer: () => {} });
    guard.setHls(hls);
    setupHlsEvents(hls, { now: () => 0, setInterval: () => 0, clearInterval: () => {} }, guard);
    hls.trigger(Hls.Events.BUFFER_CODECS, { audio: { codec: 'mp4a.40.2', metadata: { channelCount: 6 } } });
    hls.trigger(Hls.Events.ERROR, fatalMedia);
    assert.equal(hls.recovered, 1, 'nobody pinned it on the audio: hls-manager\'s recovery, as ever');
    hls.trigger(Hls.Events.ERROR, audioAppending);
    hls.trigger(Hls.Events.ERROR, fatalMedia);
    assert.equal(hls.recovered, 2, 'the guard\'s one recovery');
    t += SAME_INCIDENT_MS + 1;
    hls.trigger(Hls.Events.ERROR, audioAppending);
    hls.trigger(Hls.Events.ERROR, fatalMedia);
    assert.equal(hls.recovered, 2, 'no further recovery');
    assert.deepEqual(fired, [['media_error', 'mse', 'aac51', 'buffer']], 'with what blamed the audio: its buffer');
    // A network error still restarts loading, as on every route -- the page
    // is restarting, so the guard keeps it.
    hls.trigger(Hls.Events.ERROR, { type: Hls.ErrorTypes.NETWORK_ERROR, details: Hls.ErrorDetails.FRAG_LOAD_TIMEOUT, fatal: true });
    assert.equal(hls.startLoads, 0);
});

test('old route, multichannel audio declared but stereo in play: every fatal media error recovers, as ever', () => {
    const hls = busHls();
    const video = audioVideo({ decode: 'aac51', videoRoute: 'reencode' });
    let t = 0;
    const fired = [];
    const guard = createAudioGuard({ video, fallback: (...a) => fired.push(a), now: () => t, setTimer: () => 0, clearTimer: () => {} });
    guard.setHls(hls);
    const net = netTimers();
    setupHlsEvents(hls, { now: () => 0, setInterval: () => 0, clearInterval: () => {} }, guard, net.opts);
    hls.trigger(Hls.Events.BUFFER_CODECS, { audio: { codec: 'mp4a.40.2', metadata: { channelCount: 2 } } });
    for (let i = 0; i < 3; i++) {
        t += SAME_INCIDENT_MS + 1;
        hls.trigger(Hls.Events.ERROR, fatalMedia);
    }
    assert.equal(hls.recovered, 3);
    assert.deepEqual(fired, []);
    hls.trigger(Hls.Events.ERROR, { type: Hls.ErrorTypes.NETWORK_ERROR, details: Hls.ErrorDetails.FRAG_LOAD_TIMEOUT, fatal: true });
    net.fire();
    assert.equal(hls.startLoads, 1);
});
