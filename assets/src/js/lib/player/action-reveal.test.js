import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// app/action.js (here, not next to it: every file in app/ is a webpack
// entry): the job's log, and the player it renders -- kept out of
// sight until it can play (player_ready), or until it has something the
// viewer must see before that (player_show: the stream restart's card,
// lib/player/Player.jsx). Chrome, 2026-09-30 (scenario 9b): a restarted job
// whose new session was dead at its first request never got to canplay; its
// card sat in the hidden render, and the viewer read "waiting for the player"
// for minutes.
const dom = new JSDOM(`<!doctype html><body><form id="watch" action="/stream-video" method="post" data-async-target="#host"><button type="submit">Watch</button></form><div id="host"><script id="s"></script>
    <div class="progress-alert" data-async-progress-log="/log/1"><div class="log-target"></div></div>
    </div></body>`, { url: 'https://webtor.io/r' });
const w = dom.window;
// The job's log is an EventSource; this one is fed by hand.
const sources = [];
class FakeEventSource {
    constructor(url) { this.url = url; this.onmessage = null; sources.push(this); }
    close() {}
}
w.EventSource = FakeEventSource;
for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'Event', 'CustomEvent', 'Node', 'MutationObserver', 'EventSource']) {
    Object.defineProperty(globalThis, k, { value: k === 'window' ? w : w[k], configurable: true, writable: true });
}
Object.defineProperty(w.document, 'currentScript', { value: w.document.getElementById('s'), configurable: true });
after(() => w.close());

await import('../../app/action.js');
const [, init, destroy] = w.av[0];

// start: the view's init, then the job's render of the player.
async function start() {
    const host = w.document.getElementById('host');
    const progress = host.querySelector('.progress-alert');
    progress.classList.remove('hidden');
    await init.call(host);
    const src = sources.at(-1);
    src.onmessage({ data: JSON.stringify({ level: 'rendertemplate', tag: 'rendering action', body: '<div class="rendered-player"><video></video></div>' }) });
    const el = Array.from(host.querySelectorAll('.rendered-player')).at(-1).parentElement;
    return { progress, el };
}

test('the rendered player waits out of sight under the log until it can play', async () => {
    const { progress, el } = await start();
    assert.equal(el.classList.contains('hidden'), true, 'hidden while the player loads');
    assert.equal(progress.classList.contains('hidden'), false, 'the log says what is going on');
    w.dispatchEvent(new w.CustomEvent('player_ready'));
    assert.equal(el.classList.contains('hidden'), false);
    assert.equal(progress.classList.contains('hidden'), true);
});

test('a player with a card before its first frame is shown: the card, not the log', async () => {
    const { progress, el } = await start();
    assert.equal(el.classList.contains('hidden'), true);
    w.dispatchEvent(new w.CustomEvent('player_show'));
    assert.equal(el.classList.contains('hidden'), false, 'the player -- and its card -- in sight');
    assert.equal(progress.classList.contains('hidden'), true, 'one message: the log goes');
    // canplay after all (the card's own restart, a stall's reload): nothing
    // more to do.
    w.dispatchEvent(new w.CustomEvent('player_ready'));
    assert.equal(el.classList.contains('hidden'), false);
});


test('preparation survives job close until media is ready, and ends on teardown', async () => {
    const { progress } = await start();
    assert.ok('transferPreparing' in progress.dataset);
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'close' }) });
    assert.ok('transferPreparing' in progress.dataset, 'job done, player not ready yet');
    w.dispatchEvent(new w.CustomEvent('player_ready'));
    assert.equal('transferPreparing' in progress.dataset, false);
    await start();
    destroy.call(w.document.getElementById('host'));
    assert.equal('transferPreparing' in progress.dataset, false);
});

test('failed preparation does not latch viewer presence', async () => {
    const host = w.document.getElementById('host');
    await init.call(host);
    const progress = host.querySelector('.progress-alert');
    assert.ok('transferPreparing' in progress.dataset);
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'error', tag: 'load', message: 'failed' }) });
    assert.equal('transferPreparing' in progress.dataset, false);
    destroy.call(host);
});

