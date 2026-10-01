import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
const boot = new JSDOM('', { url: 'https://webtor.io/res?file=movie.mkv' });
global.window = boot.window; global.document = boot.window.document;
const { createBackgroundRestart, restartStartForm, restartFields } = await import('./background-restart.js');
const { OPTIN_KEY, CACHE_KEY, AUDIO_CACHE_KEY, rememberFallback } = await import('./decode-declaration.js');

function page({ tag = 'video' } = {}) {
    const dom = new JSDOM(`<form action="/stream-video" data-async-target="#log">
      <input name="resource-id" value="res"><input name="item-id" value="previous"></form>
      <div id="log"><div id="wrapper"><div class="wt-player-stage"></div><dialog id="subtitles" data-item-id="current"></dialog></div></div>` , { url: 'https://webtor.io/res?file=movie.mkv' });
    const w = dom.window;
    global.window = w; global.document = w.document; global.CustomEvent = w.CustomEvent;
    w.EventSource = class {};
    const video = w.document.createElement(tag);
    Object.assign(video.dataset, { resourceId: 'res', path: 'movie.mkv', itemId: 'current', decode: 'hevc10,aac51' });
    const stage = w.document.querySelector('.wt-player-stage');
    stage.append(video);
    let state = { at: 1397, play: true, settled: true, grace: 'continue' };
    const mounted = [], visible = [], loading = [];
    let failures = 0;
    const create = (options = {}) => createBackgroundRestart({
        video, root: stage.parentNode, win: w, doc: w.document,
        getStage: () => stage, getAspectRatio: () => '16 / 9', getState: () => state,
        getToken: async () => '',
        initPlayer: async (host, opts) => mounted.push({ host, opts }),
        destroyPlayer: ({ keepStage }) => { assert.equal(keepStage, true); video.remove(); },
        visible: (...args) => visible.push(args), onLoading: (v) => loading.push(v), onFailure: () => failures++,
        ...options,
    });
    const render = () => new w.DOMParser().parseFromString(`<div><${tag} class="player" data-item-id="current"></${tag}><dialog id="subtitles"></dialog><script>throw 1</script></div>`, 'text/html');
    return { w, stage, video, create, render, mounted, visible, loading, failures: () => failures, setState: (v) => state = { ...state, ...v } };
}

test('restart re-addresses the current file after Next without changing the page form; audio uses audio endpoint', () => {
    const p = page({ tag: 'audio' });
    const f = restartStartForm(p.video, p.stage.parentNode, p.w, p.w.document);
    assert.equal(f.action, 'https://webtor.io/stream-audio');
    assert.equal(f.querySelector('[name="item-id"]').value, 'current');
    assert.equal(p.w.document.querySelector('form [name="item-id"]').value, 'previous');
    p.video.dataset.resourceId = 'different';
    assert.equal(restartStartForm(p.video, p.stage.parentNode, p.w, p.w.document), null);
});

test('a background restart retains stage, latest position and pause intent; no visible start or scripts', async (t) => {
    const p = page();
    t.after(() => p.w.dispatchEvent(new p.w.CustomEvent('player_ready')));
    let resolve, request;
    const r = p.create({ fetchRender: (form, opts) => { request = { form, opts }; return new Promise((r) => resolve = r); } });
    const first = r.start({ position: { at: 1397, play: true } });
    const duplicate = r.start();
    await Promise.resolve();
    assert.equal(request.opts.timeoutMs, 10 * 60 * 1000, 'fresh stream jobs retain their server deadline window');
    assert.equal(request.opts.fields.purge, 'true');
    assert.equal(request.form.isConnected, false);
    assert.equal(p.video.isConnected, true, 'old buffer remains usable while fetching');
    p.setState({ at: 1410 });
    p.video.dispatchEvent(new p.w.Event('pause'));
    resolve(p.render());
    await Promise.all([first, duplicate]);
    assert.equal(p.mounted.length, 1);
    assert.equal(p.mounted[0].opts.stage, p.stage);
    assert.equal(p.mounted[0].opts.restartState.at, 1410);
    assert.equal(p.mounted[0].opts.restartState.play, false);
    assert.equal(p.w.document.querySelectorAll('#subtitles').length, 1);
    assert.equal(p.stage.parentNode.querySelector('script'), null);
    assert.equal(p.visible.length, 0);
    assert.deepEqual(p.loading, [true, false]);
});

