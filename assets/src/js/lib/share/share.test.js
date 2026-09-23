import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM(`<!doctype html><body>
    <dialog id="share-dialog"><input id="share-url"></dialog>
    <button id="magnet" data-magnet-hash="08ada5a7a6183aae1e09d831df6748d566095a10" data-magnet-name="Sintel"></button>
</body>`, { url: 'https://webtor.io/08ada5a7a6183aae1e09d831df6748d566095a10?tool=magnet-to-torrent&file=%2FSintel%2FSintel.mp4' });
global.window = dom.window;
global.document = dom.window.document;
// jsdom has no showModal; the share dialog only needs to receive the URL.
dom.window.HTMLDialogElement.prototype.showModal = function () {};

const { shareResource, copyMagnet } = await import('./share.js');

test('a shared link does not carry the sharer\'s tool page', () => {
    shareResource();
    const u = new URL(document.getElementById('share-url').value);
    assert.equal(u.searchParams.has('tool'), false, '?tool= is the sharer\'s arrival, not the recipient\'s');
    // Everything else about the page survives, UTM included.
    assert.equal(u.pathname, '/08ada5a7a6183aae1e09d831df6748d566095a10');
    assert.equal(u.searchParams.get('file'), '/Sintel/Sintel.mp4');
    assert.equal(u.searchParams.get('utm_campaign'), 'resource_share');
});

test('copy-magnet carries the tool prop only when the button has one', async () => {
    const tracked = [];
    window.umami = { track: (name, data) => tracked.push([name, data]) };
    const writes = [];
    // share.js reads the global navigator; Node has one of its own, without
    // a clipboard.
    Object.defineProperty(globalThis, 'navigator', { value: { clipboard: { writeText: async (s) => writes.push(s) } }, configurable: true });
    const btn = document.getElementById('magnet');

    copyMagnet(btn);
    await new Promise((r) => setTimeout(r, 0));
    assert.deepEqual(tracked.pop(), ['copy-magnet', undefined], 'an ordinary copy is tracked as before');

    btn.dataset.tool = 'torrent-to-magnet';
    copyMagnet(btn);
    await new Promise((r) => setTimeout(r, 0));
    assert.deepEqual(tracked.pop(), ['copy-magnet', { tool: 'torrent-to-magnet' }]);
    assert.equal(writes[1], 'magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Sintel');
});