// The Watch button that started the job (lib/actionBusy.js) stays busy until
// the player is shown -- not when its template arrives: in between it buffers
// out of sight, and a press there replayed the job over it.
const { markBusy, isBusy, clearBusy } = await import('../actionBusy.js');
const watch = () => w.document.getElementById('watch');

test('the button stays busy through the rendered template and the job\'s close, until the player is shown', async () => {
    markBusy(watch(), { timeoutMs: 0 });
    await start();
    assert.equal(isBusy(watch()), true, 'a rendered player is not yet on screen');
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'close' }) });
    assert.equal(isBusy(watch()), true, 'close follows rendertemplate in every job');
    w.dispatchEvent(new w.CustomEvent('player_ready'));
    assert.equal(isBusy(watch()), false, 'released when the player can play');
    destroy.call(w.document.getElementById('host'));
});

test('a player that never says it can play releases the button after the backstop', async (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    markBusy(watch(), { timeoutMs: 0 });
    await start();
    t.mock.timers.tick(29 * 1000);
    assert.equal(isBusy(watch()), true);
    t.mock.timers.tick(1000);
    assert.equal(isBusy(watch()), false, '30 s after the template');
    destroy.call(w.document.getElementById('host'));
});

test('a job that renders nothing to wait for (a download card) is released by its close', async () => {
    const host = w.document.getElementById('host');
    markBusy(watch(), { timeoutMs: 0 });
    await init.call(host);
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'custom', tag: 'card', body: '<div>card</div>' }) });
    assert.equal(isBusy(watch()), true);
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'close' }) });
    assert.equal(isBusy(watch()), false);
    destroy.call(host);
    clearBusy(watch());
});

// What the job produces renders under the buttons, below the fold on most
// screens (2026-10-14): app/action.js brings it into view
// (lib/stickyStatus.js reveal) -- each step, card and verdict, the shown
// player, a refusal; never the per-second status ticks.
const { notePress } = await import('../stickyStatus.js');
const scrolls = [];
w.innerHeight = 844;
w.matchMedia = () => ({ matches: false });
w.scrollTo = (o) => scrolls.push(o);
const below = (el) => { el.getBoundingClientRect = () => ({ top: 775, bottom: 869, height: 94 }); };

test('each new step brings the log into view; the status ticks do not', async () => {
    const host = w.document.getElementById('host');
    notePress(w);
    await init.call(host);
    const progress = host.querySelector('.progress-alert');
    below(progress);
    scrolls.length = 0;
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'inprogress', tag: 'warmup', message: 'warming up' }) });
    assert.equal(scrolls.length, 1, 'a step');
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'statusupdate', tag: 'warmup', status: '37%' }) });
    assert.equal(scrolls.length, 1, 'not a tick');
    destroy.call(host);
});

test('the player, once shown, is brought into view', async () => {
    notePress(w);
    const { el } = await start();
    below(el);
    scrolls.length = 0;
    w.dispatchEvent(new w.CustomEvent('player_ready'));
    assert.equal(scrolls.length, 1);
    destroy.call(w.document.getElementById('host'));
});

test('the download card focuses its "start download"', async () => {
    const host = w.document.getElementById('host');
    await init.call(host);
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'custom', tag: 'dl', body: '<a data-transfer-download href="/f" download>start</a>' }) });
    assert.equal(w.document.activeElement, host.querySelector('[data-transfer-download]'));
    destroy.call(host);
});

test('a refused start (no job log) brings its card into view', async () => {
    const refused = w.document.createElement('div');
    refused.innerHTML = '<div class="progress-alert" role="alert"><pre>refused</pre></div>';
    w.document.body.appendChild(refused);
    notePress(w);
    below(refused.firstElementChild);
    scrolls.length = 0;
    await init.call(refused);
    assert.equal(scrolls.length, 1);
    refused.remove();
});