test('leaving cancels the job and ignores a late render from the old file', async () => {
    const p = page(); let resolve, signal;
    const r = p.create({ fetchRender: (f, opts) => { signal = opts.signal; return new Promise((r) => resolve = r); } });
    const pending = r.start(); await Promise.resolve();
    r.dispose();
    assert.equal(signal.aborted, true);
    resolve(p.render()); await pending;
    assert.equal(p.mounted.length, 0);
    assert.equal(p.video.isConnected, true);
    assert.equal(p.failures(), 0);
});

test('failed network stays in the player; interaction and non-player renders use the visible path', async () => {
    const p = page();
    await p.create({ fetchRender: async () => null }).start();
    assert.equal(p.failures(), 1);
    assert.equal(p.visible.length, 0);
    await p.create({ getToken: async () => null }).start();
    assert.equal(p.visible.length, 1);
    await p.create({ fetchRender: async (f, opts) => { opts.onVisibleRequired(); return null; } }).start();
    assert.equal(p.visible.length, 2);
    assert.equal(p.video.isConnected, true);
});

test('a fixed seek/decoder position survives detach and a failed retry; play intent comes from the retry', async (t) => {
    const p = page(); t.after(() => p.w.dispatchEvent(new p.w.CustomEvent('player_ready')));
    let attempts = 0;
    const r = p.create({ fetchRender: async () => ++attempts === 1 ? null : p.render() });
    p.setState({ at: 0 });
    await r.start({ position: { at: 1397, play: false }, fixedPosition: true, fallback: { reason: 'decode_error', cls: 'aac51' } });
    await r.start({ position: { at: 0, play: true } });
    assert.equal(p.mounted[0].opts.restartState.at, 1397);
    assert.equal(p.mounted[0].opts.restartState.play, true);
});

test('explicit fallback fields keep the codec policy; the Dolby drop keeps video and AAC, restart is purged', () => {
    const p = page();
    p.w.localStorage.setItem(OPTIN_KEY, 'on');
    p.w.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: p.w.navigator.userAgent, tokens: ['hevc10', 'aac51', 'ac3', 'ec3'], at: Date.now() }));
    p.w.localStorage.setItem(AUDIO_CACHE_KEY, JSON.stringify({ ua: p.w.navigator.userAgent, tokens: ['aac51', 'ac3', 'ec3'], at: Date.now() }));
    rememberFallback(p.w, { resourceId: 'res', itemId: 'current', cls: 'dolby' });
    p.video.dataset.offerAnswered = 'continue-slow';
    const fields = restartFields(p.video, p.stage.parentNode, { reason: 'media_error', cls: 'dolby' }, p.w);
    assert.equal(fields.decode, 'hevc10,aac51');
    assert.equal(fields['decode-fallback'], 'media_error');
    assert.equal(fields['decode-class'], 'dolby');
    assert.equal(fields['force-slow'], 'true');
});

test('embed uses its original settings through the same off-page form transport', () => {
    const p = page(); p.w._embedSettings = { id: 'res', path: 'movie.mkv', features: { share: false } };
    const f = restartStartForm(p.video, p.stage.parentNode, p.w, p.w.document);
    assert.equal(f.action, p.w.location.href);
    assert.deepEqual(JSON.parse(new p.w.FormData(f).get('settings')), p.w._embedSettings);
    assert.equal(f.getAttribute('data-async-target'), null);
});
