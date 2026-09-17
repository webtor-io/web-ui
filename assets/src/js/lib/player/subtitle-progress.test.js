import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseProgress, withRev, pollProgress, progressText, POLL_TIMEOUT_MS, QUEUE_HINT_MS } from './subtitle-progress.js';

// headers builds the HEAD response the translate service answers with.
// `status` is X-Subtitle-Status, absent unless a case sets it.
const answer = (progress, { live = false, status = null, pendingFrom = null, sessionOffset = null } = {}) => ({
    status: 200,
    headers: {
        get: (n) => {
            if (n === 'X-Subtitle-Progress') return progress;
            if (n === 'X-Subtitle-Live') return live ? '1' : null;
            if (n === 'X-Subtitle-Status') return status;
            if (n === 'X-Subtitle-Pending-From') return pendingFrom;
            if (n === 'X-Subtitle-Session-Offset') return sessionOffset;
            return null;
        },
    },
});

test('parseProgress', () => {
    assert.deepEqual(parseProgress('12/48'), { done: 12, total: 48, final: false, live: false, pendingFrom: null });
    assert.deepEqual(parseProgress('100/100'), { done: 100, total: 100, final: true, live: false, pendingFrom: null });
    // 0/0 is "the job has not counted the cues yet", not "done".
    assert.deepEqual(parseProgress('0/0'), { done: 0, total: 0, final: false, live: false, pendingFrom: null });
    assert.deepEqual(parseProgress(null), { done: 0, total: 0, final: false, live: false, pendingFrom: null });
});

test('parseProgress with live never reports final', () => {
    assert.deepEqual(parseProgress('7/7', true), { done: 7, total: 7, final: false, live: true, pendingFrom: null });
    assert.deepEqual(parseProgress('7/7', false), { done: 7, total: 7, final: true, live: false, pendingFrom: null });
    assert.deepEqual(parseProgress('7/7'), { done: 7, total: 7, final: true, live: false, pendingFrom: null });
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

// ---- kick(): a session seek wants its cues now ------------------------
//
// Without it, the first cues for a seeked-to position wait out whatever is
// left of the 3 s poll interval on top of the service's own translation
// lag, and then the caller's own reload throttle on top of that. kick()
// only owns the first half (an immediate tick); the second half is a mark
// on the report that change lands on, which the caller (Player.jsx) reads
// as permission to bypass its own throttle for that one reload.

test('kick() ticks at once instead of waiting out the interval, and marks the change it finds', async () => {
    let header = '3/10';
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => header } }; };
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 200, onProgress: (p) => seen.push(p) });
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, 1, 'one tick at start');
    assert.deepEqual(seen.map((p) => p.done), [3]);

    // The service has produced more by the time the seek lands.
    header = '7/10';
    stop.kick();
    await new Promise((r) => setTimeout(r, 20));
    stop();

    assert.ok(calls >= 2, `kick() must tick at once rather than wait out the 200 ms interval, got ${calls} calls in ~20 ms`);
    assert.deepEqual(seen.map((p) => p.done), [3, 7], 'the kicked tick reports the new count');
    assert.equal(seen[0].forceReload, undefined, 'only the report a kick produced is marked');
    assert.equal(seen[1].forceReload, true, 'so the caller can bypass its own reload throttle once');
});

test('a kick that finds nothing new yet still marks the change that follows', async () => {
    // The immediate tick usually lands before the service has caught up —
    // the mark must survive to whichever later tick brings the first real
    // change, not just the one kick() itself triggered.
    let header = '3/10';
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => header } }; };
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 5, onProgress: (p) => seen.push(p) });
    await new Promise((r) => setTimeout(r, 10));
    stop.kick();
    // Several unchanged ticks pass before the count actually moves.
    await new Promise((r) => setTimeout(r, 15));
    header = '6/10';
    await new Promise((r) => setTimeout(r, 20));
    stop();

    assert.deepEqual(seen.map((p) => p.done), [3, 6], 'no report for the unchanged ticks in between');
    assert.equal(seen[1].forceReload, true, 'the mark waited for an actual change to land on');
});

