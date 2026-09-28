import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// The embed (app/embed/check.js; this test lives here because every .js
// under app/ is a webpack entry) takes part in the HEVC passthrough
// declaration the way any page does -- and, like the layout, runs the
// decoder probe only for a browser that takes part: every other embed pays
// nothing (no decodingInfo, no write to the iframe's storage). The module
// then waits for its parent's `init` forever here; only its first lines run.
const doms = [];
after(() => { for (const d of doms) d.window.close(); });

async function embedPage(url, optin) {
    const dom = new JSDOM('<!doctype html><body></body>', { url, pretendToBeVisual: true });
    doms.push(dom);
    const w = dom.window;
    if (optin) w.localStorage.setItem('wt-passthrough', optin);
    for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'Event', 'CustomEvent', 'localStorage']) {
        Object.defineProperty(globalThis, k, { value: k === 'window' ? w : w[k], configurable: true, writable: true });
    }
    w._id = 'e1';
    // A fresh instance of the module for each page.
    import(`../../app/embed/check.js?page=${doms.length}`).catch(() => {});
    await new Promise((r) => setTimeout(r, 50));
    return w;
}

test('an embed of a browser that opted out runs no probe', async () => {
    const w = await embedPage('https://webtor.io/embed?id=e1', 'off');
    assert.ok(w.__wtDecode, 'the declaration module ran');
    assert.equal(w.__wtDecode.probe, null, 'no probe');
    assert.equal(w.localStorage.getItem('wt-decode'), null, 'nothing written');
});

test('an embed of a browser that takes part probes, by default and opted in', async () => {
    const d = await embedPage('https://webtor.io/embed?id=e1');
    assert.ok(d.__wtDecode && d.__wtDecode.probe, 'the probe started without any switch');
    const w = await embedPage('https://webtor.io/embed?id=e1', 'on');
    assert.ok(w.__wtDecode && w.__wtDecode.probe, 'the probe started');
});
