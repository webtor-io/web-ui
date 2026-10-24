// Turnstile on job starts (lib/turnstileAction.js): where the check is shown,
// and that the form it held back can still be sent.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.HTMLFormElement = dom.window.HTMLFormElement;

// A widget that always vouches silently: execute() answers with a token --
// unless a test sets `silent`, and then no widget answers by itself.
const widgets = [];
let silent = false;
global.turnstile = {
    render(el, opts) { widgets.push(opts); return widgets.length - 1; },
    execute(id) { if (!silent) setTimeout(() => widgets[id].callback('tok'), 0); },
    reset() {},
    remove() {},
};

document.body.innerHTML = `
    <div id="turnstile-action" data-sitekey="k" data-label="Checking that you are not a robot" data-label-interactive="Tick the box to continue"></div>
    <form id="watch" action="/stream-video" method="post" data-async-target="#log-1">
        <button type="submit">Watch</button>
    </form>
    <div id="log-1"></div>`;

const { default: init } = await import('./turnstileAction.js');
const { clearBusy } = await import('./actionBusy.js');
init();
// A press marks its form busy (lib/actionBusy.js) with a 15-minute backstop
// timer; nothing here ends the job, so each test releases what it pressed --
// a live timer kept this file's process up for the full 15 minutes.
test.afterEach(() => { for (const f of document.querySelectorAll('form')) clearBusy(f); silent = false; });

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
// Timer hops in sequence, not one 20 ms wait: the re-submit after a token is
// a chain of timers, and on a loaded machine one long wait could fire before
// the chain's later links were even scheduled (full-suite flake, 2026-10-14).
const settle = async () => { for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 2)); };

test('a form outside its log gets the check written into the log, then goes out', async () => {
    const form = document.getElementById('watch');
    const seen = watchSecondPass(form);
    form.requestSubmit();
    assert.ok(document.querySelector('#log-1 [data-turnstile-step]'), 'the step is in the log');
    await settle();
    assert.deepEqual(seen, [{ ready: true, connected: true }]);
});

test('File info is a job start: its form gets the check and a token too', async () => {
    // Without /file-info in the list the form went out without a token and
    // the server refused every anonymous press (fail closed).
    document.getElementById('log-1').innerHTML = '';
    const form = document.createElement('form');
    form.className = 'file-info';
    form.setAttribute('action', '/ru/file-info');
    form.setAttribute('method', 'post');
    form.setAttribute('data-async-target', '#log-1');
    document.body.appendChild(form);
    const seen = watchSecondPass(form);
    form.requestSubmit();
    assert.ok(document.querySelector('#log-1 [data-turnstile-step]'), 'the step is in the log');
    await settle();
    assert.deepEqual(seen, [{ ready: true, connected: true }]);
    form.remove();
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
    assert.equal(document.querySelector('#log-1 [data-turnstile-step]'), null, 'no step block over the card');
    await settle();
    assert.deepEqual(seen, [{ ready: true, connected: true }]);
});

test("the check is the job's first step in its own words; when Cloudflare wants a click, the line says so", async () => {
    document.getElementById('log-1').innerHTML = '';
    const form = document.getElementById('watch');
    watchSecondPass(form);
    form.requestSubmit();
    const step = document.querySelector('#log-1 [data-turnstile-step]');
    assert.equal(step.querySelector('.job-stage-title').textContent, 'Checking that you are not a robot');
    assert.ok(step.querySelector('progress.job-stage-bar'), 'with the bar of every step');
    // The warm widget asks for interaction: a live one is rendered here.
    widgets[0]['before-interactive-callback']();
    assert.equal(step.querySelector('.job-stage-title').textContent, 'Tick the box to continue');
    assert.ok(step.querySelector('.job-stage').classList.contains('job-stage-done'), 'no spinner: the wait is the viewer\'s');
    await settle();
});

test('once ticked, the line goes back to working until the job answers', async () => {
    document.getElementById('log-1').innerHTML = '';
    const form = document.getElementById('watch');
    form.requestSubmit();
    const step = document.querySelector('#log-1 [data-turnstile-step]');
    const warm = widgets.length - 1;
    widgets[warm]['before-interactive-callback']();
    assert.equal(step.querySelector('.job-stage-title').textContent, 'Tick the box to continue');
    await settle(); // the live widget answers (the stub's execute)
    assert.equal(step.querySelector('.job-stage-title').textContent, 'Checking that you are not a robot');
    assert.equal(step.querySelector('.job-stage').classList.contains('job-stage-done'), false, 'spinning again');
    assert.equal(step.lastElementChild.style.minHeight, '', 'no empty box held open');
});

test('on a narrow phone the live checkbox is the compact one', async () => {
    document.getElementById('log-1').innerHTML = '';
    const form = document.getElementById('watch');
    form.requestSubmit();
    const step = document.querySelector('#log-1 [data-turnstile-step]');
    Object.defineProperty(step.lastElementChild, 'clientWidth', { value: 262, configurable: true });
    const warm = widgets.length - 1;
    widgets[warm]['before-interactive-callback']();
    assert.equal(widgets.at(-1).size, 'compact');
    assert.equal(step.lastElementChild.style.minHeight, '140px');
    await settle();
});

// The answer resolves a promise and the re-submit is a timer of its own: let
// the promise jobs run (setImmediate is not mocked), then fire the timer.
const resubmit = async (t) => { await new Promise((r) => setImmediate(r)); t.mock.timers.tick(0); };

test('silent past the deadline: a widget the person can see, not a refusal; its token says so', async (t) => {
    // 2026-10-23: a person on a slow phone was refused twice at 15 s, then
    // passed the support form's visible widget.
    t.mock.timers.enable({ apis: ['setTimeout'] });
    silent = true;
    document.getElementById('log-1').innerHTML = '';
    const form = document.getElementById('watch');
    const seen = watchSecondPass(form);
    form.requestSubmit();
    const step = document.querySelector('#log-1 [data-turnstile-step]');
    const before = widgets.length;
    t.mock.timers.tick(15000);
    assert.deepEqual(seen, [], 'not sent at 15 s');
    assert.equal(widgets.length, before + 1, 'a live widget');
    const live = widgets.at(-1);
    assert.equal(live.appearance, 'always');
    assert.equal(step.querySelector('.job-stage-title').textContent, 'Checking that you are not a robot', 'no click asked for yet');
    live.callback('tok-late');
    await resubmit(t);
    assert.deepEqual(seen, [{ ready: true, connected: true }]);
    assert.equal(form.querySelector('[name="cf-turnstile-response"]').value, 'tok-late');
    assert.match(form.querySelector('[name="cf-turnstile-reason"]').value, /^silent-fallback:\d+$/);
});

test('a fallback nobody answers is refused after two minutes, and says which wait ran out', async (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    silent = true;
    document.getElementById('log-1').innerHTML = '';
    const form = document.getElementById('watch');
    const seen = watchSecondPass(form);
    form.requestSubmit();
    t.mock.timers.tick(15000);
    t.mock.timers.tick(119000);
    assert.deepEqual(seen, [], 'still waiting at 2:14');
    t.mock.timers.tick(1000);
    await resubmit(t);
    assert.deepEqual(seen, [{ ready: true, connected: true }]);
    assert.equal(form.querySelector('[name="cf-turnstile-response"]').value, '');
    assert.match(form.querySelector('[name="cf-turnstile-reason"]').value, /^fallback-timeout:\d+$/);
});
