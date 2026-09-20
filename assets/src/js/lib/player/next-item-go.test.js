import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// The import chain reaches lib/turnstileAction.js, which reads `window` when
// it loads: a document first, the module after.
const boot = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = boot.window; global.document = boot.window.document;
const { nextStartForm, nextURL, createNextItemGo, PREPARED_MAX_AGE_MS } = await import('./next-item-go.js');

function page() {
    const dom = new JSDOM(`<!doctype html><body>
        <div id="file">
          <form class="stream" action="https://webtor.io/ru/stream-video" method="post" data-async-target="#log-i1">
            <input type="hidden" name="resource-id" value="res"><input type="hidden" name="item-id" value="i1">
            <button type="submit">Watch</button>
          </form>
          <div id="log-i1" data-async-layout="L"></div>
        </div></body>`, { url: 'https://webtor.io/ru/res?file=S01%2Fe01.mkv&file-idx=3#action=stream' });
    global.window = dom.window; global.document = dom.window.document; global.FormData = dom.window.FormData;
    global.CustomEvent = dom.window.CustomEvent;
    return dom.window;
}

test('the start form of the next file: same form, next item, the carried choice', () => {
    const w = page();
    const form = nextStartForm({ itemId: 'i2', path: 'S01/e02.mkv' }, { 'carry-audio-lang': 'en', 'carry-sub': 'off' }, w.document);
    const data = Object.fromEntries(new w.FormData(form).entries());
    assert.deepEqual(data, { 'resource-id': 'res', 'item-id': 'i2', 'carry-audio-lang': 'en', 'carry-sub': 'off' });
    assert.equal(form.getAttribute('data-async-target'), '#log-i1', 'the layout is looked up through it');
    assert.equal(w.document.querySelector('input[name="item-id"]').value, 'i1', 'the form on the page is not touched');
    assert.equal(nextStartForm(null, {}, w.document), null);
});

test('an audio page has an audio form', () => {
    const w = page();
    w.document.querySelector('form').setAttribute('action', 'https://webtor.io/stream-audio');
    assert.ok(nextStartForm({ itemId: 'i2', path: 'a/2.flac' }, {}, w.document));
    w.document.querySelector('form').setAttribute('action', 'https://webtor.io/download-file');
    assert.equal(nextStartForm({ itemId: 'i2', path: 'a/2.flac' }, {}, w.document), null, 'a download form starts no stream');
});

test('the address of the next file: file= replaced, file-idx and the hash gone', () => {
    assert.equal(nextURL('https://webtor.io/ru/res?file=S01%2Fe01.mkv&file-idx=3&pwd=S01#action=stream', 'S01/e02 & more.mkv'),
        '/ru/res?file=S01%2Fe02+%26+more.mkv&pwd=S01');
});

test('prepare once; a stale render is not used; what cannot be quiet falls back', async () => {
    const w = page();
    let t = 1000; let fetches = 0; const events = [];
    const docWith = () => new w.DOMParser().parseFromString('<video class="player" data-resource-title="T"></video>', 'text/html');
    let assigned = null;
    const make = (fetchRender, getToken = async () => '') => createNextItemGo({
        next: { itemId: 'i2', path: 'S01/e02.mkv' }, resourceID: 'res', root: w.document.body,
        getStage: () => null, initPlayer: async () => {}, destroyPlayer: () => {},
        onEvent: (n, d) => events.push([n, d]), fetchRender, getToken, now: () => t,
        navigate: (u) => { assigned = u; },
    });

    const a = make(async () => { fetches++; return docWith(); });
    await Promise.all([a.prepare(), a.prepare()]);
    assert.equal(fetches, 1, 'one background start per file');
    assert.equal(a.isPrepared(), true);
    t += PREPARED_MAX_AGE_MS + 1;
    assert.equal(a.isPrepared(), false, 'its session may be gone');

    // Anonymous viewer who needs a visible Turnstile checkbox: null token.
    const b = make(async () => { throw new Error('must not be asked'); }, async () => null);
    await b.go('auto');
    assert.equal(assigned, '/ru/res?file=S01%2Fe02.mkv#action=stream', 'the ordinary way in');
    assert.deepEqual(events.at(-1)[1].fallback, true);

    // An error card instead of a player: the render resolves to null.
    assigned = null;
    const c = make(async () => null);
    await c.go('button');
    assert.ok(assigned, 'a render that is not a player is shown the visible way');
});
