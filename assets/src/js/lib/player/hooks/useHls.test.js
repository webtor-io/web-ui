// useHls hands each stream the guard it was given and nothing else: a
// passthrough's with its own fragment policy, a multichannel-audio start's
// (createAudioGuard) with the config every stream has, none otherwise.
// jsdom has no MediaSource, so hls.js is told it is supported and its two
// calls that need one do nothing; everything the hook and hls-manager.js
// wire up is real.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'https://webtor.io/res', pretendToBeVisual: true });
globalThis.window = dom.window;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, 'navigator', { configurable: true, value: dom.window.navigator });
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const { h, render } = await import('preact');
const { useRef } = await import('preact/hooks');
const { default: Hls } = await import('hls.js');
const { useHls } = await import('./useHls.js');

Hls.isSupported = () => true;
Hls.prototype.loadSource = function () {};
Hls.prototype.attachMedia = function () {};
let recovered = 0;
Hls.prototype.recoverMediaError = function () { recovered++; };

const settle = () => new Promise((r) => setTimeout(r, 50));
const FATAL_MEDIA = { type: Hls.ErrorTypes.MEDIA_ERROR, details: Hls.ErrorDetails.BUFFER_STALLED_ERROR, fatal: true };

async function mountHls(opts) {
    const root = document.createElement('div');
    document.body.appendChild(root);
    function C() {
        const v = useRef(null);
        useHls(v, 'https://x.test/index.m3u8', opts);
        return h('video', { ref: v });
    }
    render(h(C, null), root);
    await settle();
    const hls = window.hlsPlayer;
    return { hls, unmount: () => render(null, root) };
}

function spyGuard(handles) {
    const g = { seen: [], set: [], onHlsError: (hls, d) => { g.seen.push(d.details); return handles; }, setHls: (x) => g.set.push(x) };
    return g;
}

test('no guard: the stream is what it has always been -- the old config, every fatal media error recovered', async () => {
    recovered = 0;
    const m = await mountHls({});
    assert.ok(m.hls instanceof Hls);
    assert.equal(m.hls.config.fragLoadPolicy.default.timeoutRetry.maxNumRetry, 100, 'the legacy settings');
    m.hls.trigger(Hls.Events.ERROR, FATAL_MEDIA);
    m.hls.trigger(Hls.Events.ERROR, FATAL_MEDIA);
    assert.equal(recovered, 2);
    m.unmount();
});

test('an audio guard: told the instance, first to see errors, and the old config', async () => {
    recovered = 0;
    const g = spyGuard(true);
    const m = await mountHls({ audioGuard: g });
    assert.deepEqual(g.set, [m.hls]);
    assert.equal(m.hls.config.fragLoadPolicy.default.timeoutRetry.maxNumRetry, 100, 'not the passthrough\'s policy');
    m.hls.trigger(Hls.Events.ERROR, FATAL_MEDIA);
    assert.deepEqual(g.seen, [FATAL_MEDIA.details]);
    assert.equal(recovered, 0, 'what the guard handles goes no further');
    m.unmount();
});

test('a passthrough: its guard, its fragment policy; an audio guard beside it is not used', async () => {
    recovered = 0;
    const g = spyGuard(true);
    const a = spyGuard(true);
    const m = await mountHls({ passthrough: { fragLoadMs: 240000, guard: g }, audioGuard: a });
    assert.deepEqual(g.set, [m.hls]);
    assert.deepEqual(a.set, []);
    assert.equal(m.hls.config.fragLoadPolicy.default.maxLoadTimeMs, 240000);
    m.hls.trigger(Hls.Events.ERROR, FATAL_MEDIA);
    assert.deepEqual(g.seen, [FATAL_MEDIA.details]);
    assert.deepEqual(a.seen, []);
    m.unmount();
});