test('a frontier move after a kick does not spend the mark: the first new count does', async () => {
    // After a session seek the service's X-Subtitle-Pending-From moves
    // (same cues, a new run window) before any new cue is translated. That
    // is a change onProgress reports, but not one that changes the track's
    // revision — a mark spent on it would leave the seek's first real cues
    // waiting out the reload throttle after all.
    let progress = '3/10';
    let pending = '100.000';
    const fetchImpl = async () => ({ status: 200, headers: { get: (k) => (k === 'X-Subtitle-Pending-From' ? pending : progress) } });
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 5, onProgress: (p) => seen.push(p) });
    await new Promise((r) => setTimeout(r, 10));
    stop.kick();
    pending = '40.000';
    await new Promise((r) => setTimeout(r, 15));
    progress = '6/10';
    await new Promise((r) => setTimeout(r, 20));
    stop();

    assert.deepEqual(seen.map((p) => [p.done, p.pendingFrom]), [[3, 100], [3, 40], [6, 40]]);
    assert.equal(seen[1].forceReload, undefined, 'the frontier move alone must not spend the mark');
    assert.equal(seen[2].forceReload, true, 'the first count change after the kick carries it');
});

test('kick() does nothing once the run is stopped or asleep', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '1/10' } }; };
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 200 });
    await new Promise((r) => setTimeout(r, 10));

    stop.suspend();
    const asleep = calls;
    stop.kick();
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, asleep, 'kick() must not wake a suspended run — that defeats the reason it is asleep');

    stop.resume();
    await new Promise((r) => setTimeout(r, 10));
    stop();
    const after = calls;
    stop.kick();
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, after, 'kick() on a stopped run polls nothing');
});

// The negative control for the module's fourth, implicit state:
// "finished, not stopped". The three terminal returns used to leave both
// `stopped` and `suspended` false, so nothing here prevented a later
// suspend()/resume() from re-arming a run that had already reported. The
// module's own suite drives suspend()/resume() directly, and the player's
// pause and visibility handlers reach a live stop ref, so this was a
// caller-side invariant rather than a module guarantee.
test('a run that reported done is terminal: resume() polls nothing', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '10/10' } }; };
    let done = 0;
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onDone: () => { done++; } });
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(done, 1);
    const after = calls;

    stop.suspend();
    stop.resume();
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, after, 'a finished run is gone, not asleep');
    assert.equal(done, 1, 'and it does not report a second time');
});

test('a run that reported an error is terminal too, timeout included', async () => {
    for (const [what, fetchImpl, want] of [
        ['a non-200', async () => ({ status: 500, headers: { get: () => null } }), 500],
        ['a throw', async () => { throw new Error('offline'); }, 0],
    ]) {
        let calls = 0;
        const counted = async (...a) => { calls++; return fetchImpl(...a); };
        const errs = [];
        const stop = pollProgress('https://x/a.vtt', { fetchImpl: counted, intervalMs: 1, onError: (c) => errs.push(c) });
        await new Promise((r) => setTimeout(r, 20));
        assert.deepEqual(errs, [want], what);
        const after = calls;
        stop.suspend();
        stop.resume();
        await new Promise((r) => setTimeout(r, 20));
        assert.equal(calls, after, `${what}: resume() must not re-poll a failed run`);
        assert.deepEqual(errs, [want], `${what}: and must not report twice`);
    }

    // The timeout path is the one that reported twice: its branch emits
    // subtitle-translate-error {code:'timeout'} on every tick it is
    // reached on.
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 200, headers: { get: () => '1/10' } }; };
    const errs = [];
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, timeoutMs: 15, onError: (c) => errs.push(c) });
    await new Promise((r) => setTimeout(r, 40));
    assert.deepEqual(errs, ['timeout']);
    const after = calls;
    stop.suspend();
    stop.resume();
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, after, 'a timed-out run must not poll again');
    assert.deepEqual(errs, ['timeout'], 'and must not emit a second timeout');
});

// ---- X-Subtitle-Status ------------------------------------------------
//
// The service's own verdict on a live run. It answers two questions the
// counts cannot: a live run that finished (the source ended and everything
// in it is translated, even with no final artifact to cache) looks exactly
// like one that is merely between playlist segments, and a run that was
// stopped incomplete (source_gone, too_large) looks exactly like one that
// is behind.

test('parseProgress: status done is final even while the source is live', () => {
    assert.deepEqual(parseProgress('7/9', true, 'done'), { done: 7, total: 9, final: true, live: true, pendingFrom: null });
    assert.deepEqual(parseProgress('7/9', false, 'done'), { done: 7, total: 9, final: true, live: false, pendingFrom: null });
    // Without it the live rule stands: done == total is "caught up for now".
    assert.deepEqual(parseProgress('9/9', true), { done: 9, total: 9, final: false, live: true, pendingFrom: null });
    // Case and whitespace are the wire's, not ours.
    assert.equal(parseProgress('7/9', true, ' DONE ').final, true);
    // A missing or unparseable count with a done verdict is still final:
    // the run is over, there is simply nothing to show for it.
    assert.deepEqual(parseProgress(null, true, 'done'), { done: 0, total: 0, final: true, live: true, pendingFrom: null });
    // Anything else leaves the counts in charge.
    assert.equal(parseProgress('7/9', true, 'stopped').final, false);
    assert.equal(parseProgress('7/9', true, 'running').final, false);
});

