// The page URL names a catalog's addon by key (?catalog-base=), never by its
// URL: a configured addon keeps the user's credentials in its path, and the
// page URL reaches Umami, the access log and the login return-url
// (StremioClient.addonKey). End to end in jsdom: what the page reads, and
// every URL it writes into history.
//
// Real: DiscoverApp, StremioClient, preact, the DOM. Faked: `fetch`
// (Cinemeta and one configured addon, a catalog each), window.umami.

import { test, after, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const SECRET = 'debrid-secret';
const ADDON = `https://addon.test/${SECRET}`;
const ADDON_ID = '6f1c2b0e-0000-4000-8000-000000000001';
const CINEMETA_ROW = '6f1c2b0e-0000-4000-8000-000000000002';
const CINEMETA = 'https://v3-cinemeta.strem.io';

const dom = new JSDOM('<!doctype html><html lang="en"><body><div id="root"></div></body></html>', {
    url: 'https://webtor.io/discover',
    pretendToBeVisual: true,
});
const w = dom.window;
globalThis.window = w;
globalThis.document = w.document;
Object.defineProperty(globalThis, 'navigator', { configurable: true, value: w.navigator });
for (const k of ['HTMLElement', 'Event', 'CustomEvent', 'MouseEvent', 'PopStateEvent', 'localStorage', 'sessionStorage']) {
    globalThis[k] = w[k];
}
globalThis.requestAnimationFrame = w.requestAnimationFrame.bind(w);
globalThis.cancelAnimationFrame = w.cancelAnimationFrame.bind(w);
globalThis.__SUPPORTED_LOCALES__ = ['en'];
w.HTMLCanvasElement.prototype.getContext = () => null;
w.scrollTo = () => {};
class NoEventSource {
    static CLOSED = 2;
    constructor() { this.readyState = 0; }
    addEventListener() {}
    close() { this.readyState = NoEventSource.CLOSED; }
}
globalThis.EventSource = NoEventSource;
w.EventSource = NoEventSource;
w.umami = { track: () => {} };

const respond = (body, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const manifest = (id, catalog) => ({ id, name: id, version: '1.0.0', resources: ['catalog'], types: ['movie'], catalogs: [{ type: 'movie', id: catalog, name: catalog }] });
const routes = {
    [`${CINEMETA}/manifest.json`]: manifest('cinemeta', 'top'),
    [`${CINEMETA}/catalog/movie/top.json`]: { metas: [{ id: 'tt1', type: 'movie', name: 'One' }] },
    [`${ADDON}/manifest.json`]: manifest('addon', 'popular'),
    [`${ADDON}/catalog/movie/popular.json`]: { metas: [{ id: 'tt2', type: 'movie', name: 'Two' }] },
};
let fetched = [];
globalThis.fetch = async (url) => {
    const u = String(url);
    fetched.push(u);
    return u in routes ? respond(routes[u]) : respond({}, 404);
};

// Every URL the page writes into history.
let written = [];
for (const m of ['pushState', 'replaceState']) {
    const orig = w.history[m].bind(w.history);
    w.history[m] = (state, title, url) => { if (url != null) written.push(String(url)); return orig(state, title, url); };
}

after(() => { render(null, document.getElementById('root')); w.close(); });

const { h, render } = await import('preact');
const { init } = await import('../i18n.js');
const { DiscoverApp } = await import('./DiscoverApp.jsx');
const { urlKey } = await import('../client.js');
const KEY = urlKey(ADDON);
await init();

const until = async (cond, what, ms = 3000) => {
    const t0 = Date.now();
    while (!cond()) {
        if (Date.now() - t0 > ms) throw new Error(`timed out waiting for ${what}`);
        await new Promise((r) => setTimeout(r, 10));
    }
};
const param = () => new URLSearchParams(w.location.search).get('catalog-base');
const leaked = () => written.filter((u) => decodeURIComponent(u).includes('addon.test'));

// Opens /discover?<query> as the app's bootstrap would: Cinemeta first,
// with no row, then the user's addon with its row id. seeds overrides the
// bootstrap seeds (an addon the wizard just installed has none).
function open(query, seeds = [{ id: '', url: CINEMETA }, { id: ADDON_ID, url: `${ADDON}/manifest.json` }]) {
    w.history.replaceState(null, '', `/discover?${query}`);
    w.localStorage.clear();
    fetched = [];
    written = [];
    const root = document.getElementById('root');
    render(h(DiscoverApp, {
        addonUrls: [CINEMETA, ADDON],
        addonSeeds: seeds,
        hasCustomAddons: true,
    }), root);
    return root;
}

beforeEach(() => render(null, document.getElementById('root')));

test('a link from before the keys, naming the addon by URL, still opens its catalog, and the page writes the key over it', async () => {
    open(new URLSearchParams({ type: 'movie', 'catalog-base': ADDON, 'catalog-id': 'popular' }));
    await until(() => fetched.includes(`${ADDON}/catalog/movie/popular.json`), 'the addon\'s catalog');
    assert.equal(param(), KEY, 'the key in the URL');
    assert.ok(!fetched.includes(`${CINEMETA}/catalog/movie/top.json`), 'not the default one');
    assert.deepEqual(leaked(), []);
});

test('a link naming the addon by key opens its catalog', async () => {
    open(new URLSearchParams({ type: 'movie', 'catalog-base': KEY, 'catalog-id': 'popular' }));
    await until(() => fetched.includes(`${ADDON}/catalog/movie/popular.json`), 'the addon\'s catalog');
    assert.equal(param(), KEY);
});

test('choosing a catalog writes its addon\'s key; Cinemeta has a literal one', async () => {
    const root = open(new URLSearchParams({ type: 'movie' }));
    await until(() => param() === 'cinemeta', 'Cinemeta\'s key for the default catalog');
    const select = root.querySelector('select');
    select.value = `${ADDON}::popular`;
    select.dispatchEvent(new w.Event('change', { bubbles: true }));
    await until(() => fetched.includes(`${ADDON}/catalog/movie/popular.json`), 'the addon\'s catalog');
    assert.equal(param(), KEY);
    assert.deepEqual(leaked(), [], 'no URL the page wrote carries the addon\'s');
    assert.ok(written.length >= 2, 'the page wrote the URL (sync and the choice)');
});

test('an addon with no seed (installed by the wizard this session) still gets a key', async () => {
    const root = open(new URLSearchParams({ type: 'movie' }), [{ id: '', url: CINEMETA }]);
    await until(() => param() === 'cinemeta', 'the default catalog');
    const select = root.querySelector('select');
    select.value = `${ADDON}::popular`;
    select.dispatchEvent(new w.Event('change', { bubbles: true }));
    await until(() => fetched.includes(`${ADDON}/catalog/movie/popular.json`), 'the addon\'s catalog');
    assert.equal(param(), KEY);
    assert.deepEqual(leaked(), []);
});

test('Cinemeta as the user\'s own row is still "cinemeta"', async () => {
    open(new URLSearchParams({ type: 'movie', 'catalog-base': 'cinemeta', 'catalog-id': 'top' }),
        [{ id: CINEMETA_ROW, url: `${CINEMETA}/manifest.json` }, { id: ADDON_ID, url: `${ADDON}/manifest.json` }]);
    await until(() => fetched.includes(`${CINEMETA}/catalog/movie/top.json`), 'Cinemeta\'s catalog');
    assert.equal(param(), 'cinemeta');
});

test('back after choosing a catalog restores the previous one', async () => {
    const root = open(new URLSearchParams({ type: 'movie' }));
    await until(() => param() === 'cinemeta', 'the default catalog');
    const select = root.querySelector('select');
    select.value = `${ADDON}::popular`;
    select.dispatchEvent(new w.Event('change', { bubbles: true }));
    await until(() => param() === KEY, 'the addon\'s key');
    w.history.back();
    await until(() => param() === 'cinemeta' && root.querySelector('select').value === `${CINEMETA}::top`, 'Cinemeta selected again');
    assert.deepEqual(leaked(), []);
});
