import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { waitForElement } from './waitForElement.js';

function dom() {
    const { window } = new JSDOM('<body><div id="root"></div></body>');
    return window;
}

test('resolves at once when the element is already there', async () => {
    const w = dom();
    w.document.getElementById('root').innerHTML = '<form class="stream"></form>';
    const el = await waitForElement(() => w.document.querySelector('form.stream'), { root: w.document.body, observe: w.MutationObserver });
    assert.ok(el);
});

test('resolves when the element appears later', async () => {
    const w = dom();
    const p = waitForElement(() => w.document.querySelector('form.stream'), { root: w.document.body, observe: w.MutationObserver, timeoutMs: 2000 });
    setTimeout(() => { w.document.getElementById('root').innerHTML = '<form class="stream"></form>'; }, 30);
    const el = await p;
    assert.ok(el, 'the late element must be found');
    assert.equal(el.className, 'stream');
});

test('resolves null on timeout', async () => {
    const w = dom();
    const el = await waitForElement(() => w.document.querySelector('form.stream'), { root: w.document.body, observe: w.MutationObserver, timeoutMs: 50 });
    assert.equal(el, null);
});
