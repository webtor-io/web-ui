import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// A player's restart of a stream whose transcoder session is gone, when the
// page's start form is not this file's (lib/player/stream-restart.js
// restartURL): this page's deep link, past the job cache -- and, where the
// viewer answered the slow-download modal "watch as is" for this run, with
// that answer, or the job asks it again before any player.
const dom = new JSDOM(`<!doctype html><body><div id="host"><script id="s"></script></div>
    <form class="stream-video" action="/stream-video" method="post">
        <input type="hidden" name="resource-id" value="r"><input type="hidden" name="item-id" value="ep2">
    </form></body>`, { url: 'https://webtor.io/r?file=ep2.mkv#action=stream&purge=true&force-slow=true' });
const w = dom.window;
for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'HTMLFormElement', 'Event', 'CustomEvent', 'FormData', 'Node', 'MutationObserver', 'URLSearchParams']) {
    Object.defineProperty(globalThis, k, { value: k === 'window' ? w : w[k], configurable: true, writable: true });
}
const nodeSetTimeout = globalThis.setTimeout;
const timers = new Set();
globalThis.setTimeout = (fn, ms, ...a) => { const t = nodeSetTimeout(fn, ms, ...a); timers.add(t); return t; };
Object.defineProperty(w.document, 'currentScript', { value: w.document.getElementById('s'), configurable: true });
after(() => {
    for (const t of timers) clearTimeout(t);
    w.close();
});

const { restartURL, clearPurgeMarks, PURGE_MARK } = await import('./stream-restart.js');

test('the restart\'s deep link starts the file past the job cache, with the "watch as is" it answered', async () => {
    assert.equal(restartURL('https://webtor.io/r?file=ep1.mkv', 'ep2.mkv', { forceSlow: true }),
        '/r?file=ep2.mkv#action=stream&purge=true&force-slow=true');
    const sent = [];
    const form = w.document.querySelector('form');
    form.addEventListener('submit', (e) => {
        e.preventDefault();
        sent.push(Object.fromEntries(new w.FormData(form)));
    });
    await import('../../app/resource/get.js');
    const [, init] = w.av[0];
    await init.call(w.document.getElementById('host'));
    assert.equal(sent.length, 1);
    assert.equal(sent[0].purge, 'true');
    assert.equal(sent[0]['force-slow'], 'true', 'the modal is not asked again');
    // Marked as the restart's, as on the form path: a start that ends before
    // any player leaves them for the viewer's next press, and the next player
    // takes them off -- the presses after it are ordinary starts, not purges.
    for (const name of ['purge', 'force-slow']) {
        assert.ok(form.querySelector(`input[name="${name}"]`).hasAttribute(PURGE_MARK), `${name} marked`);
    }
    form.requestSubmit();
    assert.equal(sent[1].purge, 'true', 'the next press, no player yet: purged');
    assert.equal(sent[1]['force-slow'], 'true');
    clearPurgeMarks(w.document);
    form.requestSubmit();
    assert.equal(sent[2].purge, undefined, 'a player mounted: an ordinary start');
    assert.equal(sent[2]['force-slow'], undefined);
    sent.length = 0;
    // Without the answer: an ordinary start of the gate.
    w.location.hash = restartURL('https://webtor.io/r?file=ep1.mkv', 'ep2.mkv').split('#')[1];
    await init.call(w.document.getElementById('host'));
    assert.equal(sent.length, 1);
    assert.equal(sent[0].purge, 'true');
    assert.equal(sent[0]['force-slow'], undefined);
});
