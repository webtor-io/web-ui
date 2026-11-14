// app/private_refusal.js: private-refused-shown reaches Umami on a full page
// load (a shared link, a reload), not only after an async swap. On a full load
// the view's script runs before layout.js has set window.umami, and asyncView
// starts it afterwards; an inline script tracking at parse time lost it.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const card = (reason) => `<div data-umami-impression="private-refused-shown" data-umami-impression-code="private_torrent" data-umami-impression-reason="${reason}"><script src="/assets/private_refusal.js"></script></div>`;
const dom = new JSDOM(`<!doctype html><body><main>${card('sign_in')}</main></body>`, { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.CustomEvent = dom.window.CustomEvent;

test('a full page load counts the refusal once umami is up; a re-render does not, another refusal does', async () => {
    Object.defineProperty(document, 'currentScript', { value: document.querySelector('script'), configurable: true });
    await import('../app/private_refusal.js'); // the async script ran; umami is not there yet
    const calls = [];
    window.umami = { track: (name, props) => calls.push([name, props]) }; // layout.js: umami init
    (await import('./asyncView.js')).default(); // layout.js: initAsyncView()
    assert.deepEqual(calls, [['private-refused-shown', { code: 'private_torrent', reason: 'sign_in' }]]);

    const main = document.querySelector('main');
    for (const reason of ['sign_in', 'no_record']) { // loadAsyncView: the same refusal again, then another
        main.innerHTML = card(reason);
        window.dispatchEvent(new CustomEvent('async:/assets/private_refusal', { detail: { target: main.firstChild } }));
    }
    assert.deepEqual(calls.map(([, p]) => p.reason), ['sign_in', 'no_record']);
});
