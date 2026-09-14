import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseProgress, withRev, pollProgress, POLL_TIMEOUT_MS } from './subtitle-progress.js';

test('parseProgress', () => {
    assert.deepEqual(parseProgress('12/48'), { done: 12, total: 48, final: false });
    assert.deepEqual(parseProgress('100/100'), { done: 100, total: 100, final: true });
    // 0/0 is "the job has not counted the cues yet", not "done".
    assert.deepEqual(parseProgress('0/0'), { done: 0, total: 0, final: false });
    assert.deepEqual(parseProgress(null), { done: 0, total: 0, final: false });
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
