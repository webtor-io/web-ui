// lib/turnstileAction.js when the Turnstile script is not there yet (async,
// deferred: a slow phone; a deep link that presses at load), and when a
// check is left behind by a swap.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.HTMLFormElement = dom.window.HTMLFormElement;

document.body.innerHTML = `
    <div id="turnstile-action" data-sitekey="k" data-label="Checking that you are not a robot" data-label-interactive="Tick the box to continue"></div>
    <form id="watch" action="/stream-video" method="post" data-async-target="#log-1"><button type="submit">Watch</button></form>
    <div id="log-1"></div>
    <form id="other" action="/download-file" method="post" data-async-target="#log-2"><button type="submit">Download</button></form>
    <div id="log-2"></div>`;

const { default: init } = await import('./turnstileAction.js');
const { isBusy, clearBusy } = await import('./actionBusy.js');
init();
test.afterEach(() => { for (const f of document.querySelectorAll('form')) clearBusy(f); });

const passes = [];
document.addEventListener('submit', (e) => {
    if (e.target.dataset.turnstileReady) {
        e.preventDefault();
        passes.push({ id: e.target.id, token: e.target.querySelector('input[name="cf-turnstile-response"]')?.value });
    }
});
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// A widget that vouches silently once it exists; one that never answers.
const widgets = [];
function scriptArrives({ answers = true } = {}) {
    global.turnstile = {
        render(el, opts) { widgets.push(opts); return widgets.length - 1; },
        execute(id) { if (answers) setTimeout(() => widgets[id].callback('tok'), 0); },
        reset() {}, remove() {},
    };
}

test('a press before the script: the step on screen, the press held, the token sent once the script is there', async () => {
    const watch = document.getElementById('watch');
    watch.requestSubmit();
    assert.equal(isBusy(watch), true);
    assert.ok(document.querySelector('#log-1 [data-turnstile-step]'), 'the check is on screen');
    await wait(300);
    assert.deepEqual(passes, [], 'not refused at once for want of a script');
    scriptArrives();
    await wait(800);
    assert.deepEqual(passes, [{ id: 'watch', token: 'tok' }]);
});

test('a check whose step went with its log does not hold every other press', async () => {
    passes.length = 0;
    global.turnstile.execute = () => {}; // no answer: the check stays pending
    const watch = document.getElementById('watch');
    watch.requestSubmit();
    assert.ok(document.querySelector('#log-1 [data-turnstile-step]'));
    // Another file picked: #content swapped, the step and its form gone.
    document.getElementById('log-1').replaceChildren();
    watch.remove();
    const other = document.getElementById('other');
    other.requestSubmit();
    assert.ok(document.querySelector('#log-2 [data-turnstile-step]'), 'the new press gets its check');
    await wait(50);
    assert.deepEqual(passes, [], 'and the old, detached form sends nothing');
    window.__turnstileAction.pending.finish('', 'test'); // its 15 s timer
    clearBusy(watch); // out of the document: afterEach does not see it
});