test('pollProgress stops a live run on status done, count or no count', async (t) => {
    // Stopped in t.after, not after the assertions: a poll left running by
    // a failed assertion keeps the event loop alive and wedges the whole
    // file instead of reporting the failure. Found by this test's own
    // negative control.
    const running = [];
    t.after(() => running.forEach((s) => s()));
    for (const [header, want] of [['9/40', 9], ['0/0', 0]]) {
        let calls = 0;
        const fetchImpl = async () => { calls++; return answer(header, { live: true, status: 'done' }); };
        let done = null;
        const errs = [];
        running.push(pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onDone: (p) => { done = p; }, onError: (c) => errs.push(c) }));
        await new Promise((r) => setTimeout(r, 20));
        assert.ok(done, `${header}: the run must finish`);
        assert.equal(done.done, want);
        assert.deepEqual(errs, [], 'finishing is not failing');
        assert.equal(calls, 1, 'and it polls no further');
    }
});

test('pollProgress reports status stopped once, after the last count', async (t) => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return answer('11/40', { live: true, status: 'stopped' }); };
    const seen = [];
    const errs = [];
    let done = 0;
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 1,
        onProgress: (p) => seen.push(p.done),
        onDone: () => { done++; },
        onError: (c) => errs.push(c),
    });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 20));
    // The counts in a stopped response are the last ones there will be, so
    // the chip is painted with them before the run is reported dead.
    assert.deepEqual(seen, [11]);
    assert.deepEqual(errs, ['stopped']);
    assert.equal(done, 0, 'stopped is not done');
    const after = calls;

    // Terminal, like every other error path.
    stop.suspend();
    stop.resume();
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(calls, after, 'a stopped run must not poll again');
    assert.deepEqual(errs, ['stopped'], 'and must not report twice');
});

// Negative control for the whole feature: a service that sends no
// X-Subtitle-Status (every deployment before it shipped, and every batch
// run) must behave exactly as before — the live rule keeps polling and the
// batch rule still finishes on the counts.
test('without the header the counts decide alone', async (t) => {
    let calls = 0;
    const live = async () => { calls++; return answer('9/9', { live: true }); };
    let done = 0;
    const errs = [];
    const stop = pollProgress('https://x/a.vtt', { fetchImpl: live, intervalMs: 1, onDone: () => { done++; }, onError: (c) => errs.push(c) });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 20));
    stop();
    assert.equal(done, 0, 'a live run is never final on its counts');
    assert.deepEqual(errs, []);
    assert.ok(calls > 1, `expected repeated polls, got ${calls}`);

    let doneBatch = 0;
    pollProgress('https://x/a.vtt', { fetchImpl: async () => answer('9/9'), intervalMs: 1, onDone: () => { doneBatch++; } });
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(doneBatch, 1, 'a batch run still finishes on done >= total');
});

// ---- the queue hint ---------------------------------------------------
//
// `0/0` covers two very different situations: a job that has just started
// and a job that is waiting behind every other translation on the service.
// The chip used to show `· 0%` for both, which reads as a translation
// stalled at the start.

test('progressText: waiting, live and counting, in that order', () => {
    assert.deepEqual(progressText({ done: 0, total: 0, queued: true }),
        { text: '· …', key: 'player.subtitleTranslationQueued', args: [] });
    // Queued outranks live: it is the count that is being contradicted.
    assert.deepEqual(progressText({ done: 3, total: 3, live: true, queued: true }),
        { text: '· …', key: 'player.subtitleTranslationQueued', args: [] });
    assert.deepEqual(progressText({ done: 3, total: 3, live: true }),
        { text: '· 3', key: 'player.subtitleTranslatingLive', args: [] });
    assert.deepEqual(progressText({ done: 12, total: 48 }),
        { text: '· 25%', key: 'player.subtitleTranslating', args: [25] });
    // No denominator yet, and not waiting long enough to say so.
    assert.deepEqual(progressText({ done: 0, total: 0 }),
        { text: '· 0%', key: 'player.subtitleTranslating', args: [0] });
});

