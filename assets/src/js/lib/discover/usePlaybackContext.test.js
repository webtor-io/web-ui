// usePlaybackContext: the browser's probe is started on mount, and the
// context is gathered again when it answers -- the part of the stream
// modal's switches that lives in DiscoverApp. Rendered through preact in
// jsdom; the declaration module is a stand-in whose answer the test gives.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
    url: 'https://webtor.io/discover',
    pretendToBeVisual: true,
});
globalThis.window = dom.window;
globalThis.document = dom.window.document;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

const { h, render } = await import('preact');
const { usePlaybackContext } = await import('./usePlaybackContext.js');

const settle = () => new Promise((r) => setTimeout(r, 30));

// A browser whose probe answers when the test says so.
function browser({ tokens = ['hevc8', 'hevc10'], cached = null, throws = false } = {}) {
    let answered = cached;
    let resolve;
    let reject;
    const probe = new Promise((res, rej) => { resolve = res; reject = rej; });
    const started = [];
    return {
        dd: {
            startProbe: (w) => {
                started.push(w);
                if (throws) throw new Error('the probe module did not load');
                return probe;
            },
            decodedTokens: () => answered,
            declaredTokens: () => null,
            takesPart: () => false,
        },
        answer: () => { answered = tokens; resolve(); },
        fail: () => reject(new Error('probe failed')),
        started,
    };
}

async function mount(win, dd) {
    const root = document.getElementById('root');
    render(null, root);
    const seen = [];
    function Consumer() {
        seen.push(usePlaybackContext(win, dd));
        return null;
    }
    render(h(Consumer, null), root);
    await settle();
    return { seen, last: () => seen[seen.length - 1], root };
}

const PAGE = { _passthrough: { hevc: 'off' } };

test('the probe is started on mount, and the context is gathered again when it answers', async () => {
    const b = browser();
    const m = await mount(PAGE, b.dd);
    assert.deepEqual(b.started, [PAGE], 'started once, with the page');
    assert.equal(m.last().decodes, null, 'not answered yet');
    assert.equal(m.last().caps, 'off');
    b.answer();
    await settle();
    assert.deepEqual(m.last().decodes, ['hevc8', 'hevc10'], 'the answer reaches the context');
    assert.equal(b.started.length, 1, 'not started again');
});

test('a cached answer of this browser counts at once', async () => {
    const b = browser({ cached: [] });
    const m = await mount(PAGE, b.dd);
    assert.deepEqual(m.seen[0].decodes, [], 'the first render already has it');
});

test('a probe that throws leaves the context unanswered, and the page renders', async () => {
    const b = browser({ throws: true });
    const m = await mount(PAGE, b.dd);
    assert.equal(b.started.length, 1);
    assert.equal(m.last().decodes, null);
    assert.equal(m.last().caps, 'off');
});

test('a probe that fails leaves the context unanswered', async () => {
    const b = browser();
    const m = await mount(PAGE, b.dd);
    b.fail();
    await settle();
    assert.equal(m.last().decodes, null);
});

// What lets the hook go without a flag of its own: preact renders nothing for
// an unmounted component. If that ever changes, this goes red.
test('unmounted before the probe answers: nothing is gathered for it any more', async () => {
    const b = browser();
    const m = await mount(PAGE, b.dd);
    const before = m.seen.length;
    render(null, m.root);
    b.answer();
    await settle();
    assert.equal(m.seen.length, before, 'no render after unmount');
});
