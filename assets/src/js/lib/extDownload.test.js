import test from 'node:test';
import assert from 'node:assert/strict';
import { receiveTorrent, ExtDownloadError } from './extDownload.js';

const bytes = [100, 56, 58, 97, 110, 110, 111, 117, 110, 99, 101];

// A window as far as the handshake goes: postMessage delivers to the page's
// own listeners asynchronously, with event.source === the window, the way a
// browser delivers a message the extension's content script posts to it.
function fakeWindow() {
    const listeners = new Set();
    const win = {
        sent: [],
        addEventListener(type, fn) { if (type === 'message') listeners.add(fn); },
        removeEventListener(type, fn) { if (type === 'message') listeners.delete(fn); },
        postMessage(data) {
            win.sent.push(data);
            setTimeout(() => { for (const fn of [...listeners]) fn({ source: win, data }); }, 0);
        },
        listeners,
    };
    return win;
}

// The extension: announces itself, then answers a downloadId with `reply`.
function extension(win, reply, { announce = true } = {}) {
    win.addEventListener('message', (e) => {
        if (e.data && e.data.downloadId !== undefined && reply !== undefined) {
            win.postMessage(typeof reply === 'function' ? reply(e.data.downloadId) : reply);
        }
    });
    if (announce) setTimeout(() => win.postMessage({ webtorInjected: true }), 5);
}

test('the handshake hands over the bytes', async () => {
    const win = fakeWindow();
    extension(win, (id) => ({ torrent: { data: bytes }, ver: '0.1.13', id }));
    const got = await receiveTorrent(win, 7, 1000);
    assert.deepEqual(Array.from(got), bytes);
    assert.deepEqual(win.sent.find((d) => 'downloadId' in d), { downloadId: 7 });
});

test('an extension already injected is not waited for', async () => {
    const win = fakeWindow();
    win.__webtorInjected = true;
    extension(win, { torrent: bytes }, { announce: false });
    assert.deepEqual(Array.from(await receiveTorrent(win, 1, 1000)), bytes);
});

test('no extension: it gives up instead of waiting forever', async () => {
    const win = fakeWindow();
    await assert.rejects(receiveTorrent(win, 1, 30), (e) => e instanceof ExtDownloadError && /did not announce/.test(e.message));
    assert.equal(win.listeners.size, 0, 'the listener must go');
});

test('an extension that never answers the download', async () => {
    const win = fakeWindow();
    extension(win, undefined);
    await assert.rejects(receiveTorrent(win, 1, 50), (e) => e instanceof ExtDownloadError && /sent no torrent/.test(e.message));
});

test('an answer without bytes fails at once, not at the deadline', async () => {
    const win = fakeWindow();
    extension(win, { torrent: { data: [] }, ver: '9.9.9' });
    const started = Date.now();
    await assert.rejects(receiveTorrent(win, 1, 5000), (e) => e instanceof ExtDownloadError && /without bytes, ver=9\.9\.9/.test(e.message));
    assert.ok(Date.now() - started < 1000, 'it waited for the deadline');
    assert.equal(win.listeners.size, 1, 'only the fake extension may still listen');
});

test('messages from another source are ignored', async () => {
    const win = fakeWindow();
    win.__webtorInjected = true;
    // a frame posts a torrent: not the extension, not this page
    setTimeout(() => { for (const fn of [...win.listeners]) fn({ source: {}, data: { torrent: [1, 2, 3] } }); }, 0);
    extension(win, { torrent: bytes }, { announce: false });
    assert.deepEqual(Array.from(await receiveTorrent(win, 1, 1000)), bytes);
});