test('an old job whose log was replaced by a new press releases nothing', async () => {
    const host = w.document.getElementById('host');
    const saved = host.innerHTML;
    markBusy(watch(), { timeoutMs: 0 });
    await start();
    // Download pressed into the same #log: Turnstile's step replaces the log
    // before this view is torn down, and Download is the busy one now.
    const dl = w.document.createElement('form');
    dl.setAttribute('action', '/download-file');
    dl.setAttribute('data-async-target', '#host');
    dl.innerHTML = '<button type="submit">Download</button>';
    w.document.body.appendChild(dl);
    markBusy(dl, { timeoutMs: 0 });
    host.replaceChildren(w.document.createElement('div'));
    w.dispatchEvent(new w.CustomEvent('player_ready'));
    assert.equal(isBusy(dl), true, 'the old player\'s canplay is not the new job\'s end');
    clearBusy(dl);
    dl.remove();
    host.innerHTML = saved;
});

test('the download card takes no focus from a field the viewer is typing in, nor after they scrolled away', async () => {
    const host = w.document.getElementById('host');
    const search = w.document.createElement('input');
    w.document.body.appendChild(search);
    notePress(w);
    await init.call(host);
    search.focus();
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'custom', tag: 'dl', body: '<a data-transfer-download href="/f" download>start</a>' }) });
    assert.equal(w.document.activeElement, search, 'typing goes on where it was');
    destroy.call(host);
    search.blur();
    notePress(w);
    await init.call(host);
    w.dispatchEvent(new w.WheelEvent('wheel', { deltaY: 50 }));
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'custom', tag: 'dl2', body: '<a data-transfer-download href="/f" download>start</a>' }) });
    assert.notEqual(w.document.activeElement, host.querySelector('[data-transfer-download]'), 'scrolled away: left alone');
    destroy.call(host);
    search.remove();
});

test('an image preview is brought into view once it has loaded (no height before)', async () => {
    const host = w.document.getElementById('host');
    notePress(w);
    await init.call(host);
    scrolls.length = 0;
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'rendertemplate', tag: 'img', body: '<div class="wt-preview-mount"><img class="wt-preview-image" src="/p.jpg"></div>' }) });
    const el = host.querySelector('.wt-preview-mount').parentElement;
    below(el);
    host.querySelector('img').dispatchEvent(new w.Event('load'));
    assert.equal(scrolls.length, 1);
    destroy.call(host);
});

test('a newer press into the same log during its request is not released by the old job', async () => {
    // Signed in: no Turnstile step replaces the log at the press, so the old
    // job's log is still there while Download's POST is out.
    const host = w.document.getElementById('host');
    markBusy(watch(), { timeoutMs: 0 });
    await start();
    const dl = w.document.createElement('form');
    dl.setAttribute('action', '/download-file');
    dl.setAttribute('data-async-target', '#host');
    dl.innerHTML = '<button type="submit">Download</button>';
    w.document.body.appendChild(dl);
    markBusy(dl, { timeoutMs: 0 });
    w.dispatchEvent(new w.CustomEvent('player_ready'));
    assert.equal(isBusy(dl), true, 'Watch\'s player is not Download\'s end');
    clearBusy(dl);
    dl.remove();
    destroy.call(host);
});

test('in the archive dialog the "start download" takes focus from the dialog\'s own ✕', async () => {
    const dialog = w.document.createElement('dialog');
    dialog.setAttribute('open', '');
    dialog.innerHTML = '<button id="x">✕</button><div id="dlog"><div class="progress-alert" data-async-progress-log="/l2"><div class="log-target"></div></div></div>';
    w.document.body.appendChild(dialog);
    const dlog = w.document.getElementById('dlog');
    notePress(w);
    await init.call(dlog);
    w.document.getElementById('x').focus();
    sources.at(-1).onmessage({ data: JSON.stringify({ level: 'custom', tag: 'dl', body: '<a data-transfer-download href="/f" download>start</a>' }) });
    assert.equal(w.document.activeElement, dlog.querySelector('[data-transfer-download]'));
    destroy.call(dlog);
    dialog.remove();
});
