import { test } from 'node:test';
import assert from 'node:assert/strict';
import Hls from 'hls.js';
import { createLoaderRestart, loadProgress, receivingBody, NO_BYTES_MS, RESTART_AFTER_MS } from './loader-restart.js';
import { setupHlsEvents } from './hls-manager.js';

// A stand-in for an hls.js instance: its event bus, the two getters the
// restart reads (media, inFlightFragments) and a counted startLoad().
function fakeHls(media) {
    const handlers = new Map();
    return {
        media,
        inFlightFragments: { main: { frag: null, state: 'IDLE' }, audio: { frag: null, state: 'IDLE' } },
        startLoads: 0,
        levels: [],
        on(ev, fn) {
            if (!handlers.has(ev)) handlers.set(ev, []);
            handlers.get(ev).push(fn);
        },
        off(ev, fn) {
            const a = handlers.get(ev) || [];
            const i = a.indexOf(fn);
            if (i >= 0) a.splice(i, 1);
        },
        trigger(ev, data) {
            for (const fn of [...(handlers.get(ev) || [])]) fn(ev, data);
        },
        listeners(ev) {
            return (handlers.get(ev) || []).length;
        },
        startLoad() {
            this.startLoads++;
        },
    };
}

function ranges(list) {
    return { length: list.length, start: (i) => list[i][0], end: (i) => list[i][1] };
}

// The element as the recorded runs left it at a stall at the cap: playing,
// starved (readyState 2), the playhead at the buffer's end.
function stalledMedia(buf = [[0, 111.9]]) {
    return { paused: false, seeking: false, ended: false, readyState: 2, currentTime: 111.87, buffered: ranges(buf) };
}

const STALL = { type: Hls.ErrorTypes.MEDIA_ERROR, details: Hls.ErrorDetails.BUFFER_STALLED_ERROR, fatal: false };
const frag = (sn, stats) => ({ sn, start: sn * 4, ...(stats ? { stats } : {}) });

// hls.js's LoadStats as xhr-loader keeps it: `first` set at the headers,
// `loaded` moved by every progress event. One object per request (a retry is
// a new loader with new stats: fragment-loader.ts frag.stats = loader.stats).
const loadStats = ({ headers = true, loaded = 0 } = {}) => ({
    loaded,
    total: 2500000,
    retry: 0,
    loading: { start: 1, first: headers ? 2 : 0, end: 0 },
});
// A fragment of the main controller whose request is receiving its body.
const receiving = (sn, stats = loadStats({ loaded: 82908 })) => ({ frag: frag(sn, stats), state: 'FRAG_LOADING' });

// A clock and an interval the test turns by hand.
function clock() {
    let t = 0;
    const timers = new Map();
    let id = 0;
    return {
        now: () => t,
        setInterval: (fn, ms) => {
            timers.set(++id, { fn, ms, next: t + ms });
            return id;
        },
        clearInterval: (i) => timers.delete(i),
        advance(ms) {
            const end = t + ms;
            for (;;) {
                let due = null;
                for (const [i, tm] of timers) if (tm.next <= end && (!due || tm.next < due[1].next)) due = [i, tm];
                if (!due) break;
                t = due[1].next;
                due[1].next += due[1].ms;
                due[1].fn();
            }
            t = end;
        },
        get running() {
            return timers.size;
        },
    };
}

function setup(media = stalledMedia()) {
    const hls = fakeHls(media);
    const c = clock();
    const logs = [];
    const w = createLoaderRestart(hls, Hls, { now: c.now, setInterval: c.setInterval, clearInterval: c.clearInterval, log: (m) => logs.push(m) });
    return { hls, c, w, logs, media };
}

test('hls.js reading of "in flight": a fragment in any state but IDLE/STOPPED/ENDED/ERROR', () => {
    const nothingInFlight = (x) => loadProgress(x).size === 0;
    assert.equal(nothingInFlight({}), true);
    assert.equal(nothingInFlight(undefined), true);
    assert.equal(nothingInFlight({ main: { frag: frag(27), state: 'FRAG_LOADING' } }), false);
    assert.equal(nothingInFlight({ main: { frag: frag(27), state: 'PARSING' } }), false);
    assert.equal(nothingInFlight({ main: { frag: frag(27), state: 'FRAG_LOADING_WAITING_RETRY' } }), false, 'a scheduled retry is hls.js at work');
    assert.equal(nothingInFlight({ main: { frag: frag(27), state: 'IDLE' } }), true, 'the last fragment, loaded');
    assert.equal(nothingInFlight({ main: { frag: frag(27), state: 'ERROR' } }), true);
    assert.equal(nothingInFlight({ main: { frag: null, state: 'WAITING_LEVEL' } }), true);
    assert.equal(nothingInFlight({ main: { frag: null, state: 'IDLE' }, audio: { frag: frag(3), state: 'FRAG_LOADING' } }), false);
});

