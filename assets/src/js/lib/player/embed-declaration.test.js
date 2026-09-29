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

// The audio part's own switch reaches the embed the same way: `?audio=` on
// its address is read and remembered there. Its probe asks about audio
// wherever an audio token may go out -- by default (every audio token since
// the audio's stage 5) and with `?audio=on` -- and not for a browser that
// opened `?audio=off`.
test('an embed reads ?audio= too, and asks about audio unless opted out', async () => {
    const w = await embedPage('https://webtor.io/embed?id=e1&audio=on', 'on');
    assert.equal(w.localStorage.getItem('wt-audio'), 'on');
    assert.equal(w.__wtDecode.askAudio, true);
    const v = await embedPage('https://webtor.io/embed?id=e1', 'on');
    assert.equal(v.localStorage.getItem('wt-audio'), null);
    assert.equal(v.__wtDecode.askAudio, true, 'by default');
    const x = await embedPage('https://webtor.io/embed?id=e1&audio=off', 'on');
    assert.equal(x.localStorage.getItem('wt-audio'), 'off');
    assert.equal(x.__wtDecode.askAudio, false, 'opted out: the video probe only');
});
