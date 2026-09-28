// The stream modal's video switches, rendered: HEVC, HDR and 4K above the
// list, what each hides, the warnings, the empty states, the DV5 badge,
// and what a click on a release records (lib/discover/playback.js has the
// rules; these check the modal applies them).
//
// The real StreamModal in jsdom, through preact. Not faked: the DOM, the
// rules, discover-prefs in localStorage. Faked: window.umami (the events
// are assertions) and the <dialog>'s showModal/close, which jsdom lacks.
// t() answers with the key (the test loader serves an empty locale), so
// the texts are asserted by key.

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
// The filter chips ask a canvas whether flags render (lang.js); jsdom has no
// 2D context and would say so on the console. No context is an answer that
// code already handles.
dom.window.HTMLCanvasElement.prototype.getContext = () => null;

const { h, render } = await import('preact');
const { init } = await import('../i18n.js');
const { StreamModal } = await import('./StreamModal.jsx');
await init();

const HEVC = ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160'];
const EVERYTHING = [...HEVC, 'hevc-high', 'hdr-pq'];

let n = 0;
// stream: a release as an addon sends it; the hash only has to be unique.
const stream = (name, title) => ({
    name,
    title,
    infoHash: (++n).toString(16).padStart(40, '0'),
});
const S = {
    hevc1080: () => stream('Addon\n1080p', 'Movie.2023.1080p.WEB-DL.x265-GRP'),
    avc1080: () => stream('Addon\n1080p', 'Movie.2023.1080p.WEB-DL.x264-GRP'),
    unknown1080: () => stream('Addon\n1080p', 'Movie.2023.1080p.WEB-DL-GRP'),
    hdr1080: () => stream('Addon\n1080p HDR', 'Movie.2023.1080p.WEB-DL.HDR10.x265-GRP'),
    uhdHevc: () => stream('Addon\n4k HDR', 'Movie.2023.2160p.WEB-DL.HDR10.H.265-GRP'),
    uhdAvc: () => stream('Addon\n4k', 'Movie.2023.2160p.WEB-DL.H264-GRP'),
    dv5: () => stream('Addon\n4k DV', 'Movie.2023.2160p.WEB-DL.DDP5.1.DV.H.265-GRP'),
};

const settle = () => new Promise((r) => setTimeout(r, 30));

let events;
async function mount(streams, playback, prefs) {
    const root = document.getElementById('root');
    render(null, root);
    localStorage.clear();
    window.sessionStorage.clear();
    if (prefs) localStorage.setItem('discover-prefs', JSON.stringify(prefs));
    events = [];
    window.umami = { track: (name, data) => events.push(data === undefined ? { name } : { name, data }) };
    const clicks = [];
    render(h(StreamModal, {
        modal: { view: 'streams', title: 'Movie', streams },
        onClose: () => {},
        onStreamClick: (hash, idx) => clicks.push([hash, idx]),
        hasCustomAddons: true,
        playback,
    }), root);
    await settle();
    return { root, clicks };
}

const switchOf = (root, kind) => root.querySelector(`[data-switch="${kind}"]`);
const checked = (root, kind) => switchOf(root, kind).querySelector('input').checked;
const flip = async (root, kind) => {
    switchOf(root, kind).querySelector('input').dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    await settle();
};
const warning = (root) => root.querySelector('[data-switch-warning]');
const warningButtons = (root) => warning(root).querySelectorAll('button');
const rows = (root) => [...root.querySelectorAll('.cursor-pointer.flex.items-center.gap-3')];
const rowTitles = (root) => rows(root).map((r) => r.querySelector('.text-xs.text-w-sub').textContent);
const prefs = () => JSON.parse(localStorage.getItem('discover-prefs') || '{}');
const eventNames = () => events.map((e) => e.name);

const off = (decodes) => ({ caps: 'off', decodes, part: false, declared: null });

// ---- HEVC ------------------------------------------------------------------