// Only a request past its headers is judged by its bytes; everything else in
// flight is hls.js's own (its TTFB timer before the headers, its retry timer,
// parsing and appending).
test('receivingBody: FRAG_LOADING past the headers only', () => {
    const st = loadStats({ loaded: 82908 });
    assert.equal(receivingBody({ frag: frag(27, st), state: 'FRAG_LOADING' }), st);
    assert.equal(receivingBody({ frag: frag(27, loadStats({ headers: false })), state: 'FRAG_LOADING' }), null, 'waiting for headers: hls.js TTFB');
    assert.equal(receivingBody({ frag: frag(27), state: 'FRAG_LOADING' }), null, 'no stats: not judged');
    assert.equal(receivingBody({ frag: frag(27, st), state: 'PARSING' }), null);
    assert.equal(receivingBody({ frag: frag(27, st), state: 'FRAG_LOADING_WAITING_RETRY' }), null);
    assert.equal(receivingBody({ frag: null, state: 'FRAG_LOADING' }), null);
});

// The recorded failure: at the cap every stall is a segment still arriving.
// The old handler restarted the loader 5 s after the report -- stopLoad()
// aborted that segment and it was fetched again from byte 0.
test('a stall at the cap: the segment arriving is never aborted, however long it takes', () => {
    const { hls, c, w } = setup();
    // Its bytes keep coming, however slowly: 1 KB a second is ~8 kbit/s, far
    // below any cap -- a trickle is arriving all the same.
    const st = loadStats({ loaded: 82908 });
    hls.inFlightFragments.main = { frag: frag(27, st), state: 'FRAG_LOADING' };
    hls.trigger(Hls.Events.ERROR, STALL);
    assert.equal(w.armed, true);
    for (let i = 0; i < 120; i++) {
        c.advance(1000);
        st.loaded += 1024;
    }
    assert.equal(hls.startLoads, 0);
    // Waiting for its headers: hls.js's TTFB timer owns it, however long.
    hls.inFlightFragments.main = { frag: frag(27, loadStats({ headers: false })), state: 'FRAG_LOADING' };
    c.advance(60 * 1000);
    assert.equal(hls.startLoads, 0);
    // Alternate audio loading while the video's segment is parsed: still work.
    hls.inFlightFragments.main = { frag: frag(27), state: 'PARSING' };
    hls.inFlightFragments.audio = { frag: frag(28), state: 'FRAG_LOADING' };
    c.advance(30 * 1000);
    assert.equal(hls.startLoads, 0);
    // A retry hls.js has scheduled is its own recovery.
    hls.inFlightFragments.main = { frag: frag(27), state: 'FRAG_LOADING_WAITING_RETRY' };
    hls.inFlightFragments.audio = { frag: null, state: 'IDLE' };
    c.advance(30 * 1000);
    assert.equal(hls.startLoads, 0);
});

// Between two segments the controllers are IDLE for a tick; appends change
// the buffer. Neither is a stopped loader.
test('segments arriving one after another: no restart between them', () => {
    const { hls, c, media } = setup();
    hls.trigger(Hls.Events.ERROR, STALL);
    for (let i = 0; i < 20; i++) {
        c.advance(1000);
        // Sampled while idle, but the buffer grew since the last sample.
        media.buffered = ranges([[0, 112 + i]]);
    }
    assert.equal(hls.startLoads, 0);
});

// What the restart is still for: the playhead starving, nothing on its way
// in any controller, the buffer unchanged -- for RESTART_AFTER_MS straight.
test('the loader stopped with nothing in flight: restarted after RESTART_AFTER_MS, not before', () => {
    const { hls, c, w, logs } = setup();
    hls.inFlightFragments.main = { frag: frag(26), state: 'ERROR' };
    hls.trigger(Hls.Events.ERROR, STALL);
    c.advance(RESTART_AFTER_MS - 1000);
    assert.equal(hls.startLoads, 0, 'not before');
    c.advance(1000);
    assert.equal(hls.startLoads, 1);
    assert.equal(w.kicks, 1);
    assert.match(logs[0], /restarting the loader/);
    // It loads again: no second restart while that fragment is on its way.
    hls.inFlightFragments.main = { frag: frag(27), state: 'FRAG_LOADING' };
    c.advance(30 * 1000);
    assert.equal(hls.startLoads, 1);
    // Still nothing after it: once more, RESTART_AFTER_MS later, not sooner.
    hls.inFlightFragments.main = { frag: frag(27), state: 'IDLE' };
    c.advance(RESTART_AFTER_MS - 1000);
    assert.equal(hls.startLoads, 1);
    c.advance(1000);
    assert.equal(hls.startLoads, 2);
});

