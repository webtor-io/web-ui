// The credits line (and, at the end, the genre labels) in the stream modal
// header, rendered: the real
// StreamModal in jsdom, through preact (setup as in
// StreamModal.switches.test.js). Faked: fetch for /discover/credits and the
// <dialog>'s showModal/close. t() answers with the key.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><html lang="en"><body><div id="root"></div></body></html>', {
    url: 'https://webtor.io/discover',
    pretendToBeVisual: true,
});
globalThis.window = dom.window;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, 'navigator', { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.localStorage = dom.window.localStorage;
globalThis.__SUPPORTED_LOCALES__ = ['en'];
dom.window.HTMLDialogElement.prototype.showModal = function showModal() { this.open = true; };
dom.window.HTMLDialogElement.prototype.close = function close() { this.open = false; };
dom.window.HTMLCanvasElement.prototype.getContext = () => null;

const { h, render } = await import('preact');
const { init } = await import('../i18n.js');
const { StreamModal } = await import('./StreamModal.jsx');
await init();

const answers = {
    tt1: { directors: ['Director A'], people: [{ name: 'Actor A', photo: '/lib/person/photo/aaaaaaaa.jpg/96.jpg' }, { name: 'Actor C' }] },
    tt2: { people: [{ name: 'Actor B' }] },
};
const calls = [];
globalThis.fetch = window.fetch = async (url, opts) => {
    if (String(url).includes('/discover/credits')) {
        const { id, type } = JSON.parse(opts.body);
        calls.push([id, type]);
        if (id === 'tt9') return { ok: false, json: async () => ({}) };
        return { ok: true, json: async () => answers[id] || {} };
    }
    return { ok: true, json: async () => ({}) };
};

const settle = () => new Promise(r => setTimeout(r, 40));
const root = document.getElementById('root');
const show = modal => render(h(StreamModal, {
    modal, onClose: () => {}, onStreamClick: () => {}, hasCustomAddons: true,
    playback: { caps: 'off', decodes: [], part: false, declared: null },
}), root);
const text = () => root.textContent;

test('known credits are drawn on the first render of the next view', async () => {
    show({ view: 'fetching', metaId: 'tt1', itemType: 'movie', title: 'One', addons: [] });
    await settle();
    assert.match(text(), /discover\.credits\.director:\s*Director A/);
    show({ view: 'streams', metaId: 'tt1', itemType: 'movie', title: 'One', streams: [] });
    assert.match(text(), /Director A/, 'the header remounts, the names stay');
});

test('another title never shows the previous title\'s names', async () => {
    show({ view: 'streams', metaId: 'tt1', itemType: 'movie', title: 'One', streams: [] });
    await settle();
    show({ view: 'streams', metaId: 'tt2', itemType: 'movie', title: 'Two', streams: [] });
    assert.doesNotMatch(text(), /Director A|Actor A/);
    await settle();
    assert.match(text(), /Actor B/);
});

test('a failure draws nothing and the next open asks again; episode ids ask nothing', async () => {
    show({ view: 'streams', metaId: 'tt9', itemType: 'series', title: 'Nine', streams: [] });
    await settle();
    render(null, root);
    show({ view: 'fetching', metaId: 'tt9', itemType: 'series', title: 'Nine', addons: [] });
    await settle();
    assert.deepEqual(calls.filter(([id]) => id === 'tt9'), [['tt9', 'series'], ['tt9', 'series']]);
    assert.doesNotMatch(text(), /discover\.credits/);

    const n = calls.length;
    show({ view: 'streams', metaId: 'tt1:1:2', itemType: 'series', title: 'Episode', streams: [] });
    await settle();
    assert.equal(calls.length, n);
    render(null, root);
});

test('the cast strip draws no photo until it is opened, and stays open across views', async () => {
    show({ view: 'fetching', metaId: 'tt1', itemType: 'movie', title: 'One', addons: [] });
    await settle();
    const details = () => root.querySelector('details');
    assert.ok(details(), 'the cast line is there');
    assert.match(details().querySelector('summary').textContent, /Actor A, Actor C/);
    assert.equal(root.querySelectorAll('img[src^="/lib/person/photo/"]').length, 0, 'closed: no photo requested');

    details().open = true;
    await settle();
    const photos = [...root.querySelectorAll('img[src^="/lib/person/photo/"]')].map(i => i.getAttribute('src'));
    assert.deepEqual(photos, ['/lib/person/photo/aaaaaaaa.jpg/96.jpg'], 'one photo; the other actor has none');

    show({ view: 'streams', metaId: 'tt1', itemType: 'movie', title: 'One', streams: [] });
    assert.ok(details().open, 'the view switch did not fold it');
    assert.equal(root.querySelectorAll('img[src^="/lib/person/photo/"]').length, 1);
    render(null, root);
});

test('genre labels sit in the year/rating row: up to four; the episodes view falls back to its meta', async () => {
    const row = () => root.querySelector('h3 + div.text-w-sub');
    show({ view: 'streams', metaId: 'tt2', itemType: 'movie', title: 'Two', year: '1999', streams: [],
        genres: ['Action', 'Sci-Fi', 'Science Fiction', 'Drama', 'Anime', 'Crime'] });
    assert.equal(row().lastElementChild.textContent, 'genre.action, genre.scifi, genre.drama, Anime');

    show({ view: 'episodes', itemId: 'tt3', itemType: 'series', title: 'Three', meta: { genres: ['Western'], videos: [{ id: 'tt3:1:1', season: 1, episode: 1 }] } });
    assert.equal(row().textContent, 'genre.western', 'no year, no rating: the row holds the genres alone');

    show({ view: 'streams', metaId: 'tt2', itemType: 'movie', title: 'Two', streams: [], genres: ['Bogus', 7] });
    assert.equal(row().textContent, 'Bogus');
    show({ view: 'streams', metaId: 'tt2', itemType: 'movie', title: 'Two', streams: [] });
    assert.equal(row(), null, 'nothing to show, no row');
    render(null, root);
});
