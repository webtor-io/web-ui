// lib/async.js and the busy job-start button (actionBusy.js): what releases
// the button when the request itself is the answer.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.DOMParser = dom.window.DOMParser;
global.HTMLElement = dom.window.HTMLElement;
global.CustomEvent = dom.window.CustomEvent;
global.FormData = dom.window.FormData;

document.body.innerHTML = `
    <form class="stream" action="/stream-video" method="post" data-async-target="#log-1" data-async-push-state="false">
        <button type="submit"><svg class="icon"></svg>Watch</button>
    </form>
    <div id="log-1" data-async-layout="{{ template &quot;main&quot; . }}"></div>`;

// The answer the next request gets: post.html's refusal card (no job), or a
// job log the job's own messages will release the button from.
let answer = { status: 400, body: '<div class="progress-alert closeable" role="alert"><pre>refused</pre></div>' };

const { bindAsync } = await import('./async.js');
const { isBusy, clearBusy } = await import('./actionBusy.js');
bindAsync({
    async fetch(f, url) {
        const a = answer; // this request's, whatever the next one is told
        if (a.gate) await a.gate;
        const res = new Response(a.body, { status: a.status });
        Object.defineProperty(res, 'url', { value: url });
        return res;
    },
    update() {},
    fallback: { selector: 'main', layout: '' },
});

const form = document.querySelector('form.stream');
// Timer hops in sequence, not one 20 ms wait: the re-submit after a token is
// a chain of timers, and on a loaded machine one long wait could fire before
// the chain's later links were even scheduled (full-suite flake, 2026-10-14).
const settle = async () => { for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 2)); };

test('a refused start (400, no job) releases its button and shows the card', async () => {
    answer = { status: 400, body: '<div class="progress-alert closeable" role="alert"><pre>refused</pre></div>' };
    form.requestSubmit();
    assert.equal(isBusy(form), true, 'busy while the request is out');
    await settle();
    assert.ok(document.querySelector('#log-1 [role=alert]'), 'the card is in the log');
    // Nothing else would ever release it: no job log arrives with a 400. It
    // stayed dead for BUSY_MAX_MS (15 min) under a card that said "try again".
    assert.equal(isBusy(form), false, 'the button can be pressed again');
    assert.equal(form.querySelector('button').classList.contains('btn-busy'), false);
});

test('a started job keeps its button busy: the job log is what ends it', async () => {
    answer = { status: 200, body: '<div class="progress-alert" data-async-progress-log="/x"><div class="log-target"></div></div>' };
    form.requestSubmit();
    await settle();
    assert.equal(isBusy(form), true, 'a 200 does not release it');
    // A press on the working button lands on the form, and the form async.js
    // bound carries the counter.
    const events = [];
    window.umami = { track: (name, data) => events.push(name) };
    form.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
    assert.deepEqual(events, ['busy-press']);
    delete window.umami;
    clearBusy(form); // its 15-min backstop would hold the runner
});

test('a refused start whose button a watched toggle re-rendered meanwhile releases the new one', async () => {
    // The toggle (an in-place form: data-async-no-scroll) answers before the
    // start does, and keeps the job's log (lib/loadAsyncView.js keepJobs):
    // the press moves to the re-rendered Watch, and the refusal finds the
    // form it was sent from detached.
    const wrap = document.createElement('div');
    wrap.id = 'wrap';
    wrap.setAttribute('data-async-layout', '');
    wrap.append(...document.body.childNodes);
    wrap.insertAdjacentHTML('beforeend', '<form id="toggle" action="/library/movie/tt1/mark" method="post" data-async-target="#wrap" data-async-no-scroll="true" data-async-push-state="false"><button type="submit">Mark</button></form>');
    document.body.appendChild(wrap);
    window.dispatchEvent(new CustomEvent('async', { detail: { target: wrap } }));
    let open;
    answer = { status: 400, body: '<div class="progress-alert closeable" role="alert"><pre>refused</pre></div>', gate: new Promise((r) => { open = r; }) };
    const pressed = document.querySelector('form.stream');
    pressed.requestSubmit();
    assert.equal(isBusy(pressed), true);
    answer = { status: 200, body: wrap.innerHTML.replace('>Mark<', '>Watched<') };
    document.getElementById('toggle').requestSubmit();
    await settle();
    const twin = document.querySelector('form.stream');
    assert.equal(document.querySelector('#toggle button').textContent, 'Watched', 'the toggle re-rendered the page');
    assert.notEqual(twin, pressed);
    assert.equal(isBusy(twin), true, 'the press goes on in the new Watch');
    open();
    await settle();
    assert.ok(document.querySelector('#log-1 [role=alert]'), 'the card lands in the kept log');
    assert.equal(isBusy(twin), false, 'not left spinning for the 15-minute backstop');
    clearBusy(twin);
    clearBusy(pressed);
});
