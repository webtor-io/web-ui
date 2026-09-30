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
const dom = new JSDOM(`<!doctype html><body><div id="host"><script id="s"></script>
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
const [, init] = w.av[0];

// start: the view's init, then the job's render of the player.
async function start() {
    const host = w.document.getElementById('host');
    const progress = host.querySelector('.progress-alert');
    progress.classList.remove('hidden');
    await init.call(host);
    const src = sources.at(-1);
    src.onmessage({ data: JSON.stringify({ level: 'rendertemplate', tag: 'rendering action', body: '<div class="rendered-player"></div>' }) });
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
