// lib/loadAsyncView.js: an in-place toggle -- the watched mark, a rating --
// re-renders the whole page around a running job and keeps the job.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body><main id="main"></main></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.CustomEvent = dom.window.CustomEvent;

const { default: loadAsyncView } = await import('./loadAsyncView.js');
const { markBusy, isBusy, clearBusy } = await import('./actionBusy.js');
// Every form ever busy here, the detached ones too: a busy timer left running
// holds the runner for its fifteen minutes.
const seen = new Set();
test.afterEach(() => { for (const f of [...document.querySelectorAll('form'), ...seen]) clearBusy(f); seen.clear(); });

const ACTION = 'https://webtor.io/assets/action.js';
const events = [];
for (const name of ['async:/assets/action', 'async:/assets/action_destroy', 'async:/assets/list', 'async:/assets/list_destroy']) {
    window.addEventListener(name, (e) => events.push([name, e.detail.target.id]));
}
const IN_PLACE = { noScroll: true, keepJobs: true };

// The resource page as the server renders it: the file card (its start form,
// the toggle, the job's log) and the list (its view, the archive dialog's
// TAR and ZIP -- same target, same action -- and their log).
const page = (watched) => `
    <div id="file">
        <form id="watch" action="/stream-video" method="post" data-async-target="#log-f1"><button type="submit"><svg></svg>Watch</button></form>
        <form id="toggle" action="/library/movie/tt1/${watched ? 'unmark' : 'mark'}" method="post" data-async-target="main"><button type="submit">${watched ? 'Watched' : 'Mark'}</button></form>
        <div id="log-f1"></div>
    </div>
    <div id="list" data-async-view="/assets/list"><script src="/assets/list.js"></script>
        <form class="tar" action="/download-dir" method="post" data-async-target="#log-l1"><input type="hidden" name="archive-format" value="tar"><button type="submit">TAR</button></form>
        <form class="zip" action="/download-dir" method="post" data-async-target="#log-l1"><input type="hidden" name="archive-format" value="zip"><button type="submit">ZIP</button></form>
        <div id="log-l1"><span class="spinner"></span></div>
    </div>`;

// A job in the log: what app/action.js's view leaves there (asyncView.js puts
// data-async-view on the script's parent -- the log itself).
function startJob() {
    const log = document.getElementById('log-f1');
    log.innerHTML = `<script src="${ACTION}"></script><div class="progress-alert" data-async-progress-log="/log/1">Buffering</div><div class="player"><video></video></div>`;
    log.setAttribute('data-async-view', '/assets/action');
    return log;
}

function fresh(watched = false) {
    document.getElementById('main').innerHTML = page(watched);
    events.length = 0;
}
const main = () => document.getElementById('main');

test('the watched toggle re-renders the page around a running job: the job stays, as it was', () => {
    fresh();
    const log = startJob();
    const script = log.querySelector('script');
    loadAsyncView(main(), page(true), IN_PLACE);
    assert.equal(document.getElementById('log-f1'), log, 'the same log, moved into its new place');
    assert.equal(log.querySelector('.progress-alert').textContent, 'Buffering');
    assert.equal(log.querySelector('script'), script, 'its script not run again');
    assert.equal(document.querySelector('#toggle button').textContent, 'Watched', 'the rest is the new page');
    assert.equal(document.querySelectorAll('[id="log-f1"]').length, 1, 'the new, empty log is not there too');
    assert.deepEqual(events.filter(([, id]) => id === 'log-f1'), [], 'the job is neither told it is going nor started again');
    assert.ok(events.some(([n]) => n === 'async:/assets/list_destroy'), 'the views around it are');
    assert.ok(events.some(([n]) => n === 'async:/assets/list'));
});

test('any other swap of the page renders it anew: a log id names a file, not a job', () => {
    // The home page's #log-load comes back with a new job in it; a language
    // switch re-renders the page in other words; another torrent can have a
    // file at the same path.
    fresh();
    const log = startJob();
    loadAsyncView(main(), page(true), { noScroll: false });
    assert.notEqual(document.getElementById('log-f1'), log);
    assert.deepEqual(events.filter(([n]) => n === 'async:/assets/action_destroy'), [['async:/assets/action_destroy', 'log-f1']]);
});

test('the start form rendered anew is busy from the same press, and the old one lets go', () => {
    fresh();
    startJob();
    const old = document.getElementById('watch');
    seen.add(old);
    markBusy(old);
    const { busyPress, busySince } = old.dataset;
    loadAsyncView(main(), page(true), IN_PLACE);
    const twin = document.getElementById('watch');
    assert.notEqual(twin, old);
    assert.equal(isBusy(twin), true, 'a second press on Watch is still swallowed');
    assert.equal(twin.dataset.busyPress, busyPress, 'the job that releases the press releases it');
    assert.equal(twin.dataset.busySince, busySince);
    assert.ok(twin.querySelector('button').classList.contains('btn-busy'), 'and it looks busy');
    assert.ok(twin._busyTimer, 'with the rest of the backstop');
    assert.equal(isBusy(old), false);
    assert.equal(old._busyTimer, null, 'no timer left behind on the detached one');
});

test('a press still out for the log (no response yet) keeps it: the response lands in it', () => {
    fresh();
    const log = document.getElementById('log-f1');
    const old = document.getElementById('watch');
    seen.add(old);
    markBusy(old);
    loadAsyncView(main(), page(true), IN_PLACE);
    assert.equal(document.getElementById('log-f1'), log, 'the element asyncFetch will render the answer into');
    assert.equal(isBusy(document.getElementById('watch')), true);
});

test('after a quiet move to the next episode the log holds only the player\'s view: kept', () => {
    // next-item-go.js syncPage moves the playing player into a fresh log,
    // which never gets the action view's mark.
    fresh();
    const log = document.getElementById('log-f1');
    log.innerHTML = '<div data-async-view="/assets/action/stream"><video></video></div>';
    const player = log.firstElementChild;
    loadAsyncView(main(), page(true), IN_PLACE);
    assert.equal(document.getElementById('log-f1').firstElementChild, player);
});

test('the archive dialog: a busy ZIP stays ZIP, not the TAR beside it', () => {
    fresh();
    const log = document.getElementById('log-l1');
    log.innerHTML = `<script src="${ACTION}"></script><div class="progress-alert">Archiving</div>`;
    log.setAttribute('data-async-view', '/assets/action');
    const zip = document.querySelector('form.zip');
    seen.add(zip);
    markBusy(zip);
    loadAsyncView(main(), page(true), IN_PLACE);
    assert.equal(document.getElementById('log-l1'), log);
    assert.equal(isBusy(document.querySelector('form.zip')), true);
    assert.equal(isBusy(document.querySelector('form.tar')), false);
});

test('a page that does not render the log again (another file) ends its job', () => {
    fresh();
    startJob();
    loadAsyncView(main(), page(true).replaceAll('log-f1', 'log-f2'), IN_PLACE);
    assert.equal(document.getElementById('log-f1'), null);
    assert.deepEqual(events.filter(([n]) => n === 'async:/assets/action_destroy'), [['async:/assets/action_destroy', 'log-f1']]);
});

test('a log with no job in it is the new page\'s', () => {
    fresh();
    const idle = document.getElementById('log-l1');
    loadAsyncView(main(), page(true), IN_PLACE);
    assert.notEqual(document.getElementById('log-l1'), idle);
    assert.ok(document.getElementById('log-l1').querySelector('.spinner'));
});
