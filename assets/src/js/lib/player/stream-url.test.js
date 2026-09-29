// readStreamUrl against the element states the player sees, including the
// one hls.js leaves behind on a ManagedMediaSource (Safari on a Mac): our
// <source> gone, its own blob: <source> in its place.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'https://webtor.io/res' });
globalThis.document = dom.window.document;

const { readStreamUrl } = await import('./stream-url.js');

const el = (html) => {
    const d = document.createElement('div');
    d.innerHTML = html;
    return d.firstElementChild;
};

test('the rendered <source> is the stream', () => {
    const v = el('<video class="player"><source src="https://x.test/index.m3u8" type="application/x-mpegURL"><track src="https://x.test/en.vtt"></video>');
    assert.equal(readStreamUrl(v), 'https://x.test/index.m3u8');
});

// Negative control: returning the first <source> whatever its URL (the code
// before this fix) makes this test fail with the blob.
test('hls.js swapped in its blob: <source>: no URL, the caller keeps the last one', () => {
    const v = el('<video class="player"><source src="blob:https://webtor.io/8a61e4c8"></video>');
    assert.equal(readStreamUrl(v), null);
    v.setAttribute('src', 'blob:https://webtor.io/8a61e4c8');
    assert.equal(readStreamUrl(v), null);
});

test('a blob: <source> next to a real one: the real one', () => {
    const v = el('<video><source src="blob:https://webtor.io/1"><source src="https://x.test/next.m3u8"></video>');
    assert.equal(readStreamUrl(v), 'https://x.test/next.m3u8');
});

test('no <source>: the element\'s own src, resolved', () => {
    const v = el('<video src="/files/movie.mp4"></video>');
    assert.equal(readStreamUrl(v), 'https://webtor.io/files/movie.mp4');
    assert.equal(readStreamUrl(el('<video></video>')), null);
    assert.equal(readStreamUrl(null), null);
});
