// Turnstile on job starts (lib/turnstileAction.js): where the check is shown,
// and that the form it held back can still be sent.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.HTMLFormElement = dom.window.HTMLFormElement;

// A widget that always vouches silently: execute() answers with a token.
const widgets = [];
global.turnstile = {
    render(el, opts) { widgets.push(opts); return widgets.length - 1; },
    execute(id) { setTimeout(() => widgets[id].callback('tok'), 0); },
    reset() {},
    remove() {},
};

document.body.innerHTML = `
    <div id="turnstile-action" data-sitekey="k" data-label="Checking that you are not a robot"></div>
    <form id="watch" action="/stream-video" method="post" data-async-target="#log-1">
        <button type="submit">Watch</button>
    </form>
    <div id="log-1"></div>`;

const { default: init } = await import('./turnstileAction.js');
init();

// The second pass -- token attached -- is the one that reaches the form
// (the first is stopped on document in the capture phase). Record whether the
// form was still in the document then: jsdom fires submit on a detached form,
// a browser does not (a form that is not connected "cannot navigate").
function watchSecondPass(form) {
    const seen = [];
    form.addEventListener('submit', (e) => {
        e.preventDefault();
        seen.push({ ready: !!form.dataset.turnstileReady, connected: form.isConnected });
    });
    return seen;
}
const settle = () => new Promise((r) => setTimeout(r, 20));

test('a form outside its log gets the check written into the log, then goes out', async () => {
    const form = document.getElementById('watch');
    const seen = watchSecondPass(form);
    form.requestSubmit();
    assert.ok(document.querySelector('#log-1 pre[task-tag=turnstile]'), 'the step is in the log');
    await settle();
    assert.deepEqual(seen, [{ ready: true, connected: true }]);
});

test('a retry form inside its own log stays connected and goes out (anonymous "Try again")', async () => {
    // The no-peers and slow cards render their resubmit forms into the very
    // log they target (#log-<item>). Writing the step block there replaced
    // the log -- and the form with it -- so the re-submit after the token
    // went nowhere: the button did nothing for anyone not signed in.
    document.getElementById('log-1').innerHTML = `
        <div class="progress-alert"><div class="log-target"><div task-custom="no-peers">
            <form id="retry" action="/stream-video" method="post" data-async-target="#log-1">
                <button type="submit">Try again</button>
            </form>
        </div></div></div>`;
    const form = document.getElementById('retry');
    const seen = watchSecondPass(form);
    form.requestSubmit(form.querySelector('button'));
    assert.ok(form.isConnected, 'the card is not wiped by the check');
    assert.equal(document.querySelector('#log-1 pre[task-tag=turnstile]'), null, 'no step block over the card');
    await settle();
    assert.deepEqual(seen, [{ ready: true, connected: true }]);
});
