// An anonymous job start: Turnstile's check (lib/turnstileAction.js), then
// the request (lib/async.js). The busy button (lib/actionBusy.js) has to
// cover the check too.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.HTMLFormElement = dom.window.HTMLFormElement;
global.DOMParser = dom.window.DOMParser;
global.HTMLElement = dom.window.HTMLElement;
global.CustomEvent = dom.window.CustomEvent;
global.FormData = dom.window.FormData;
// jsdom has <dialog open> but no close().
dom.window.HTMLDialogElement.prototype.close = function () { this.open = false; };

// A widget whose answer the test gives by hand.
const widgets = [];
let executes = 0;
global.turnstile = {
    render(el, opts) { widgets.push(opts); return widgets.length - 1; },
    execute() { executes++; },
    reset() {},
    remove() {},
};
const answer = (token = 'tok') => widgets[0].callback(token);

document.body.innerHTML = `
    <div id="turnstile-action" data-sitekey="k" data-label="Checking"></div>
    <form id="watch" action="/stream-video" method="post" data-async-target="#log-1" data-async-push-state="false">
        <button type="submit"><svg class="icon"></svg>Watch</button>
    </form>
    <div id="log-1" data-async-layout="x"></div>
    <form id="tar" action="/download-dir" method="post" data-async-target="#log-dir" data-async-push-state="false">
        <button type="submit">TAR</button>
    </form>
    <dialog id="dlg" open><div id="log-dir" data-async-layout="x"></div></dialog>`;

const posts = [];
const { bindAsync } = await import('./async.js');
const { isBusy, clearBusy } = await import('./actionBusy.js');
const { default: initTurnstile } = await import('./turnstileAction.js');
initTurnstile();
bindAsync({
    async fetch(f, url) {
        posts.push(url);
        const res = new Response('<div class="progress-alert" data-async-progress-log="/x"><div class="log-target"></div></div>', { status: 200 });
        Object.defineProperty(res, 'url', { value: url });
        return res;
    },
    update() {},
    fallback: { selector: 'main', layout: '' },
});

const watch = document.getElementById('watch');
// Timer hops in sequence, not one 20 ms wait: the re-submit after a token is
// a chain of timers, and on a loaded machine one long wait could fire before
// the chain's later links were even scheduled (full-suite flake, 2026-10-14).
const settle = async () => { for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 2)); };

test('the button is busy from the press, through the check, and the token still goes out', async () => {
    watch.requestSubmit();
    assert.equal(isBusy(watch), true, 'busy while Turnstile checks -- not idle until the token');
    assert.ok(document.querySelector('#log-1 [data-turnstile-step]'), 'the check is written in the log');
    assert.equal(posts.length, 0, 'nothing sent before the token');
    answer();
    await settle();
    assert.equal(posts.length, 1, 'the second pass (token attached) is the request, not a repeat');
    assert.equal(isBusy(watch), true, 'and the job keeps it busy');
});

test('a busy button pressed again (Enter) spends no token and leaves the running log alone', async () => {
    assert.equal(isBusy(watch), true);
    const log = document.getElementById('log-1').innerHTML;
    const before = executes;
    watch.requestSubmit();
    await settle();
    assert.equal(executes, before, 'no second check');
    assert.equal(document.getElementById('log-1').innerHTML, log, 'the job log is not replaced by a step');
    assert.equal(posts.length, 1, 'no second request');
    clearBusy(watch);
});

test('a press dropped because another check is running closes the dialog it opened', async () => {
    watch.requestSubmit(); // a check is running for Watch now
    assert.equal(isBusy(watch), true);
    const dialog = document.getElementById('dlg');
    assert.equal(dialog.open, true);
    const tar = document.getElementById('tar');
    tar.requestSubmit();
    assert.equal(dialog.open, false, 'not left spinning over a job that never started');
    assert.equal(isBusy(tar), false, 'and its button is not busy');
    answer();
    await settle();
    clearBusy(watch);
});

test('the check is written under the button and brought into view', async () => {
    const scrolls = [];
    dom.window.innerHeight = 844;
    dom.window.matchMedia = () => ({ matches: false });
    dom.window.scrollTo = (o) => scrolls.push(o);
    const proto = dom.window.HTMLElement.prototype;
    const was = proto.getBoundingClientRect;
    proto.getBoundingClientRect = function () {
        return this.classList.contains('progress-alert') ? { top: 775, bottom: 869, height: 94 } : { top: 0, bottom: 0, height: 0 };
    };
    watch.requestSubmit();
    assert.equal(scrolls.length, 1, 'the step block, below the fold, is brought up');
    answer();
    await settle();
    proto.getBoundingClientRect = was;
    clearBusy(watch);
});

test('an answer that comes at once is re-submitted in a task of its own, not inside the press', async (t) => {
    // No script yet / a widget that throws: getToken resolves synchronously,
    // and a re-submit as a microtask would run inside the press's own submit
    // dispatch, where a browser ignores requestSubmit() (jsdom does not, so
    // this pins the task boundary itself).
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const was = global.turnstile.execute;
    global.turnstile.execute = () => { throw new Error('widget failed'); };
    const before = posts.length;
    watch.requestSubmit();
    for (let i = 0; i < 5; i++) await Promise.resolve();
    assert.equal(posts.length, before, 'nothing re-submitted inside the press');
    t.mock.timers.tick(0);
    assert.equal(posts.length, before + 1, 'the re-submit, a task later');
    global.turnstile.execute = was;
    t.mock.timers.reset();
    await settle();
    clearBusy(watch);
});

test('the live checkbox itself is brought into view, holding its height while the iframe loads', async () => {
    const scrolls = [];
    dom.window.innerHeight = 844;
    dom.window.matchMedia = () => ({ matches: false });
    dom.window.scrollTo = (o) => scrolls.push(o);
    watch.requestSubmit();
    const step = document.querySelector('#log-1 [data-turnstile-step]');
    const slot = step.lastElementChild;
    // The block may be tall (a card's whole log): only the slot counts.
    step.getBoundingClientRect = () => ({ top: 100, bottom: 1300, height: 1200 });
    slot.getBoundingClientRect = () => ({ top: 900, bottom: 965, height: 65 });
    scrolls.length = 0;
    widgets[0]['before-interactive-callback']();
    assert.equal(slot.style.minHeight, '65px');
    assert.equal(scrolls.length, 1);
    assert.equal(scrolls[0].top, 965 - (844 - 12), 'the slot to the fold, not the block\'s top under the navbar');
    answer();
    await settle();
    clearBusy(watch);
});