test('pollProgress says "queued" after QUEUE_HINT_MS of 0/0, once', async (t) => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return answer('0/0'); };
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 1, queuedAfterMs: 15,
        onProgress: (p) => seen.push(p.queued === true),
    });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 60));
    // The first report is the ordinary 0/0; exactly one queued report
    // follows, however many polls happen after the threshold.
    assert.deepEqual(seen.slice(0, 2), [false, true], `reports: ${JSON.stringify(seen)}`);
    assert.equal(seen.filter(Boolean).length, 1, 'one hint, not one per poll');
    assert.ok(calls > 3, `expected the poll to keep running, got ${calls} calls`);
});

test('the queue hint clears on the first real count', async (t) => {
    const answers = ['0/0', '0/0', '0/0', '0/0', '4/100'];
    let i = 0;
    const fetchImpl = async () => answer(answers[Math.min(i++, answers.length - 1)]);
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 5, queuedAfterMs: 8,
        onProgress: (p) => seen.push([p.done, p.total, Boolean(p.queued)]),
    });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 60));
    assert.deepEqual(seen[0], [0, 0, false]);
    assert.ok(seen.some((r) => r[2]), 'the hint was shown');
    const last = seen[seen.length - 1];
    assert.deepEqual(last, [4, 100, false], 'and the count replaces it');
});

// A total appearing while done stays 0 is the job registering its cues --
// "queued" is over even though the number the poll keys on has not moved.
// The report used to fire on `done` alone, so that transition was silent
// and the chip kept saying "waiting" at 0/900.
test('a total appearing is a change worth reporting', async (t) => {
    const answers = ['0/0', '0/900'];
    let i = 0;
    const fetchImpl = async () => answer(answers[Math.min(i++, answers.length - 1)]);
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onProgress: (p) => seen.push([p.done, p.total]) });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 20));
    assert.deepEqual(seen, [[0, 0], [0, 900]]);
});

test('time asleep does not count toward the queue hint', async (t) => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return answer('0/0'); };
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 1, queuedAfterMs: 40,
        onProgress: (p) => seen.push(Boolean(p.queued)),
    });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 10));
    stop.suspend();
    await new Promise((r) => setTimeout(r, 50));
    stop.resume();
    await new Promise((r) => setTimeout(r, 10));
    // 20 ms of polling either side of a 50 ms sleep: a run asleep is not a
    // run queued, so the hint is not due yet.
    assert.deepEqual(seen.filter(Boolean), [], `reports: ${JSON.stringify(seen)}`);
    await new Promise((r) => setTimeout(r, 40));
    assert.deepEqual(seen.filter(Boolean), [true], 'and it still arrives once the waking run has waited');
});

test('QUEUE_HINT_MS is the shipped default', () => {
    assert.equal(QUEUE_HINT_MS, 30 * 1000);
});

// ---- X-Subtitle-Pending-From and onTick -------------------------------
//
// The header says where in the film the earliest untranslated cue the
// viewer can still meet begins. It is the only number in this module that
// is about the film rather than the job, and the catching-up banner is
// the only reader of it.

test('parseProgress reads X-Subtitle-Pending-From, and only a number', () => {
    assert.equal(parseProgress('3/10', true, '', '124.5').pendingFrom, 124.5);
    assert.equal(parseProgress('3/10', true, '', '0').pendingFrom, 0,
        'zero is a position in the film, not a missing header');
    // Absent: every response before the service shipped it, every batch
    // run, and every live run with nothing pending ahead.
    assert.equal(parseProgress('3/10', true).pendingFrom, null);
    assert.equal(parseProgress('3/10', true, '', null).pendingFrom, null);
    // Garbage, in the shapes Number() is happy to swallow.
    assert.equal(parseProgress('3/10', true, '', '').pendingFrom, null);
    assert.equal(parseProgress('3/10', true, '', '  ').pendingFrom, null);
    assert.equal(parseProgress('3/10', true, '', 'soon').pendingFrom, null);
    assert.equal(parseProgress('3/10', true, '', '12s').pendingFrom, null);
    // A negative is not a movie time.
    assert.equal(parseProgress('3/10', true, '', '-4').pendingFrom, null);
    assert.equal(parseProgress('3/10', true, '', 'Infinity').pendingFrom, null);
});

test('onTick fires on every 200, onProgress only on a change', async (t) => {
    // The banner compares the run against the playhead, and the playhead
    // moves while the counts stand still: a translation that stopped
    // producing during a film that keeps playing is exactly the case it
    // exists to catch. onProgress keeps its only-on-change contract.
    let calls = 0;
    const fetchImpl = async () => { calls++; return answer('3/10', { live: true, pendingFrom: '60' }); };
    const ticks = [];
    const changes = [];
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 1,
        onProgress: (p) => changes.push(p.done),
        onTick: (p) => ticks.push(p.pendingFrom),
    });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 25));
    stop();
    assert.deepEqual(changes, [3], 'the same answer is one change');
    assert.ok(ticks.length > 2, `every answer is a tick, got ${ticks.length} for ${calls} polls`);
    assert.equal(ticks.length, calls, 'one tick per successful poll, no more and no fewer');
    assert.deepEqual([...new Set(ticks)], [60], 'and the tick carries the report');
});