test('a browser that does not decode HEVC: the switch is off and HEVC releases are hidden; unknown codec stays', async () => {
    const { root } = await mount([S.hevc1080(), S.avc1080(), S.unknown1080()], off([]));
    assert.ok(switchOf(root, 'hevc'), 'the HEVC switch is there');
    assert.equal(checked(root, 'hevc'), false);
    assert.match(switchOf(root, 'hevc').textContent, /\(1\)/, 'it counts the HEVC releases');
    assert.deepEqual(rowTitles(root), ['Movie.2023.1080p.WEB-DL.x264-GRP', 'Movie.2023.1080p.WEB-DL-GRP']);
    assert.equal(prefs().showHevc, undefined, 'a default is not a choice: nothing written');
});

test('turning HEVC on where the browser does not decode it warns first; confirming shows them and remembers', async () => {
    const { root } = await mount([S.hevc1080(), S.avc1080()], off([]));
    await flip(root, 'hevc');
    assert.ok(warning(root), 'a warning');
    assert.equal(warning(root).getAttribute('data-switch-warning'), 'hevc');
    assert.match(warning(root).textContent, /discover\.warningHevcTitle/);
    assert.match(warning(root).textContent, /discover\.warningHevcBody/);
    assert.equal(rows(root).length, 1, 'nothing shown before the viewer confirms');
    const [, confirm] = warningButtons(root);
    assert.match(confirm.textContent, /discover\.showHevc/);
    confirm.click();
    await settle();
    assert.equal(warning(root), null);
    assert.equal(checked(root, 'hevc'), true);
    assert.equal(rows(root).length, 2);
    assert.equal(prefs().showHevc, true);
    assert.deepEqual(events.filter((e) => e.name.startsWith('discover-hevc')), [
        { name: 'discover-hevc-toggle-attempt', data: { browser: 'no' } },
        { name: 'discover-hevc-enabled', data: { browser: 'no' } },
    ]);
});

test('cancelling the HEVC warning changes nothing', async () => {
    const { root } = await mount([S.hevc1080(), S.avc1080()], off([]));
    await flip(root, 'hevc');
    warningButtons(root)[0].click();
    await settle();
    assert.equal(warning(root), null);
    assert.equal(checked(root, 'hevc'), false);
    assert.equal(rows(root).length, 1);
    assert.equal(prefs().showHevc, undefined);
    assert.deepEqual(eventNames().filter((e) => e.startsWith('discover-hevc')), ['discover-hevc-toggle-attempt', 'discover-hevc-cancelled']);
});

test('a browser that decodes HEVC: on by default; off and on again without a warning', async () => {
    const { root } = await mount([S.hevc1080(), S.avc1080()], off(EVERYTHING));
    assert.equal(checked(root, 'hevc'), true);
    assert.equal(rows(root).length, 2);
    await flip(root, 'hevc');
    assert.equal(warning(root), null);
    assert.equal(rows(root).length, 1);
    assert.equal(prefs().showHevc, false);
    await flip(root, 'hevc');
    assert.equal(warning(root), null, 'no warning where the browser decodes it');
    assert.equal(rows(root).length, 2);
    assert.equal(prefs().showHevc, true);
});

// A check that has not answered is not a browser that cannot decode.
test('a browser that has not answered: HEVC shown, and turning it back on does not warn', async () => {
    const { root } = await mount([S.hevc1080(), S.avc1080()], off(null));
    assert.equal(checked(root, 'hevc'), true);
    assert.equal(rows(root).length, 2);
    await flip(root, 'hevc');
    await flip(root, 'hevc');
    assert.equal(warning(root), null);
    assert.deepEqual(events.filter((e) => e.name.startsWith('discover-hevc')).map((e) => e.data.browser), ['unknown', 'unknown']);
});

test('the viewer\'s stored choice is kept over the default', async () => {
    let { root } = await mount([S.hevc1080(), S.avc1080()], off([]), { showHevc: true });
    assert.equal(checked(root, 'hevc'), true);
    assert.equal(rows(root).length, 2);
    ({ root } = await mount([S.hevc1080(), S.avc1080()], off(EVERYTHING), { showHevc: false }));
    assert.equal(checked(root, 'hevc'), false);
    assert.equal(rows(root).length, 1);
});

test('no HEVC release, no HEVC switch', async () => {
    const { root } = await mount([S.avc1080(), S.unknown1080()], off([]));
    assert.equal(switchOf(root, 'hevc'), null);
});

