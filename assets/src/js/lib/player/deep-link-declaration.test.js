import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// A deep link (`#action=stream`) starts the stream as soon as the form is
// there -- possibly before the page's decoder probe has answered. A browser
// that declares gets up to 300 ms for it (app/resource/get.js), so a probe
// that answers within that sends its tokens, not `unknown`.
const dom = new JSDOM(`<!doctype html><body><div id="host"><script id="s"></script></div>
    <form class="stream-video" action="/stream-video" method="post">
        <input type="hidden" name="resource-id" value="r"><input type="hidden" name="item-id" value="i">
    </form></body>`, { url: 'https://webtor.io/r?file=a.mkv#action=stream' });
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

test('a deep-link start waits for a probe that answers within 300 ms', async () => {
    w.localStorage.setItem(OPTIN_KEY, 'on');
    let answer;
    const pq = new Promise((r) => { answer = r; });
    startProbe(w, { load: async () => ({ declarationSupport: () => ({ path: 'mse', hevc: ['hevc8', 'hevc10'], pq }), envFromWindow: () => ({}) }) });
    installSubmitHook(w.document, w);
    const sent = [];
    const form = w.document.querySelector('form');
    form.addEventListener('submit', (e) => {
        e.preventDefault();
        const d = form.querySelector('input[name="decode"]');
        sent.push(d ? d.value : null);
    });
    nodeSetTimeout(() => answer(true), 100);
    await import('../../app/resource/get.js');
    const [, init] = w.av[0];
    await init.call(w.document.getElementById('host'));
    assert.deepEqual(sent, ['hevc8,hevc10,hdr-pq']);
});
