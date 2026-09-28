import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// A passthrough that failed after the player moved on to the next file
// quietly (lib/player/passthrough.js fallbackToOldRoute) restarts it through
// this page's deep link, with why in the hash: the start carries the
// fallback's fields and no declaration for the file -- even in a browser
// whose storage keeps no memory of the failure.
const dom = new JSDOM(`<!doctype html><body><div id="host"><script id="s"></script></div>
    <form class="stream-video" action="/stream-video" method="post">
        <input type="hidden" name="resource-id" value="r"><input type="hidden" name="item-id" value="ep2">
    </form></body>`, { url: 'https://webtor.io/r?file=ep2.mkv#action=stream&decode-fallback=no_frames&decode-class=hevc10-2160' });
const w = dom.window;
for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'HTMLFormElement', 'Event', 'CustomEvent', 'FormData', 'Node', 'MutationObserver', 'URLSearchParams']) {
    Object.defineProperty(globalThis, k, { value: k === 'window' ? w : w[k], configurable: true, writable: true });
}
const nodeSetTimeout = globalThis.setTimeout;
const timers = new Set();
globalThis.setTimeout = (fn, ms, ...a) => { const t = nodeSetTimeout(fn, ms, ...a); timers.add(t); return t; };
Object.defineProperty(w.document, 'currentScript', { value: w.document.getElementById('s'), configurable: true });
after(() => {
    for (const t of timers) clearTimeout(t);
    w.close();
});

const { OPTIN_KEY, startProbe, installSubmitHook } = await import('./decode-declaration.js');

test('a deep link with a fallback\'s reason restarts the file with why and without a declaration', async () => {
    w.localStorage.setItem(OPTIN_KEY, 'on');
    // A probe that answers at once, with every token: without the note the
    // start would declare them all.
    startProbe(w, { load: async () => ({ declarationSupport: () => ({ path: 'mse', hevc: ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160'], pq: Promise.resolve(true) }), envFromWindow: () => ({}) }) });
    installSubmitHook(w.document, w);
    const sent = [];
    const form = w.document.querySelector('form');
    form.addEventListener('submit', (e) => {
        e.preventDefault();
        sent.push(Object.fromEntries(new w.FormData(form)));
    });
    await import('../../app/resource/get.js');
    const [, init] = w.av[0];
    await init.call(w.document.getElementById('host'));
    assert.equal(sent.length, 1);
    assert.equal(sent[0]['decode-fallback'], 'no_frames');
    assert.equal(sent[0]['decode-class'], 'hevc10-2160');
    assert.equal(sent[0].decode, undefined);
});