// ---- HDR -------------------------------------------------------------------

test('HDR: on where the browser decodes PQ, off (with a warning to turn on) where it decodes HEVC but not PQ', async () => {
    let { root } = await mount([S.hdr1080(), S.hevc1080()], off(EVERYTHING));
    assert.equal(checked(root, 'hdr'), true);
    assert.equal(rows(root).length, 2);
    ({ root } = await mount([S.hdr1080(), S.hevc1080()], off(HEVC)));
    assert.equal(checked(root, 'hdr'), false);
    assert.deepEqual(rowTitles(root), ['Movie.2023.1080p.WEB-DL.x265-GRP']);
    await flip(root, 'hdr');
    assert.match(warning(root).textContent, /discover\.warningHdrBody/);
    warningButtons(root)[1].click();
    await settle();
    assert.equal(rows(root).length, 2);
    assert.equal(prefs().showHdr, true);
});

// ---- 4K: the gate ------------------------------------------------------------

test('where 4K HEVC plays (transcoder on, 4K Main10 declared) there is no 4K switch and 4K is shown', async () => {
    const { root } = await mount([S.uhdHevc(), S.uhdAvc(), S.avc1080()],
        { caps: 'on', decodes: EVERYTHING, part: true, declared: EVERYTHING }, { show4k: false });
    assert.equal(switchOf(root, 'uhd'), null);
    assert.equal(rows(root).length, 3, 'an old "hide 4K" does not hide what now plays, without a switch to undo it');
});

const warning4k = async (playback) => {
    const { root } = await mount([S.uhdAvc(), S.avc1080()], playback);
    assert.ok(switchOf(root, 'uhd'), 'the 4K switch');
    assert.equal(checked(root, 'uhd'), false);
    assert.equal(rows(root).length, 1, '4K hidden by default');
    await flip(root, 'uhd');
    const body = warning(root).querySelectorAll('p')[1].textContent.trim();
    warningButtons(root)[1].click();
    await settle();
    assert.equal(rows(root).length, 2);
    assert.equal(prefs().show4k, true);
    return body;
};

test('the 4K warning says why, by the gate: off, unknown, a declaring browser without 4K Main10', async () => {
    assert.equal(await warning4k({ caps: 'off', decodes: EVERYTHING, part: true, declared: EVERYTHING }), 'discover.warning4kBody');
    assert.equal(await warning4k({ caps: 'unknown', decodes: EVERYTHING, part: true, declared: EVERYTHING }), 'discover.warning4kBodyUnchecked');
    assert.equal(await warning4k({ caps: 'on', decodes: ['hevc8', 'hevc8-2160'], part: true, declared: ['hevc8', 'hevc8-2160'] }), 'discover.warning4kBodyNoHevc');
    assert.equal(await warning4k({ caps: 'on', decodes: ['hevc8'], part: true, declared: ['hevc8'] }), 'discover.warning4kBodyNoHevc',
        'HEVC 1080p only: not offered 4K HEVC as playable');
    assert.equal(await warning4k({ caps: 'on', decodes: EVERYTHING, part: false, declared: null }), 'discover.warning4kBody', 'not declaring');
    // Production before the transcoder has GET /capabilities: nobody
    // declares, and the answer is unknown everywhere. The text stays the one
    // Discover has always shown.
    assert.equal(await warning4k({ caps: 'unknown', decodes: EVERYTHING, part: false, declared: null }), 'discover.warning4kBody',
        'not declaring, transcoder not answered');
    assert.equal(await warning4k({ caps: 'unknown', decodes: null, part: false, declared: null }), 'discover.warning4kBody');
    // The 4K events keep their names and carry nothing new.
    assert.deepEqual(events.filter((e) => e.name.startsWith('discover-4k')),
        [{ name: 'discover-4k-toggle-attempt' }, { name: 'discover-4k-enabled' }]);
});

test('a stored "show 4K" still shows 4K where the switch stands', async () => {
    const { root } = await mount([S.uhdAvc(), S.avc1080()], off(EVERYTHING), { show4k: true });
    assert.equal(checked(root, 'uhd'), true);
    assert.equal(rows(root).length, 2);
});

