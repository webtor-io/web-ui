// A quiet move to the next file syncs the page around the player
// (next-item-go.js syncPage). A dialog the viewer has open on the page -- a
// file's Download (partials/file.html) -- holds it, as fullscreen does: the
// swap took the dialog away under them, its job with it. Its own file: the
// listeners are bound once per module, to the document of the first sync.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM(`<!doctype html><body><div id="content"><div id="file">
    <div id="log-i1"><div class="live"><div class="wt-player-stage"></div><dialog id="subtitles"></dialog></div></div>
    <dialog id="download-modal-i1"><div id="log-i1-download"></div></dialog>
</div></div></body>`, { url: 'https://webtor.io/ru/res?file=e01' });
global.window = dom.window;
global.document = dom.window.document;
global.CustomEvent = dom.window.CustomEvent;
const fetches = [];
global.fetch = async (url) => { fetches.push(url); return { ok: false }; };
const { scheduleSync } = await import('./next-item-go.js');

test('the Download dialog open: the sync waits for it to close', () => {
    const dl = document.getElementById('download-modal-i1');
    dl.open = true;
    scheduleSync('/ru/res?file=e02');
    assert.deepEqual(fetches, [], 'held');
    dl.open = false;
    dl.dispatchEvent(new dom.window.Event('close'));
    assert.deepEqual(fetches, ['/ru/res?file=e02'], 'run on close');
});

test('the player\'s own dialog open: it moves with the player, nothing to wait for', () => {
    fetches.length = 0;
    document.getElementById('subtitles').open = true;
    scheduleSync('/ru/res?file=e03');
    assert.deepEqual(fetches, ['/ru/res?file=e03']);
});
