import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseProgress, withRev, pollProgress, POLL_TIMEOUT_MS } from './subtitle-progress.js';

test('parseProgress', () => {
    assert.deepEqual(parseProgress('12/48'), { done: 12, total: 48, final: false, live: false });
    assert.deepEqual(parseProgress('100/100'), { done: 100, total: 100, final: true, live: false });
    // 0/0 is "the job has not counted the cues yet", not "done".
    assert.deepEqual(parseProgress('0/0'), { done: 0, total: 0, final: false, live: false });
    assert.deepEqual(parseProgress(null), { done: 0, total: 0, final: false, live: false });
});

test('parseProgress with live never reports final', () => {
    assert.deepEqual(parseProgress('7/7', true), { done: 7, total: 7, final: false, live: true });
    assert.deepEqual(parseProgress('7/7', false), { done: 7, total: 7, final: true, live: false });
    assert.deepEqual(parseProgress('7/7'), { done: 7, total: 7, final: true, live: false });
});

test('withRev appends or replaces rev', () => {
    assert.equal(withRev('https://x/a.vtt?token=T', 3), 'https://x/a.vtt?token=T&rev=3');
    assert.equal(withRev('https://x/a.vtt?token=T&rev=3', 4), 'https://x/a.vtt?token=T&rev=4');
    assert.equal(withRev('https://x/a.vtt', 1), 'https://x/a.vtt?rev=1');
    // Protocol-relative and relative forms keep their shape: rewriting
    // them as https:// or /path would point the track somewhere else.
    assert.equal(withRev('//cdn/a.vtt?token=T', 2), '//cdn/a.vtt?token=T&rev=2');
    assert.equal(withRev('/ext/a.vtt?token=T', 2), '/ext/a.vtt?token=T&rev=2');
    assert.equal(withRev('sub/a.vtt?token=T', 2), 'sub/a.vtt?token=T&rev=2');
});

test('pollProgress reports changes and stops when final', async () => {
    const answers = ['3/10', '3/10', '10/10'];
    let i = 0;
    const fetchImpl = async () => ({ status: 200, headers: { get: () => answers[Math.min(i++, answers.length - 1)] } });
    const seen = [];
    let done = false;
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onProgress: (p) => seen.push(p.done), onDone: () => { done = true; } });
    await new Promise((r) => setTimeout(r, 30));
    stop();
    assert.deepEqual(seen, [3, 10]);
    assert.equal(done, true);
});

test('pollProgress keeps polling while X-Subtitle-Live is set', async () => {
    const answers = [['1/1', '1'], ['1/1', '1'], ['2/2', null]];
    let i = 0;
    const fetchImpl = async () => {
        const [p, live] = answers[Math.min(i++, answers.length - 1)];
        return { status: 200, headers: { get: (h) => (h === 'X-Subtitle-Live' ? live : p) } };
    };
    const seen = [];
    let done = false;
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onProgress: (p) => seen.push([p.done, p.live]), onDone: () => { done = true; } });
    await new Promise((r) => setTimeout(r, 30));
    stop();
    assert.deepEqual(seen, [[1, true], [2, false]]);
    assert.equal(done, true);
});

test('pollProgress stops on a non-200', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 502, headers: { get: () => null } }; };
    let err = null;
    pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onError: (s) => { err = s; } });
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(err, 502);
    assert.equal(calls, 1);
});

test('pollProgress keeps polling while the header says 0/0', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '0/0' } }; };
    let done = false;
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onDone: () => { done = true; } });
    await new Promise((r) => setTimeout(r, 20));
    stop();
    assert.equal(done, false);
    assert.ok(calls > 1, `expected repeated polls, got ${calls}`);
});

test('stop() ends the poll', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '1/10' } }; };
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1 });
    await new Promise((r) => setTimeout(r, 10));
    stop();
    const after = calls;
    await new Promise((r) => setTimeout(r, 10));
    assert.equal(calls, after);
});

test('pollProgress gives up after the timeout and reports it', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '1/10' } }; };
    let err = null;
    pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, timeoutMs: 5, onError: (c) => { err = c; } });
    await new Promise((r) => setTimeout(r, 30));
    assert.equal(err, 'timeout');
    const after = calls;
    await new Promise((r) => setTimeout(r, 10));
    assert.equal(calls, after, 'polling must stop at the deadline');
});

test('the default timeout is 15 minutes', () => {
    assert.equal(POLL_TIMEOUT_MS, 15 * 60 * 1000);
});

