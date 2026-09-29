// DiscoverApp to the stream modal's switches, end to end in jsdom: the page
// asks the browser's probe on mount, gathers the context again when it
// answers, and hands it to the modal. Every piece has its own tests
// (playback.js, usePlaybackContext.js, StreamModal.switches); this one
// breaks when the wiring between them does -- a modal without the context
// shows every release as for a browser that has not answered.
//
// Real: DiscoverApp, the modal, the declaration module and its probe
// (codec-support.js asked of jsdom, which decodes nothing: no MediaSource,
// canPlayType ''), preact, the DOM. Faked: `fetch` (a Stremio addon with one
// film and its releases), the <dialog>'s showModal/close, window.umami.

import { test, after } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const ADDON = 'https://addon.test';
const FILM = 'tt0000001';

const dom = new JSDOM('<!doctype html><html lang="en"><body><div id="root"></div></body></html>', {
    url: `https://webtor.io/discover?type=movie&id=${FILM}`,
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
w.HTMLDialogElement.prototype.showModal = function showModal() { this.open = true; };
w.HTMLDialogElement.prototype.close = function close() { this.open = false; };
w.HTMLCanvasElement.prototype.getContext = () => null;
w.scrollTo = () => {};
// The AI section's chips come over an EventSource; none here ever opens.
class NoEventSource {
    static CLOSED = 2;
    constructor() { this.readyState = 0; }
    addEventListener() {}
    close() { this.readyState = NoEventSource.CLOSED; }
}
globalThis.EventSource = NoEventSource;
w.EventSource = NoEventSource;
w.umami = { track: () => {} };
// The transcoder converts (production today): HEVC releases play for every
// browser, and the HEVC switch's default follows the browser alone.
w._passthrough = { hevc: 'off' };

const RELEASES = [
    { name: 'Test\n1080p', title: 'Movie.2023.1080p.WEB-DL.x265-GRP', infoHash: '1'.repeat(40) },
    { name: 'Test\n1080p', title: 'Movie.2023.1080p.WEB-DL.x264-GRP', infoHash: '2'.repeat(40) },
];

// The addon, as the Stremio protocol has it; anything else (library
// statuses, watchlist, localisation) answers 404, which Discover treats as
// "nothing".
const respond = (body, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const routes = {
    [`${ADDON}/manifest.json`]: {
        id: 'test.addon', name: 'Test', version: '1.0.0', resources: ['catalog', 'stream'], types: ['movie'],
        catalogs: [{ type: 'movie', id: 'top', name: 'Top' }],
    },
    [`${ADDON}/catalog/movie/top.json`]: { metas: [{ id: FILM, type: 'movie', name: 'Movie' }] },
    [`https://v3-cinemeta.strem.io/meta/movie/${FILM}.json`]: { meta: { id: FILM, type: 'movie', name: 'Movie' } },
    [`${ADDON}/stream/movie/${FILM}.json`]: { streams: RELEASES },
};
globalThis.fetch = async (url) => {
    const u = String(url);
    return u in routes ? respond(routes[u]) : respond({}, 404);
};

after(() => w.close());

const { h, render } = await import('preact');
const { init } = await import('../i18n.js');
const { DiscoverApp } = await import('./DiscoverApp.jsx');
await init();

const until = async (cond, what, ms = 3000) => {
    const t0 = Date.now();
    while (!cond()) {
        if (Date.now() - t0 > ms) throw new Error(`timed out waiting for ${what}`);
        await new Promise((r) => setTimeout(r, 10));
    }
};

test('Discover hands the browser\'s answer to the stream modal: HEVC off where the probe says the browser decodes none', async () => {
    const root = document.getElementById('root');
    render(h(DiscoverApp, { addonUrls: [ADDON], addonSeeds: [], hasCustomAddons: true }), root);
    // No answer when the page mounts (no cache, the probe not started): the
    // context the page begins with is "not answered".
    assert.ok(!(w.__wtDecode && w.__wtDecode.fresh), 'the probe answers after the page mounted');
    const hevc = () => root.querySelector('[data-switch="hevc"] input');
    await until(() => hevc(), 'the streams view with its HEVC switch');
    // Let the list settle on the probe's answer; if it never does, the
    // assertions below say what is wrong.
    await until(() => w.__wtDecode && w.__wtDecode.fresh && hevc().checked === false, 'the answer to reach the modal', 2000).catch(() => {});
    // audio: [] -- every browser but one opted out (?audio=off) may declare
    // aac51, so the page's one probe asks the audio part too; jsdom answers
    // none, and Discover reads the video part only.
    assert.deepEqual(w.__wtDecode && w.__wtDecode.fresh, { hevc: [], pq: 'no', audio: [] }, 'the probe ran, and jsdom decodes nothing');
    assert.equal(hevc().checked, false, 'a browser that decodes no HEVC: the switch is off');
    const titles = [...root.querySelectorAll('.cursor-pointer.flex.items-center.gap-3 .text-xs.text-w-sub')].map((el) => el.textContent);
    assert.deepEqual(titles, ['Movie.2023.1080p.WEB-DL.x264-GRP'], 'the HEVC release is hidden');
    render(null, root);
});