test('the quiet must be unbroken: a fragment in flight or an append in between starts it over', () => {
    const { hls, c, media } = setup();
    hls.trigger(Hls.Events.ERROR, STALL);
    c.advance(3000);
    hls.inFlightFragments.main = { frag: frag(27), state: 'FRAG_LOADING' };
    c.advance(1000);
    hls.inFlightFragments.main = { frag: frag(27), state: 'IDLE' };
    c.advance(RESTART_AFTER_MS - 1000);
    assert.equal(hls.startLoads, 0, 'the load in between counted');
    // Seen by the next check (1 s on), and counted from there.
    media.buffered = ranges([[0, 115.9]]);
    c.advance(RESTART_AFTER_MS);
    assert.equal(hls.startLoads, 0, 'the append in between counted');
    c.advance(1000);
    assert.equal(hls.startLoads, 1);
});

// The hung request (finding hung-inflight-no-recovery): headers in, 30% of the
// body, then not one byte more. hls.js's own way out is its 120 s load
// timeout; the old startLoad() had it playing again 5 s after the report.
// Restarted after NO_BYTES_MS without a byte, not before.
test('a request that stopped receiving bytes: restarted after NO_BYTES_MS without a byte', () => {
    const { hls, c, w, logs } = setup();
    hls.inFlightFragments.main = receiving(27);
    hls.trigger(Hls.Events.ERROR, STALL);
    c.advance(NO_BYTES_MS - 1000);
    assert.equal(hls.startLoads, 0, 'not before');
    c.advance(1000);
    assert.equal(hls.startLoads, 1);
    assert.equal(w.kicks, 1);
    assert.match(logs[0], /no bytes for 10 s/);
    // startLoad() asked again: a new request (new stats), its bytes coming.
    const st = loadStats({ loaded: 0 });
    hls.inFlightFragments.main = { frag: frag(27, st), state: 'FRAG_LOADING' };
    for (let i = 0; i < 30; i++) {
        c.advance(1000);
        st.loaded += 60000;
    }
    assert.equal(hls.startLoads, 1, 'the new request is arriving');
});

// Gaps a real cap never makes (the limiter's are about a second) but a lossy
// path can (TCP backoff): longer than the old 5 s, shorter than NO_BYTES_MS.
// The segment is still arriving; aborting it would throw the bytes away.
test('bytes with gaps under NO_BYTES_MS: the segment is never aborted', () => {
    const { hls, c } = setup();
    const st = loadStats({ loaded: 82908 });
    hls.inFlightFragments.main = { frag: frag(27, st), state: 'FRAG_LOADING' };
    hls.trigger(Hls.Events.ERROR, STALL);
    for (let i = 0; i < 12; i++) {
        c.advance(8000);
        st.loaded += 32768;
    }
    assert.equal(hls.startLoads, 0);
});

// hls.js's own retry (its TTFB or load timeout) is a new request: new stats
// at the same byte count is still a new start, not the dead one going on.
test('hls.js retried the request: counted from the new one', () => {
    const { hls, c } = setup();
    hls.inFlightFragments.main = receiving(27, loadStats({ loaded: 0 }));
    hls.trigger(Hls.Events.ERROR, STALL);
    c.advance(NO_BYTES_MS - 2000);
    // Retried, the new request past its headers and at 0 bytes as well.
    hls.inFlightFragments.main = receiving(27, loadStats({ loaded: 0 }));
    c.advance(2000);
    assert.equal(hls.startLoads, 0, 'the retry started the count over');
    c.advance(NO_BYTES_MS - 2000);
    assert.equal(hls.startLoads, 0);
    c.advance(1000);
    assert.equal(hls.startLoads, 1);
});

// Alternate audio loads up to one fragment ahead of main's (audio-stream-
// controller atBufferSyncLimit), so with main hung it goes idle soon; while
// its bytes move it is work, and the count starts once it stops.
test('main hung, audio still arriving: counted once every load is without bytes', () => {
    const { hls, c } = setup();
    hls.inFlightFragments.main = receiving(27);
    const audio = loadStats({ loaded: 1000 });
    hls.inFlightFragments.audio = { frag: frag(28, audio), state: 'FRAG_LOADING' };
    hls.trigger(Hls.Events.ERROR, STALL);
    for (let i = 0; i < 15; i++) {
        c.advance(1000);
        audio.loaded += 30000;
    }
    assert.equal(hls.startLoads, 0, 'audio arriving');
    hls.inFlightFragments.audio = { frag: frag(28, audio), state: 'IDLE' };
    c.advance(NO_BYTES_MS - 1000);
    assert.equal(hls.startLoads, 0);
    c.advance(1000);
    assert.equal(hls.startLoads, 1);
});