// ---- empty states ------------------------------------------------------------

const emptyText = (root) => {
    const p = [...root.querySelectorAll('p')].find((el) => /discover\.all/.test(el.textContent));
    return p ? p.textContent.trim() : null;
};

test('everything hidden: the switch that hides it is named, or all of them', async () => {
    let { root } = await mount([S.hevc1080(), S.hevc1080()], off([]));
    assert.equal(emptyText(root), 'discover.allHevcStreams');
    assert.equal(rows(root).length, 0);
    ({ root } = await mount([S.uhdAvc()], off(EVERYTHING)));
    assert.equal(emptyText(root), 'discover.all4kStreams');
    ({ root } = await mount([S.hdr1080()], off(HEVC)));
    assert.equal(emptyText(root), 'discover.allHdrStreams');
    ({ root } = await mount([S.hevc1080(), S.uhdAvc()], off([])));
    assert.equal(emptyText(root), 'discover.allHiddenStreams');
    ({ root } = await mount([S.hevc1080(), S.avc1080()], off([])));
    assert.equal(emptyText(root), null);
});

// ---- DV5 ---------------------------------------------------------------------

test('a Dolby Vision profile 5 release is badged for every browser and not hidden by HDR', async () => {
    for (const playback of [off(EVERYTHING), off(HEVC), { caps: 'on', decodes: EVERYTHING, part: true, declared: EVERYTHING }, off(null)]) {
        const { root } = await mount([S.dv5(), S.hdr1080()], { ...playback }, { show4k: true });
        const dv5Row = rows(root).find((r) => /\.DV\.H\.265/.test(r.textContent));
        assert.ok(dv5Row, `${JSON.stringify(playback)}: the DV5 release is listed`);
        const badge = dv5Row.querySelector('[data-badge="dv5"]');
        assert.ok(badge);
        assert.equal(badge.textContent, 'discover.badgeDv5');
        assert.equal(badge.getAttribute('title'), 'discover.badgeDv5Hint');
    }
    const { root } = await mount([S.hdr1080()], off(EVERYTHING));
    assert.equal(root.querySelector('[data-badge="dv5"]'), null, 'DV with HDR10 is not DV5');
});

// ---- measurement ---------------------------------------------------------------

test('a click on a release records what its name said, for the player to check', async () => {
    const s = S.dv5();
    const { root, clicks } = await mount([s], off(EVERYTHING), { show4k: true });
    rows(root)[0].click();
    assert.deepEqual(clicks, [[s.infoHash, null]]);
    const rec = JSON.parse(window.sessionStorage.getItem('wt-discover-release'));
    assert.deepEqual({ ...rec, at: 0 }, { h: s.infoHash, codec: 'hevc', hdr: 'dv', dv5: true, uhd: true, at: 0 });
});

test('discover-streams-classified: once per list, what the names said and what the switches hid', async () => {
    const { root } = await mount([S.hevc1080(), S.avc1080(), S.uhdHevc(), S.dv5(), S.unknown1080()], off([]));
    const got = () => events.filter((e) => e.name === 'discover-streams-classified');
    assert.equal(got().length, 1);
    assert.deepEqual(got()[0].data, {
        n: 5, hevc: 3, hdr: 1, uhd: 2, dv5: 1, unknown_codec: 1,
        hidden: 3, empty: false, dec_hevc: 'no', dec_pq: 'no', caps: 'off', uhd_plays: false,
    });
    await flip(root, 'uhd');
    warningButtons(root)[1].click();
    await settle();
    assert.equal(got().length, 1, 'a flipped switch is the same list');
});

test('a modal without a playback context behaves as one whose checks have not answered, on a page that does not declare', async () => {
    const { root } = await mount([S.hevc1080(), S.uhdAvc(), S.avc1080()], undefined);
    assert.equal(checked(root, 'hevc'), true);
    assert.ok(switchOf(root, 'uhd'));
    await flip(root, 'uhd');
    assert.equal(warning(root).querySelectorAll('p')[1].textContent.trim(), 'discover.warning4kBody');
});