test('a pendingFrom that moves on its own is a change', async (t) => {
    // After a seek the run's frontier moves while the counts stand still —
    // the same cues, a different place in the film. Without pendingFrom in
    // the change test the banner would keep quoting the pre-seek position.
    let pending = '60';
    const fetchImpl = async () => answer('3/10', { live: true, pendingFrom: pending });
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onProgress: (p) => seen.push(p.pendingFrom) });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 15));
    pending = '900';
    await new Promise((r) => setTimeout(r, 15));
    stop();
    assert.deepEqual(seen, [60, 900]);
});

test('the header appearing and disappearing are both changes', async (t) => {
    // Absence is a value: it is what the service says when nothing is
    // pending ahead any more, and it is what takes the banner down.
    let pending = null;
    const fetchImpl = async () => answer('3/10', { live: true, pendingFrom: pending });
    const seen = [];
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onProgress: (p) => seen.push(p.pendingFrom) });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 15));
    pending = '90';
    await new Promise((r) => setTimeout(r, 15));
    pending = null;
    await new Promise((r) => setTimeout(r, 15));
    stop();
    assert.deepEqual(seen, [null, 90, null]);
});

test('onTick does not fire for a tick that never got a 200', async (t) => {
    // The negative control: the banner must not be re-evaluated against a
    // response that never arrived, or a dead service would read as a run
    // that is merely behind.
    const fetchImpl = async () => ({ status: 503, headers: { get: () => null } });
    const ticks = [];
    const errs = [];
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl, intervalMs: 1,
        onTick: (p) => ticks.push(p),
        onError: (c) => errs.push(c),
    });
    t.after(() => stop());
    await new Promise((r) => setTimeout(r, 20));
    assert.deepEqual(ticks, []);
    assert.deepEqual(errs, [503]);
});

// ---- which run an answer is about ----------------------------------------
//
// After a seek the player needs an answer about the run it is now watching.
// The service re-reads its playlist sooner when a poll names that run (`sof`)
// and says which run each answer describes (X-Subtitle-Session-Offset).

test('a live poll names the session offset it watches; a batch one does not', async () => {
    const urls = [];
    let live = true;
    let offset = 0;
    const fetchImpl = async (url) => { urls.push(url); return answer('3/10', { live }); };
    const stop = pollProgress('https://x/a.vtt?token=t', { fetchImpl, intervalMs: 5, sessionOffset: () => offset });
    await new Promise((r) => setTimeout(r, 12));
    offset = 720;
    await new Promise((r) => setTimeout(r, 20));
    stop();
    assert.equal(urls[0], 'https://x/a.vtt?token=t', 'the first poll does not know the source is live yet');
    assert.ok(urls.some((u) => u === 'https://x/a.vtt?token=t&sof=0'), `the live answer turns it on: ${urls}`);
    assert.ok(urls.at(-1) === 'https://x/a.vtt?token=t&sof=720', `and it follows a seek: ${urls.at(-1)}`);

    const batch = [];
    live = false;
    const stop2 = pollProgress('https://x/b.vtt', { fetchImpl: async (url) => { batch.push(url); return answer('3/10'); }, intervalMs: 5, sessionOffset: () => 720 });
    await new Promise((r) => setTimeout(r, 25));
    stop2();
    assert.ok(batch.length > 1 && batch.every((u) => u === 'https://x/b.vtt'), `a batch source never gets it: ${batch}`);
});

test('the answer carries the session offset it was computed against, when the service says', async () => {
    const seen = [];
    let so = '720.000';
    const stop = pollProgress('https://x/a.vtt', {
        fetchImpl: async () => answer('3/10', { live: true, sessionOffset: so }),
        intervalMs: 5,
        onTick: (p) => seen.push(p.sessionOffset),
    });
    await new Promise((r) => setTimeout(r, 12));
    so = null;
    await new Promise((r) => setTimeout(r, 15));
    so = 'garbage';
    await new Promise((r) => setTimeout(r, 15));
    stop();
    assert.equal(seen[0], 720);
    assert.ok(seen.includes(null), 'no header is null: an older service');
    assert.equal(seen.at(-1), null, 'and so is one that does not parse');
});
