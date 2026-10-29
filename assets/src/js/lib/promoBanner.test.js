import { test } from 'node:test';
import assert from 'node:assert/strict';
import { initPromoBanner } from './promoBanner.js';

// The banner as index.html and resource/get.html render it: the impression
// on the banner, its dismiss inside.
function banner() {
    const btn = { addEventListener() {} };
    return {
        dataset: { umamiImpression: 'promo-shown' },
        style: {},
        matches: () => true,
        querySelectorAll: () => [],
        querySelector: () => btn,
    };
}

function withPage(t, dismissedAt) {
    const calls = [];
    const store = dismissedAt === undefined ? {} : { 'promo-dismissed': String(dismissedAt) };
    globalThis.localStorage = { getItem: (k) => store[k] ?? null, setItem: (k, v) => { store[k] = v; } };
    globalThis.window = { umami: { track: (name, props) => calls.push([name, props]) } };
    t.after(() => { delete globalThis.localStorage; delete globalThis.window; });
    return calls;
}

test('a shown banner counts one impression', (t) => {
    const calls = withPage(t);
    const b = banner();
    initPromoBanner(b);
    assert.notEqual(b.style.display, 'none');
    assert.deepEqual(calls, [['promo-shown', {}]]);
});

test('a dismissed banner is hidden and is not an impression', (t) => {
    const calls = withPage(t, Date.now() - 60 * 1000);
    const b = banner();
    initPromoBanner(b);
    assert.equal(b.style.display, 'none');
    assert.deepEqual(calls, []);
});