// hls.js reports a stall once and resolves it on playing / seeked / ended
// (gap-controller); a pause, a finished film or a session seek's reload is
// no stall either.
test('resolved, paused, reloaded, detached, destroyed: disarmed, nothing restarted', () => {
    for (const [name, end] of [
        ['STALL_RESOLVED', (s) => s.hls.trigger(Hls.Events.STALL_RESOLVED, {})],
        ['MANIFEST_LOADING', (s) => s.hls.trigger(Hls.Events.MANIFEST_LOADING, { url: 'x' })],
        ['MEDIA_DETACHING', (s) => s.hls.trigger(Hls.Events.MEDIA_DETACHING, {})],
        ['pause', (s) => { s.media.paused = true; }],
        ['ended', (s) => { s.media.ended = true; }],
        ['DESTROYING', (s) => s.hls.trigger(Hls.Events.DESTROYING, {})],
    ]) {
        const s = setup();
        s.hls.trigger(Hls.Events.ERROR, STALL);
        end(s);
        s.c.advance(30 * 1000);
        assert.equal(s.hls.startLoads, 0, name);
        assert.equal(s.w.armed, false, name);
        assert.equal(s.c.running, 0, `${name}: no timer left`);
    }
    // Destroyed: every listener is gone with it.
    const s = setup();
    s.hls.trigger(Hls.Events.DESTROYING, {});
    for (const ev of [Hls.Events.ERROR, Hls.Events.STALL_RESOLVED, Hls.Events.MANIFEST_LOADING, Hls.Events.MEDIA_DETACHING, Hls.Events.DESTROYING]) {
        assert.equal(s.hls.listeners(ev), 0, ev);
    }
});

test('not starving (data ahead of the playhead) or seeking: no restart', () => {
    const s = setup();
    s.hls.trigger(Hls.Events.ERROR, STALL);
    s.media.readyState = 4;
    s.c.advance(30 * 1000);
    assert.equal(s.hls.startLoads, 0, 'HAVE_ENOUGH_DATA');
    s.media.readyState = 2;
    s.media.seeking = true;
    s.c.advance(30 * 1000);
    assert.equal(s.hls.startLoads, 0, 'seeking');
});

test('only a non-fatal bufferStalledError arms it', () => {
    for (const data of [
        { ...STALL, fatal: true },
        { type: Hls.ErrorTypes.MEDIA_ERROR, details: Hls.ErrorDetails.BUFFER_NUDGE_ON_STALL, fatal: false },
        { type: Hls.ErrorTypes.NETWORK_ERROR, details: Hls.ErrorDetails.FRAG_LOAD_TIMEOUT, fatal: false },
    ]) {
        const s = setup();
        s.hls.trigger(Hls.Events.ERROR, data);
        assert.equal(s.w.armed, false, data.details);
        s.c.advance(30 * 1000);
        assert.equal(s.hls.startLoads, 0, data.details);
    }
});

// The player's own wiring (hls-manager setupHlsEvents), on the real timers:
// the report of a stall at the cap does not end in a startLoad() -- which
// the 5 s setTimeout it replaced did, every time.
test('hls-manager: a stall with its segment arriving schedules no loader restart', (t) => {
    t.mock.timers.enable({ apis: ['setTimeout', 'setInterval', 'Date'], now: 0 });
    const hls = fakeHls(stalledMedia());
    hls.inFlightFragments.main = { frag: frag(27), state: 'FRAG_LOADING' };
    const warn = console.warn;
    console.warn = () => {};
    try {
        setupHlsEvents(hls);
        hls.trigger(Hls.Events.ERROR, STALL);
        t.mock.timers.tick(20 * 1000);
        assert.equal(hls.startLoads, 0);
        // Nothing in flight any more and still starving: the way out stays.
        hls.inFlightFragments.main = { frag: frag(27), state: 'ERROR' };
        t.mock.timers.tick(RESTART_AFTER_MS + 1000);
        assert.equal(hls.startLoads, 1);
        // The next request hangs after its headers: out NO_BYTES_MS later.
        // (The mocked Date reads the end of a tick in every callback of that
        // tick, so the clock goes a check at a time.)
        hls.inFlightFragments.main = receiving(27);
        for (let i = 0; i < NO_BYTES_MS / 1000; i++) t.mock.timers.tick(1000);
        assert.equal(hls.startLoads, 1, 'not before');
        t.mock.timers.tick(1000);
        assert.equal(hls.startLoads, 2);
        hls.trigger(Hls.Events.DESTROYING, {});
    } finally {
        console.warn = warn;
    }
});