// --- the cap against a live source ------------------------------------
//
// A live run ends when the transcode does, i.e. after the whole film, so
// an absolute cap would kill every feature-length translation. The cap is
// therefore an inactivity cap while live. The two tests below are the
// pair: the first says a run that keeps producing cues outlives the cap,
// the second says a run that only keeps *answering* does not.

test('a live run that keeps producing cues outlives the timeout', async () => {
    let n = 0;
    const fetchImpl = async () => {
        n++;
        const live = n < 40;
        const header = live ? `${n}/${n}` : '40/40';
        return { status: 200, headers: { get: (h) => (h === 'X-Subtitle-Live' ? (live ? '1' : null) : header) } };
    };
    let err = null;
    let done = false;
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 1, timeoutMs: 25,
        onError: (c) => { err = c; },
        onDone: () => { done = true; },
    });
    await new Promise((r) => setTimeout(r, 400));
    stop();
    // 40 ticks at ~1 ms each span several 25 ms windows; each one moved
    // the deadline because `done` moved with it.
    assert.equal(err, null, 'a run that is demonstrably producing cues is not a timeout');
    assert.equal(done, true, 'and it finishes when the source stops being live');
    assert.ok(n >= 40, `expected the run to reach the end, got ${n} polls`);
});

test('a wedged live run still times out', async () => {
    // Negative control for the rule above: the same live header, forever,
    // with a count that never moves. Answering is not progress.
    let calls = 0;
    const fetchImpl = async () => {
        calls++;
        return { status: 200, headers: { get: (h) => (h === 'X-Subtitle-Live' ? '1' : '3/9') } };
    };
    let err = null;
    pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, timeoutMs: 20, onError: (c) => { err = c; } });
    await new Promise((r) => setTimeout(r, 120));
    assert.equal(err, 'timeout');
    const after = calls;
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, after, 'polling must stop at the deadline');
});

test('a live tick that brings no new cue does not extend the cap', async () => {
    // The `p.done !== last` half of the rule, on its own. A source that
    // flips to live at a count that has not moved is a change worth
    // reporting (the chip swaps a percent for a count) but not progress,
    // so it must not buy another window. Measured rather than asserted on
    // a flag: both variants time out, the difference is when.
    const t0 = Date.now();
    const fetchImpl = async () => ({
        status: 200,
        headers: { get: (h) => (h === 'X-Subtitle-Live' ? (Date.now() - t0 >= 200 ? '1' : null) : '3/9') },
    });
    let at = 0;
    pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 5, timeoutMs: 300,
        onError: (c) => { if (c === 'timeout' && !at) at = Date.now() - t0; },
    });
    await new Promise((r) => setTimeout(r, 700));
    assert.ok(at > 0, 'the wedged run must still time out');
    assert.ok(at < 400, `the live flip must not have bought a second window (timed out at ${at}ms)`);
});

// --- sleeping with the video ------------------------------------------

test('suspend() stops the polling without reporting anything, resume() picks it up', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '1/10' } }; };
    const seen = [];
    let err = null;
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 1,
        onProgress: (p) => seen.push(p.done),
        onError: (c) => { err = c; },
    });
    await new Promise((r) => setTimeout(r, 20));
    assert.ok(calls > 1, `expected the run to be polling, got ${calls}`);
    stop.suspend();
    // Past the in-flight request too: a fetch that lands after suspend()
    // must not schedule the next tick.
    await new Promise((r) => setTimeout(r, 20));
    const asleep = calls;
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, asleep, 'a suspended run polls nothing');
    assert.equal(err, null, 'suspending is not an error — the chip keeps its count');

    stop.resume();
    await new Promise((r) => setTimeout(r, 20));
    assert.ok(calls > asleep, 'resume() wakes the same run');
    stop();
});

test('a suspended run does not spend its deadline asleep', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '1/10' } }; };
    let err = null;
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, timeoutMs: 40, onError: (c) => { err = c; } });
    await new Promise((r) => setTimeout(r, 10));
    stop.suspend();
    // Longer than the whole cap: a viewer who pauses for an hour and comes
    // back must not be told the translation timed out.
    await new Promise((r) => setTimeout(r, 60));
    stop.resume();
    await new Promise((r) => setTimeout(r, 10));
    assert.equal(err, null, 'the sleep did not count against the cap');
    stop();
});

test('resume() after stop() stays stopped', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '1/10' } }; };
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1 });
    await new Promise((r) => setTimeout(r, 10));
    stop();
    const after = calls;
    stop.resume();
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, after, 'a stopped run is gone, not asleep');
});
