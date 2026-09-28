import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM, VirtualConsole } from 'jsdom';

// A passthrough that fails after the player moved on to the next file quietly
// restarts that file through the page's deep link (passthrough.js
// fallbackToOldRoute). The deep link only works if the page actually loads:
// app/resource/get.js reads the hash once, on load. The quiet move has
// already put the next file in the address (next-item-go.js pushState), so
// the deep link differs from the address only by its hash -- a fragment
// navigation, which loads nothing.
//
// jsdom navigates by the same rule a browser does (its navigation.js: a URL
// equal to the document's but for the fragment is a fragment navigation;
// anything else, and a reload, is a document load, which jsdom does not
// implement and reports on the virtual console). That report is what these
// tests count as "the page was loaded".

// The import chain of next-item-go.js reaches lib/turnstileAction.js, which
// reads `window` when it loads: a document first, the module after.
const boot = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = boot.window; global.document = boot.window.document;
const { nextURL } = await import('./next-item-go.js');
const { fallbackToOldRoute } = await import('./passthrough.js');

function loadingPage(url) {
    const loads = [];
    const other = [];
    const vc = new VirtualConsole();
    vc.on('jsdomError', (e) => {
        (/navigation to another Document/.test(e.message) ? loads : other).push(e.message);
    });
    const dom = new JSDOM('<!doctype html><body></body>', { url, pretendToBeVisual: true, virtualConsole: vc });
    return { win: dom.window, loads, other };
}

// The resource page's start form, as stream_video.html renders it.
function startForm(win, iid) {
    const f = win.document.createElement('form');
    f.setAttribute('action', '/ru/stream-video');
    f.setAttribute('method', 'post');
    f.className = 'stream-video';
    f.innerHTML = `<input type="hidden" name="resource-id" value="res1"><input type="hidden" name="item-id" value="${iid}">`;
    win.document.body.appendChild(f);
    const submits = [];
    win.document.addEventListener('submit', (e) => { e.preventDefault(); submits.push(e.target); });
    return submits;
}

// The passthrough <video> the player renders for a file.
function video(win, iid, path) {
    const v = win.document.createElement('video');
    v.dataset.videoRoute = 'passthrough';
    v.dataset.videoClass = 'hevc10-2160';
    v.dataset.resourceId = 'res1';
    v.dataset.itemId = iid;
    v.dataset.path = path;
    win.document.body.appendChild(v);
    return v;
}

test('fallback after a quiet move: the deep link loads the page although the address already names the file', () => {
    const { win, loads, other } = loadingPage('https://webtor.io/ru/res1?file=S01%2Fep1.mkv');
    const submits = startForm(win, 'ep1');
    // The quiet move: the next file's player, and its address pushed; the
    // page's form still names the previous one.
    win.history.pushState({}, '', nextURL(win.location.href, 'S01/ep2.mkv'));
    const entries = win.history.length;
    const v = video(win, 'ep2', 'S01/ep2.mkv');

    const how = fallbackToOldRoute({ video: v, reason: 'no_frames', win, doc: win.document, track: () => {} });

    assert.equal(how, 'navigate');
    assert.equal(submits.length, 0, 'the previous episode is not restarted');
    assert.equal(loads.length, 1, 'the page is loaded, once: get.js runs the deep link');
    const u = new URL(win.location.href);
    assert.equal(u.searchParams.get('file'), 'S01/ep2.mkv');
    const h = new URLSearchParams(u.hash.slice(1));
    assert.equal(h.get('action'), 'stream');
    assert.equal(h.get('decode-fallback'), 'no_frames');
    assert.equal(h.get('decode-class'), 'hevc10-2160');
    assert.equal(win.history.length, entries, 'no second history entry for the same file');
    assert.deepEqual(other, []);
});

test('fallback to a file the address does not name: one ordinary load of its deep link', () => {
    const { win, loads, other } = loadingPage('https://webtor.io/ru/res1?file=S01%2Fep1.mkv');
    startForm(win, 'ep1');
    const v = video(win, 'ep2', 'S01/ep2.mkv');

    const how = fallbackToOldRoute({ video: v, reason: 'decode_error', win, doc: win.document, track: () => {} });

    assert.equal(how, 'navigate');
    // A reload on top would load the address, the previous file, instead.
    assert.equal(loads.length, 1, 'one load, and no reload of the old address');
    assert.deepEqual(other, []);
});
