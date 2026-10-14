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
const settle = () => new Promise((r) => setTimeout(r, 20));

test('the button is busy from the press, through the check, and the token still goes out', async () => {
    watch.requestSubmit();
    assert.equal(isBusy(watch), true, 'busy while Turnstile checks -- not idle until the token');
    assert.ok(document.querySelector('#log-1 pre[task-tag=turnstile]'), 'the check is written in the log');
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
