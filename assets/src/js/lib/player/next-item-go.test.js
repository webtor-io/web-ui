import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

// The import chain reaches lib/turnstileAction.js, which reads `window` when
// it loads: a document first, the module after.
const boot = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = boot.window; global.document = boot.window.document;
const { nextStartForm, nextURL, createNextItemGo, syncPage, canMoveOn, takeFallbackNote, PREPARED_MAX_AGE_MS, NEXT_RENDER_TIMEOUT_MS } = await import('./next-item-go.js');

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
    assert.equal(events.filter(([n]) => n === 'go').at(-1)[1].fallback, true);
    assert.deepEqual(events.filter(([n]) => n === 'loading').map(([, d]) => d.on), [true, false, true],
        'the wait is shown: while fetching, and again until the navigation takes over');

    // An error card instead of a player: the render resolves to null.
    assigned = null;
    const c = make(async () => null);
    await c.go('button');
    assert.ok(assigned, 'a render that is not a player is shown the visible way');
});

test('a cold start is given minutes, not the thirty seconds of a settings restart', async () => {
    const w = page();
    let opts = null;
    const go = createNextItemGo({
        next: { itemId: 'i2', path: 'S01/e02.mkv' }, resourceID: 'res', root: w.document.body,
        getStage: () => null, initPlayer: async () => {}, destroyPlayer: () => {},
        fetchRender: async (form, o) => { opts = o; return null; }, getToken: async () => '', navigate: () => {},
    });
    await go.prepare();
    assert.equal(opts.timeoutMs, NEXT_RENDER_TIMEOUT_MS);
    assert.ok(NEXT_RENDER_TIMEOUT_MS >= 5 * 60 * 1000, 'longer than a warm-up on a thin swarm');
    assert.equal(typeof opts.onProgress, 'function', 'and the wait is narrated');
});

// The page around a playing film, and a fresh #content for episode `n`.
function playingPage() {
    const w = page();
    global.DOMParser = w.DOMParser;
    w.document.body.innerHTML = `
        <div id="content" data-async-layout="L">
          <div id="file"><h2>e01</h2><div id="log-i1"><div class="live"><div class="wt-player-stage"><video></video></div><dialog id="subtitles"></dialog></div></div></div>
          <div id="list" data-async-view="resource/select">list of e01</div>
        </div>`;
    return w;
}
const freshContent = (n) => `<template data-async-fragment="main"><div id="file"><h2>e0${n}</h2><div id="log-i${n}"></div></div><div id="list">list of e0${n}<script src="https://webtor.io/assets/resource/select.js"></script></div></template>`;

test('syncing the page moves the live player into the new card, and runs the view lifecycle around it', async () => {
    const w = playingPage();
    const stage = w.document.querySelector('.wt-player-stage');
    const seen = [];
    w.addEventListener('async:resource/select_destroy', () => seen.push('destroy'));
    w.addEventListener('async:/assets/resource/select', (e) => seen.push('init:' + e.detail.target.id));
    const ok = await syncPage('/ru/res?file=e02', { fetchImpl: async () => ({ ok: true, text: async () => freshContent(2) }) });
    assert.equal(ok, true);
    assert.equal(w.document.querySelector('#file h2').textContent, 'e02');
    assert.equal(w.document.querySelector('.wt-player-stage'), stage, 'the very same stage: the player was moved, not rebuilt');
    assert.equal(stage.closest('[id^="log-"]').id, 'log-i2', 'into the new card\'s log');
    assert.equal(w.document.querySelectorAll('#subtitles').length, 1);
    assert.deepEqual(seen, ['destroy', 'init:list'], 'the old list was told it was going; the new one had its script started');
});

test('two syncs in flight: the newer one wins whatever order the answers come in', async () => {
    const w = playingPage();
    let releaseOld;
    const oldAnswer = new Promise((r) => { releaseOld = r; });
    const first = syncPage('/ru/res?file=e02', { fetchImpl: async () => { await oldAnswer; return { ok: true, text: async () => freshContent(2) }; } });
    const second = syncPage('/ru/res?file=e03', { fetchImpl: async () => ({ ok: true, text: async () => freshContent(3) }) });
    assert.equal(await second, true);
    releaseOld();
    assert.equal(await first, false, 'overtaken: it must not put episode 2\'s card under episode 3\'s picture');
    assert.equal(w.document.querySelector('#file h2').textContent, 'e03');
});

test('a page without the start form or #content has no "next"', () => {
    const w = page();
    assert.equal(canMoveOn(w.document), false, 'no #content');
    w.document.body.insertAdjacentHTML('beforeend', '<div id="content"></div>');
    assert.equal(canMoveOn(w.document), true);
    w.document.querySelector('form').remove();
    assert.equal(canMoveOn(w.document), false, 'no start form');
});

test('a player that is up is not a failed mount, whatever the housekeeping after it does', async () => {
    const w = page();
    w.document.body.insertAdjacentHTML('beforeend', '<div id="content"></div><div class="host"><div class="wt-player-stage"></div></div>');
    const stage = w.document.querySelector('.wt-player-stage');
    const docWith = () => new w.DOMParser().parseFromString('<video class="player" data-resource-title="T"></video>', 'text/html');
    let navigated = null; const events = [];
    // The housekeeping throws: a listener of player_replaced that blows up is
    // the realistic case, simulated here through dispatchEvent itself.
    const realDispatch = w.dispatchEvent.bind(w);
    w.dispatchEvent = (e) => { if (e.type === 'player_replaced') throw new Error('boom'); return realDispatch(e); };
    const go = createNextItemGo({
        next: { itemId: 'i2', path: 'S01/e02.mkv' }, resourceID: 'res', root: w.document.body,
        getStage: () => stage, initPlayer: async () => {}, destroyPlayer: () => {},
        onEvent: (n, d) => events.push([n, d]), fetchRender: async () => docWith(), getToken: async () => '',
        navigate: (u) => { navigated = u; },
    });
    await go.go('auto');
    w.dispatchEvent = realDispatch;
    assert.equal(navigated, null, 'no reload: the player mounted');
    assert.equal(events.filter(([n]) => n === 'go').at(-1)[1].fallback, false);
});

test('a real failure to mount reloads, and leaves the reason for the next page', async () => {
    const w = page();
    w.document.body.insertAdjacentHTML('beforeend', '<div class="host"><div class="wt-player-stage"></div></div>');
    const stage = w.document.querySelector('.wt-player-stage');
    const docWith = () => new w.DOMParser().parseFromString('<video class="player"></video>', 'text/html');
    let navigated = null;
    const go = createNextItemGo({
        next: { itemId: 'i2', path: 'S01/e02.mkv' }, resourceID: 'res', root: w.document.body,
        getStage: () => stage, initPlayer: async () => { throw new Error('hls exploded'); }, destroyPlayer: () => {},
        fetchRender: async () => docWith(), getToken: async () => '', navigate: (u) => { navigated = u; },
    });
    const err = console.error; console.error = () => {};
    await go.go('auto');
    console.error = err;
    assert.ok(navigated, 'the old player is gone: a reload is the lesser evil');
    const note = takeFallbackNote(w.sessionStorage);
    assert.match(note.reason, /mount-failed: hls exploded/);
    assert.equal(takeFallbackNote(w.sessionStorage), null, 'reported once');
});
