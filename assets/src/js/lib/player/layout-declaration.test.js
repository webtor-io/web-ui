import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// The layout sets up the HEVC passthrough declaration before Turnstile and
// the async navigation (app/layout.js). In a browser whose localStorage
// throws on access (blocked site data, some private modes) the declaration
// must not take the other two down: without Turnstile an anonymous start is
// refused by the server, without the async binding nothing navigates.
const dom = new JSDOM(`<!doctype html><body>
    <div id="progress" class="hidden"></div>
    <div id="turnstile-action" data-sitekey="k" data-label="checking"></div>
    <main data-async-layout="L">
        <div id="log-1" data-async-layout="L"></div>
        <form class="stream-video" action="/stream-video" method="post" data-async-target="#log-1" data-async-push-state="false">
            <input type="hidden" name="resource-id" value="r"><input type="hidden" name="item-id" value="i">
            <button type="submit">Watch</button>
        </form>
        <a id="nav" href="/about" data-async-target="main">about</a>
    </main>
</body>`, { url: 'https://webtor.io/?passthrough=on', pretendToBeVisual: true });
const w = dom.window;
Object.defineProperty(w, 'localStorage', { get() { throw new w.DOMException('denied', 'SecurityError'); }, configurable: true });
for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'HTMLFormElement', 'Event', 'CustomEvent', 'FormData', 'DOMParser', 'Node', 'MutationObserver']) {
    Object.defineProperty(globalThis, k, { value: k === 'window' ? w : w[k], configurable: true, writable: true });
}
// Every timer the page starts is recorded and cleared at the end (Turnstile
// polls for its script for 15 s, a busy start button waits minutes for its
// job): the test process must be able to exit.
const nodeSetTimeout = globalThis.setTimeout;
const nodeSetInterval = globalThis.setInterval;
const timers = new Set();
globalThis.setTimeout = (fn, ms, ...a) => { const t = nodeSetTimeout(fn, ms, ...a); timers.add(t); return t; };
globalThis.setInterval = (fn, ms, ...a) => { const t = nodeSetInterval(fn, ms, ...a); timers.add(t); return t; };
const fetched = [];
w.fetch = globalThis.fetch = async (url, opts) => { fetched.push({ url: String(url), opts }); return { ok: true, status: 200, url: 'https://webtor.io/x', text: async () => '' }; };

await import('../../app/layout.js');
after(() => {
    for (const t of timers) { clearTimeout(t); clearInterval(t); }
    w.close();
});

test('with a throwing localStorage the layout still binds the async navigation and Turnstile', async () => {
    const form = w.document.querySelector('form.stream-video');
    assert.equal(form._asyncBound, true, 'bindAsync ran: the start form is async');
    assert.equal(w.document.getElementById('nav')._asyncBound, true);
    // Turnstile's capture listener is in place: an anonymous start is held
    // for a token (no script here -- it fails closed and says why).
    form.requestSubmit();
    await new Promise((r) => setTimeout(r, 50));
    const reason = form.querySelector('input[name="cf-turnstile-reason"]');
    assert.ok(reason, 'the submit went through Turnstile');
    // And the declaration took part (the switch held for the page).
    assert.equal(w.__wtDecode.optin, 'on');
});
