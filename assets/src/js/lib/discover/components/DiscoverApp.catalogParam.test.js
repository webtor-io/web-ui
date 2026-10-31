// The page URL names a catalog's addon by key (?catalog-base=), never by its
// URL: a configured addon keeps the user's credentials in its path, and the
// page URL reaches Umami, the access log and the login return-url
// (StremioClient.addonKey). End to end in jsdom: what the page reads, and
// every URL it writes into history. Below them, the catalog's genre / year
// filter (genres.js): what it requests, writes and restores.
//
// Real: DiscoverApp, StremioClient, preact, the DOM. Faked: `fetch`
// (Cinemeta and one configured addon, a catalog each; a third addon with
// Cinemeta-shaped genre and year catalogs), window.umami, <dialog>.

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
w.HTMLDialogElement.prototype.showModal = function showModal() { this.open = true; };
w.HTMLDialogElement.prototype.close = function close() { this.open = false; };
w.scrollTo = () => {};
class NoEventSource {
    static CLOSED = 2;
    constructor() { this.readyState = 0; }
    addEventListener() {}
    close() { this.readyState = NoEventSource.CLOSED; }
}
globalThis.EventSource = NoEventSource;
w.EventSource = NoEventSource;
let tracked = [];
w.umami = { track: (name, data) => { tracked.push([name, data]); } };

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
function open(query, seeds = [{ id: '', url: CINEMETA }, { id: ADDON_ID, url: `${ADDON}/manifest.json` }], addonUrls = [CINEMETA, ADDON]) {
    w.history.replaceState(null, '', `/discover?${query}`);
    w.localStorage.clear();
    fetched = [];
    written = [];
    tracked = [];
    const root = document.getElementById('root');
    render(null, root); // a second open in one test mounts afresh
    render(h(DiscoverApp, {
        addonUrls,
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

// --- Genre and year filter ---
//
// META's catalogs are Cinemeta's shapes: an optional genre extra (Popular),
// a required one whose options are years (New), a required one that is
// not (left alone), and a series type with one catalog in the legacy
// extraSupported/genres form. t() answers with the key, so a genre's label
// is genre.<slug>.

const META = 'https://meta.test';
const META_KEY = urlKey(META);
const page = (prefix, n, extra = {}) => ({ metas: Array.from({ length: n }, (_, i) => ({ id: `tt${prefix}${i}`, type: 'movie', name: `${prefix} ${i}`, ...extra })) });
Object.assign(routes, {
    [`${META}/manifest.json`]: {
        id: 'meta', name: 'meta', version: '1.0.0', resources: ['catalog'], types: ['movie', 'series'],
        catalogs: [
            { type: 'movie', id: 'top', name: 'Popular', extra: [{ name: 'genre', options: ['Drama', 'Action', 'Sci-Fi'] }, { name: 'skip' }] },
            { type: 'movie', id: 'year', name: 'New', extra: [{ name: 'genre', isRequired: true, options: ['2026', '2025'] }, { name: 'skip' }] },
            { type: 'movie', id: 'anime', name: 'Anime', extra: [{ name: 'genre', isRequired: true, options: ['Shonen', 'Seinen'] }] },
            { type: 'series', id: 'top', name: 'Popular', extraSupported: ['genre', 'skip'], genres: ['Drama', 'Reality-TV'] },
        ],
    },
    [`${META}/catalog/movie/top.json`]: page('Top', 2),
    [`${META}/catalog/movie/top/genre=Action.json`]: page('Act', 2, { genres: ['Action', 'Sci-Fi', 'Science Fiction', 'Anime', 'Drama'] }),
    [`${META}/catalog/movie/top/genre=Action&skip=2.json`]: page('More', 1),
    [`${META}/catalog/movie/top/genre=Drama.json`]: page('Dra', 1),
    [`${META}/catalog/movie/year/genre=2026.json`]: page('New', 1),
    [`${META}/catalog/movie/year/genre=2025.json`]: page('Old', 1),
    [`${META}/catalog/movie/anime.json`]: page('Ani', 1),
    [`${META}/catalog/series/top.json`]: { metas: [{ id: 'ttSer0', type: 'series', name: 'Ser 0', genres: ['Drama', 'Reality-TV'] }] },
    // No genres in this meta: the episodes view rebuilt on back must carry the card's.
    [`${CINEMETA}/meta/series/ttSer0.json`]: { meta: { id: 'ttSer0', type: 'series', name: 'Ser 0', videos: [{ id: 'ttSer0:1:1', season: 1, episode: 1, title: 'Pilot' }] } },
    [`${META}/catalog/series/top/genre=Reality-TV.json`]: page('Real', 1),
    [`${CINEMETA}/meta/movie/ttTop0.json`]: { meta: { id: 'ttTop0', type: 'movie', name: 'Top 0', genres: ['Drama', 'Anime'] } },
    [`${CINEMETA}/meta/series/ttSer1.json`]: { meta: { id: 'ttSer1', type: 'series', name: 'Ser 1', genres: ['Western'], videos: [{ id: 'ttSer1:1:1', season: 1, episode: 1 }] } },
});

const openMeta = (params) => open(new URLSearchParams(params), [{ id: '', url: META }], [META]);
const urlParam = (k) => new URLSearchParams(w.location.search).get(k);
const genreSelect = (root) => root.querySelector('select[aria-label^="genre."]');
const values = (sel) => [...sel.options].map((o) => o.value);
const choose = (sel, value) => {
    sel.value = value;
    sel.dispatchEvent(new w.Event('change', { bubbles: true }));
};
const shows = (root, name) => [...root.querySelectorAll('.w-card-title')].some((el) => el.textContent === name);
const loadMore = (root) => [...root.querySelectorAll('button')].find((b) => b.textContent === 'discover.loadMore');

test('a genre is the request\'s extra, Load more keeps it, the URL carries it, Umami hears it', async () => {
    const root = openMeta({ type: 'movie' });
    await until(() => fetched.includes(`${META}/catalog/movie/top.json`) && genreSelect(root), 'Popular and its genre select');
    assert.equal(root.querySelector('select'), root.querySelectorAll('select')[0]);
    assert.notEqual(root.querySelector('select'), genreSelect(root), 'the catalog select comes first');
    assert.deepEqual(values(genreSelect(root)), ['', 'Action', 'Drama', 'Sci-Fi'], '"All" first, then by label');
    assert.equal(genreSelect(root).options[0].textContent, 'genre.all');
    choose(genreSelect(root), 'Action');
    await until(() => fetched.includes(`${META}/catalog/movie/top/genre=Action.json`), 'the Action page');
    await until(() => urlParam('genre') === 'Action' && shows(root, 'Act 0'), 'genre=Action in the URL, its items shown');
    assert.deepEqual(tracked.filter(([n]) => n === 'discover-genre-selected'), [['discover-genre-selected', { type: 'movie', catalog: 'top', genre: 'Action' }]]);
    loadMore(root).click();
    await until(() => fetched.includes(`${META}/catalog/movie/top/genre=Action&skip=2.json`), 'the next Action page');
    await until(() => shows(root, 'More 0'), 'the next page appended');
    assert.equal(urlParam('genre'), 'Action');
});

test('back and forward restore the genre', async () => {
    const root = openMeta({ type: 'movie' });
    await until(() => genreSelect(root) && shows(root, 'Top 0'), 'Popular');
    choose(genreSelect(root), 'Action');
    await until(() => urlParam('genre') === 'Action' && shows(root, 'Act 0'), 'Action');
    w.history.back();
    await until(() => genreSelect(root).value === '' && shows(root, 'Top 0') && !shows(root, 'Act 0'), 'all genres again');
    assert.equal(urlParam('genre'), null);
    w.history.forward();
    await until(() => genreSelect(root).value === 'Action' && shows(root, 'Act 0'), 'Action again');
    assert.equal(urlParam('genre'), 'Action');
});

test('another catalog drops the genre: a year catalog\'s newest year, none where the extra is not a year list', async () => {
    const root = openMeta({ type: 'movie' });
    await until(() => genreSelect(root) && shows(root, 'Top 0'), 'Popular');
    choose(genreSelect(root), 'Action');
    await until(() => urlParam('genre') === 'Action', 'Action');

    choose(root.querySelector('select'), `${META}::year`);
    await until(() => fetched.includes(`${META}/catalog/movie/year/genre=2026.json`) && genreSelect(root)?.value === '2026', 'New, at its newest year');
    assert.equal(urlParam('genre'), null, 'the default year stays out of the URL: a saved link keeps meaning the newest');
    assert.equal(genreSelect(root).getAttribute('aria-label'), 'genre.yearLabel');
    assert.deepEqual(values(genreSelect(root)), ['2026', '2025'], 'no "All", the manifest\'s order');
    choose(genreSelect(root), '2025');
    await until(() => fetched.includes(`${META}/catalog/movie/year/genre=2025.json`) && urlParam('genre') === '2025', '2025');

    choose(root.querySelector('select'), `${META}::anime`);
    await until(() => fetched.includes(`${META}/catalog/movie/anime.json`) && shows(root, 'Ani 0'), 'Anime, requested as before');
    assert.equal(genreSelect(root), null, 'no select for a required genre that is not a year');
    assert.equal(urlParam('genre'), null);

    choose(root.querySelector('select'), `${META}::top`);
    await until(() => genreSelect(root)?.value === '' && shows(root, 'Top 0'), 'Popular, all genres');
    assert.equal(urlParam('genre'), null);
});

test('links: a genre, an unknown genre, a year, an unknown year', async () => {
    let root = openMeta({ type: 'movie', 'catalog-base': META_KEY, 'catalog-id': 'top', genre: 'Drama' });
    await until(() => fetched.includes(`${META}/catalog/movie/top/genre=Drama.json`), 'the Drama page');
    await until(() => genreSelect(root)?.value === 'Drama', 'Drama selected');
    assert.ok(!fetched.includes(`${META}/catalog/movie/top.json`), 'not all genres first');

    root = openMeta({ type: 'movie', 'catalog-base': META_KEY, 'catalog-id': 'top', genre: 'Bogus' });
    await until(() => fetched.includes(`${META}/catalog/movie/top.json`) && urlParam('genre') === null, 'all genres, and the URL drops it');

    root = openMeta({ type: 'movie', 'catalog-base': META_KEY, 'catalog-id': 'year', genre: '2025' });
    await until(() => fetched.includes(`${META}/catalog/movie/year/genre=2025.json`), 'the 2025 page');
    assert.ok(!fetched.includes(`${META}/catalog/movie/year/genre=2026.json`));

    root = openMeta({ type: 'movie', 'catalog-base': META_KEY, 'catalog-id': 'year', genre: '1999' });
    await until(() => fetched.includes(`${META}/catalog/movie/year/genre=2026.json`) && genreSelect(root)?.value === '2026', 'the newest year instead');
    await until(() => urlParam('genre') === null, 'the URL drops it');
});

test('a type with one catalog still gets its genre select (legacy manifest form)', async () => {
    const root = openMeta({ type: 'series' });
    await until(() => fetched.includes(`${META}/catalog/series/top.json`) && genreSelect(root), 'the series catalog and its select');
    assert.equal(root.querySelectorAll('select').length, 1, 'no catalog select, the genre one alone');
    assert.deepEqual(values(genreSelect(root)), ['', 'Drama', 'Reality-TV']);
    choose(genreSelect(root), 'Reality-TV');
    await until(() => fetched.includes(`${META}/catalog/series/top/genre=Reality-TV.json`), 'the Reality-TV page');
});

test('watchlist and search leave the genre out of the URL', async () => {
    let root = openMeta({ type: 'movie' });
    await until(() => genreSelect(root) && shows(root, 'Top 0'), 'Popular');
    choose(genreSelect(root), 'Action');
    await until(() => urlParam('genre') === 'Action', 'Action');
    root.querySelector('button[aria-label="discover.watchlist.label"]').click();
    await until(() => urlParam('watchlist') === '1' && urlParam('genre') === null, 'the watchlist, no genre');
    assert.equal(genreSelect(root), null);

    root = openMeta({ search: 'matrix', genre: 'Action' });
    await until(() => urlParam('search') === 'matrix' && urlParam('genre') === null, 'search, no genre');
    assert.equal(genreSelect(root), null);
});

test('the modal shows a card\'s genres, and a movie\'s /meta fills them in', async () => {
    const root = openMeta({ type: 'movie' });
    await until(() => genreSelect(root) && shows(root, 'Top 0'), 'Popular');
    const card = (name) => [...root.querySelectorAll('.w-card-title')].find((el) => el.textContent === name).closest('.group');
    const header = () => root.querySelector('dialog')?.textContent || '';

    // No genres on the card: the line is left out, /meta brings them.
    assert.equal(card('Top 0').querySelector('p.line-clamp-1'), null);
    card('Top 0').click();
    await until(() => header().includes('genre.drama, Anime'), 'the meta\'s genres in the modal');

    choose(genreSelect(root), 'Action');
    await until(() => shows(root, 'Act 0'), 'Action');
    assert.equal(card('Act 0').querySelector('p.line-clamp-1').textContent, 'genre.action, genre.scifi, Anime', 'three on the card');
    card('Act 0').click();
    await until(() => header().includes('genre.action, genre.scifi, Anime, genre.drama'), 'four in the modal');
});

test('a series\' genres stay in the modal from the episodes to an episode\'s streams and back', async () => {
    const root = openMeta({ type: 'series' });
    await until(() => shows(root, 'Ser 0'), 'the series card');
    const header = () => root.querySelector('dialog')?.textContent || '';
    [...root.querySelectorAll('.w-card-title')].find((el) => el.textContent === 'Ser 0').closest('.group').click();
    await until(() => header().includes('Pilot') && header().includes('genre.drama, genre.reality'), 'the episodes view');
    [...root.querySelectorAll('dialog button')].find((b) => b.textContent.includes('Pilot')).click();
    await until(() => urlParam('episode') === '1' && !header().includes('Pilot') && header().includes('Ser 0 - S1 E1'), 'the episode\'s streams');
    assert.ok(header().includes('genre.drama, genre.reality'), 'in the streams view');
    w.history.back();
    await until(() => urlParam('episode') === null && header().includes('Pilot'), 'the episodes view again');
    assert.ok(header().includes('genre.drama, genre.reality'), 'rebuilt from backToEpisodes');
});

test('an episode link opens its streams with the series\' genres, from the /meta fetched for "‹ Episodes"', async () => {
    // ttSer1 is in no catalog: nothing but its /meta knows its genres.
    const root = openMeta({ type: 'series', id: 'ttSer1', season: '1', episode: '1' });
    const header = () => root.querySelector('dialog')?.textContent || '';
    await until(() => header().includes('ttSer1 - S1 E1'), 'the episode\'s streams');
    await until(() => header().includes('genre.western'), 'the meta\'s genres');
});

// --- The URLs a switch writes ---
//
// lib/umami.js counts a pageview for each URL that changes, through
// pushState and replaceState alike. A switch pushes, then the state→URL
// sync replaces: the push must already be what the sync writes.

const here = () => w.location.pathname + w.location.search;
const typeTab = (root, label) => [...root.querySelectorAll('button')].find((b) => b.textContent === label);
const searchInput = (root) => root.querySelector('input[placeholder="discover.search"]');
const typeSearch = (root, q) => {
    searchInput(root).value = q;
    searchInput(root).dispatchEvent(new w.Event('input', { bubbles: true }));
};
const noResults = (root) => root.textContent.includes('discover.noResultsTitle');

test('each switch writes one URL', async () => {
    // writes: the push and the sync's replace (Load more only replaces).
    const step = async (what, act, done, writes = 2) => {
        const n = written.length;
        act();
        await until(() => done() && written.length >= n + writes, what);
        assert.deepEqual([...new Set(written.slice(n))], [here()], what);
    };
    const root = openMeta({ type: 'movie' });
    await until(() => genreSelect(root) && shows(root, 'Top 0'), 'Popular');
    // Action, then its second page: page=1 in the URL, for the next switch to drop.
    const actionPage2 = async () => {
        await step('a genre', () => choose(genreSelect(root), 'Action'), () => shows(root, 'Act 0'));
        await step('Load more', () => loadMore(root).click(), () => shows(root, 'More 0'), 1);
        assert.equal(urlParam('page'), '1');
    };
    await actionPage2();
    await step('another genre', () => choose(genreSelect(root), 'Drama'), () => shows(root, 'Dra 0'));
    await actionPage2();
    await step('another catalog', () => choose(root.querySelector('select'), `${META}::year`), () => shows(root, 'New 0'));
    await step('a year', () => choose(genreSelect(root), '2025'), () => shows(root, 'Old 0'));
    await step('the default year', () => choose(genreSelect(root), '2026'), () => shows(root, 'New 0'));
    assert.equal(urlParam('genre'), null);
    await step('a year', () => choose(genreSelect(root), '2025'), () => shows(root, 'Old 0'));
    await step('a year to another catalog', () => choose(root.querySelector('select'), `${META}::top`), () => shows(root, 'Top 0'));
    await actionPage2();
    await step('another type', () => typeTab(root, 'Series').click(), () => shows(root, 'Ser 0'));
    await step('the first type', () => typeTab(root, 'Movie').click(), () => shows(root, 'Top 0'));
    await actionPage2();
    await step('search', () => typeSearch(root, 'matrix'), () => noResults(root));
    await step('out of search', () => searchInput(root).dispatchEvent(new w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true })), () => shows(root, 'Top 0'));
    await step('a genre', () => choose(genreSelect(root), 'Action'), () => shows(root, 'Act 0'));
    await step('the watchlist', () => root.querySelector('button[aria-label="discover.watchlist.label"]').click(), () => genreSelect(root) === null);
});

test('back from search restores the type, the catalog and its year, and leaves that entry as it was', async () => {
    let root = openMeta({ type: 'movie' });
    await until(() => genreSelect(root) && shows(root, 'Top 0'), 'Popular');
    choose(root.querySelector('select'), `${META}::year`);
    await until(() => shows(root, 'New 0'), 'New');
    choose(genreSelect(root), '2025');
    await until(() => shows(root, 'Old 0') && urlParam('genre') === '2025', '2025');
    let before = here();
    typeSearch(root, 'matrix');
    await until(() => urlParam('search') === 'matrix' && noResults(root), 'search');
    let n = written.length;
    w.history.back();
    await until(() => root.querySelector('select')?.value === `${META}::year` && genreSelect(root)?.value === '2025' && shows(root, 'Old 0') && written.length > n, 'New at 2025 again');
    assert.deepEqual([...new Set(written.slice(n))], [before], 'the sync writes the entry back as it was');

    // A type other than the first: EXIT_SEARCH lands on the first one.
    root = openMeta({ type: 'series', 'catalog-base': META_KEY, 'catalog-id': 'top', genre: 'Reality-TV' });
    await until(() => shows(root, 'Real 0'), 'series, Reality-TV');
    before = here();
    typeSearch(root, 'matrix');
    await until(() => urlParam('search') === 'matrix' && noResults(root), 'search');
    n = written.length;
    w.history.back();
    await until(() => genreSelect(root)?.value === 'Reality-TV' && shows(root, 'Real 0') && written.length > n, 'series, Reality-TV again');
    assert.deepEqual([...new Set(written.slice(n))], [before]);
});

// Both types have a "year" catalog, neither of them first.
const TWIN = 'https://twin.test';
Object.assign(routes, {
    [`${TWIN}/manifest.json`]: {
        id: 'twin', name: 'twin', version: '1.0.0', resources: ['catalog'], types: ['movie', 'series'],
        catalogs: [
            { type: 'movie', id: 'top', name: 'Popular' }, { type: 'movie', id: 'year', name: 'New' },
            { type: 'series', id: 'top', name: 'Popular' }, { type: 'series', id: 'year', name: 'New' },
        ],
    },
    [`${TWIN}/catalog/movie/top.json`]: page('MTop', 1),
    [`${TWIN}/catalog/movie/year.json`]: page('MNew', 1),
    [`${TWIN}/catalog/series/top.json`]: page('STop', 1),
    [`${TWIN}/catalog/series/year.json`]: page('SNew', 1),
});

test('a back step across types opens the catalog it names, though the page shows one with the same id', async () => {
    const root = open(new URLSearchParams({ type: 'series' }), [{ id: '', url: TWIN }], [TWIN]);
    await until(() => shows(root, 'STop 0'), 'series Popular');
    choose(root.querySelector('select'), `${TWIN}::year`);
    await until(() => shows(root, 'SNew 0'), 'series New');
    typeTab(root, 'Movie').click();
    await until(() => shows(root, 'MTop 0'), 'movie Popular');
    choose(root.querySelector('select'), `${TWIN}::year`);
    await until(() => shows(root, 'MNew 0'), 'movie New');
    w.history.go(-2);
    await until(() => urlParam('type') === 'series' && shows(root, 'SNew 0'), 'series New again');
    assert.equal(urlParam('catalog-id'), 'year');
});
