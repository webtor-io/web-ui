import { test } from 'node:test';
import assert from 'node:assert/strict';

// A localStorage that throws on access must not make makeDebug throw: it is
// awaited at the top level of modules the layout imports.
test('makeDebug: a throwing localStorage is a no-op logger, not an exception', async () => {
    Object.defineProperty(globalThis, 'localStorage', { get() { throw new Error('SecurityError'); }, configurable: true });
    const { makeDebug } = await import('./debug.js');
    const d = await makeDebug('webtor:test');
    assert.equal(typeof d, 'function');
    assert.doesNotThrow(() => d('x'));
    delete globalThis.localStorage;
});

test('makeDebug: without the flag a no-op, with it the debug logger', async () => {
    const store = {};
    Object.defineProperty(globalThis, 'localStorage', { value: store, configurable: true, writable: true });
    const { makeDebug } = await import('./debug.js');
    assert.equal((await makeDebug('webtor:test')).namespace, undefined);
    store.debug = 'webtor:*';
    assert.equal((await makeDebug('webtor:test')).namespace, 'webtor:test');
    delete globalThis.localStorage;
});
