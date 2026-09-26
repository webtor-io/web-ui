import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';

// The page's <video> as real Chrome drove it at the plan's cap (2026-09-26,
// transcoder HLS at 5 Mbps, __fixtures__/player-stall-trace.json), replayed
// event by event, on the recorded clock, through the listener the page runs.
// The ground truth is not the events: it is the recorder's 500 ms samples of
// the element -- playing, currentTime frozen. Chrome ends every stall's
// `waiting` with one more `timeupdate` ~250 ms later at the same
// currentTime; taken as playback it closed each stall under STALL_MIN_MS, and
// all 30 real stalls of these runs (2.5-215 s) read 'playing' -- the owner's
// "no upsell while the video really stalls" (2026-09-25).

const {
    createPlayerActivity, STALL_MIN_MS,
} = await import('./playerActivity.js');

const FX = JSON.parse(readFileSync(new URL('./__fixtures__/player-stall-trace.json', import.meta.url), 'utf8'));

// replay feeds a run's events into a page with one <video>, the way the
// browser does: the element's state first, then the event on it. At every
// `every` ms of the recorded clock, and on every event, it asks the verdict
// and hands it to probe(t, state, activity).
function replay(run, { every = 250, graceSec = 0, probe }) {
    const dom = new JSDOM('<!doctype html><body><video></video></body>');
    const doc = dom.window.document;
    const video = doc.querySelector('video');
    if (graceSec) video.dataset.graceDurationSec = String(graceSec);
    const state = { currentTime: 0, readyState: 0, paused: true, seeking: false, ended: false, buf: 0 };
    for (const k of ['currentTime', 'readyState', 'paused', 'seeking', 'ended']) {
        Object.defineProperty(video, k, { configurable: true, get: () => state[k] });
    }
    Object.defineProperty(video, 'buffered', {
        configurable: true, get: () => ({ length: state.buf > 0 ? 1 : 0, end: () => state.buf }),
    });
    let clock = 0;
    const activity = createPlayerActivity(doc, { now: () => clock });
    const offsets = (run.runOffset || []).slice();
    let next = 0;
    const sampleUntil = (t) => {
        for (; next <= t; next += every) {
            clock = next;
            probe(next, activity.state(), activity);
        }
    };
    for (const [t, type, ct, rs, paused, seeking, ended, buf] of run.events) {
        sampleUntil(t - 1);
        clock = t;
        while (offsets.length && offsets[0][0] <= t) video.dataset.runOffset = String(offsets.shift()[1]);
        Object.assign(state, { currentTime: ct, readyState: rs, paused: !!paused, seeking: !!seeking, ended: !!ended, buf });
        video.dispatchEvent(new dom.window.Event(type, { bubbles: false }));
        probe(t, activity.state(), activity);
    }
    sampleUntil(run.endMs);
    activity.stop();
}

// The first `playing` of a run: before it the player is starting, not
// playing or stalling.
const firstPlaying = (run) => run.events.find((e) => e[1] === 'playing')[0];

for (const [name, run] of Object.entries(FX.runs)) {
    test(`recorded ${name}: buffering through every real stall, never before the first`, () => {
        const start = firstPlaying(run);
        const firstStall = run.stalls.length ? run.stalls[0][0] : Infinity;
        const wrong = [];
        const caught = new Set();
        replay(run, {
            probe(t, s) {
                if (t < start) return;
                // Inside a stall, once it has lasted: the sample before the
                // freeze may lead the `waiting` by one sample period (500 ms).
                const i = run.stalls.findIndex(([a, b]) => t >= a + 500 + STALL_MIN_MS && t <= b);
                if (i >= 0) {
                    if (s === 'buffering') caught.add(i);
                    else wrong.push(`${(t / 1000).toFixed(2)} s: ${s} in the stall ${i + 1} (${run.stalls[i].map((x) => (x / 1000).toFixed(1)).join('-')} s)`);
                }
                // Before the first real stall -- the grace buffer draining, a
                // session seek, the restart after it: playing, not buffering.
                if (t < firstStall && s !== 'playing') wrong.push(`${(t / 1000).toFixed(2)} s: ${s} before any stall`);
            },
        });
        assert.deepEqual(wrong.slice(0, 5), [], `${wrong.length} wrong verdicts`);
        assert.equal(caught.size, run.stalls.length, `${caught.size} of ${run.stalls.length} stalls read as buffering`);
    });
}

test('the recorded runs are the ones described: stalls where the cap bound, none under it', () => {
    assert.equal(FX.runs.grace_end_at_cap.stalls.length, 3);
    assert.equal(FX.runs.session_seek_past_grace.stalls.length, 27);
    assert.equal(FX.runs.fits_cap_no_stall.stalls.length, 0);
    // Chrome's tick: the `timeupdate` right after a stall's `waiting`, the
    // clock where it stopped. Pinned so a re-recording shows whether the
    // engine still sends it.
    let ticks = 0;
    let waits = 0;
    for (const run of [FX.runs.grace_end_at_cap, FX.runs.session_seek_past_grace]) {
        run.events.forEach((e, i) => {
            if (e[1] !== 'waiting' || e[3] === 0) return;
            waits++;
            const after = run.events.slice(i + 1).find((x) => x[1] === 'timeupdate' || x[1] === 'playing');
            if (after && after[1] === 'timeupdate' && after[2] === e[2] && after[0] - e[0] < 400) ticks++;
        });
    }
    assert.equal(waits, 30, 'the stalls\' waits (the restart after the seek waits at readyState 0)');
    assert.equal(ticks, 30, 'each followed by a timeupdate that did not move the clock');
});

// After a session seek currentTime counts from the seek point: the player at
// 24:30 of the film read 0:06. Its movie time is currentTime plus the
// session's offset (data-run-offset), and that is what the grace window is
// measured in -- here the 20-minute window ends before the seek's target.
test('recorded session seek past grace: inGrace by movie time, not by currentTime', () => {
    const run = FX.runs.session_seek_past_grace;
    const seekAt = run.runOffset[0][0];
    const seen = { before: new Set(), after: new Set() };
    replay(run, {
        graceSec: run.graceSec,
        probe(t, s, activity) {
            if (t < firstPlaying(run)) return;
            seen[t < seekAt ? 'before' : 'after'].add(activity.inGrace());
        },
    });
    assert.deepEqual([...seen.before], [true], 'the first 44 s of the film: inside the window');
    assert.deepEqual([...seen.after], [false], 'from 24:30 on: past it');
    // Without the offset (the element's own clock) it read inside the window
    // for the whole run.
    const noOffset = { ...run, runOffset: [] };
    const stale = new Set();
    replay(noOffset, {
        graceSec: run.graceSec,
        probe(t, s, activity) {
            if (t >= seekAt) stale.add(activity.inGrace());
        },
    });
    assert.deepEqual([...stale], [true], 'the element\'s own clock after the seek: 0-110 s');
});
