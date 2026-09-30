import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createMediaRecovery, MEDIA_RETRY_WINDOW_MS } from './media-recovery.js';

function fake() {
    const calls = [];
    return {
        calls,
        recoverMediaError() { calls.push('recover'); },
        swapAudioCodec() { calls.push('swap'); },
        stopLoad() { calls.push('stop'); },
    };
}

// hls.js's advice (API.md, "Fatal Error Recovery"): recover; within the
// window, swap the audio codec and recover; within the window of that, give
// up. Given up, no recovery again -- but each further error stops loading and
// tells again (the player shows its card once): never a failure unsaid.
test('recover, swap and recover, give up -- then each further one said again, never recovered', () => {
    const hls = fake();
    let t = 0;
    const gaveUp = [];
    const m = createMediaRecovery(hls, { now: () => t, onGiveUp: (d) => gaveUp.push(d) });
    // Exactly the window apart is still within it.
    m.handle({ details: 'a' });
    t = MEDIA_RETRY_WINDOW_MS;
    m.handle({ details: 'b' });
    m.handle({ details: 'c' });
    assert.deepEqual(hls.calls, ['recover', 'swap', 'recover', 'stop']);
    assert.deepEqual(gaveUp, [{ details: 'c' }]);
    assert.equal(m.gaveUp, true);
    m.handle({ details: 'd' });
    assert.deepEqual(hls.calls, ['recover', 'swap', 'recover', 'stop', 'stop'], 'stopped again, not recovered');
    assert.deepEqual(gaveUp.map((d) => d.details), ['c', 'd'], 'said again');
});

// Just past the window is a new start: an occasional failure mid-film.
test('further apart than the window: recovered each time', () => {
    const hls = fake();
    let t = 0;
    const m = createMediaRecovery(hls, { now: () => t, onGiveUp: () => assert.fail('given up') });
    for (let i = 0; i < 5; i++) { m.handle({}); t += MEDIA_RETRY_WINDOW_MS + 1; }
    assert.deepEqual(hls.calls, ['recover', 'recover', 'recover', 'recover', 'recover']);
});

// The swap's window is its own: a third error long after the swap but soon
// after a fresh recovery is a swap again, not a give-up.
test('the swap\'s window is counted from the swap', () => {
    const hls = fake();
    let t = 0;
    const m = createMediaRecovery(hls, { now: () => t });
    m.handle({}); // recover at 0
    t = 100; m.handle({}); // swap at 100
    t = 100 + MEDIA_RETRY_WINDOW_MS + 1; m.handle({}); // recover (window from 0 passed)
    t += 100; m.handle({}); // swap again (window from 100 passed)
    assert.deepEqual(hls.calls, ['recover', 'swap', 'recover', 'recover', 'swap', 'recover']);
    assert.equal(m.gaveUp, false);
});

test('onGiveUp or stopLoad throwing: given up all the same', () => {
    const hls = { recoverMediaError() {}, swapAudioCodec() { throw new Error('x'); }, stopLoad() { throw new Error('y'); } };
    const m = createMediaRecovery(hls, { now: () => 0, onGiveUp: () => { throw new Error('z'); } });
    m.handle({}); m.handle({}); m.handle({});
    assert.equal(m.gaveUp, true);
});

// Each step's window is counted from the step before: a failure that comes
// back every 2.5 s is given up at the third, not recovered for ever.
test('a failure every 2.5 s: recovered, swapped, given up', () => {
    const hls = fake();
    let t = 0;
    const gaveUp = [];
    const m = createMediaRecovery(hls, { now: () => t, onGiveUp: (d) => gaveUp.push(d) });
    for (let i = 0; i < 3; i++) { m.handle({ i }); t += 2500; }
    assert.deepEqual(hls.calls, ['recover', 'swap', 'recover', 'stop']);
    assert.equal(gaveUp.length, 1);
});

// A new source starts the ladder again.
test('reset: recovered again after a give-up', () => {
    const hls = fake();
    const m = createMediaRecovery(hls, { now: () => 0 });
    m.handle({}); m.handle({}); m.handle({});
    assert.equal(m.gaveUp, true);
    m.reset();
    assert.equal(m.gaveUp, false);
    m.handle({});
    assert.deepEqual(hls.calls.slice(-1), ['recover']);
});
