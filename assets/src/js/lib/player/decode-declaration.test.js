import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import {
    OPTIN_KEY, CACHE_KEY, MEMORY_KEY, MEMORY_TTL_MS, CACHE_TTL_MS, TOKENS,
    applyUrlSwitch, takesPart, startProbe, whenDeclared, declaredTokens, declarationFor, decodedTokens,
    rememberFallback, loadMemory, installSubmitHook, applyDeclaration, initDecodeDeclaration,
} from './decode-declaration.js';
import { DECODE_TOKENS } from './codec-support.js';

const UA_A = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/140.0';
const UA_B = 'Mozilla/5.0 (Macintosh) Safari/605.1.15';
const ALL = ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160'];

// A page of its own for every test: the module keeps its state on window.
function page({ url = 'https://webtor.io/', ua = UA_A, storageThrows = false } = {}) {
    const dom = new JSDOM('<!doctype html><body></body>', { url });
    const win = dom.window;
    Object.defineProperty(win.navigator, 'userAgent', { value: ua, configurable: true });
    if (storageThrows) {
        Object.defineProperty(win, 'localStorage', { get() { throw new win.DOMException('denied', 'SecurityError'); }, configurable: true });
    }
    return win;
}

const optIn = (win) => win.localStorage.setItem(OPTIN_KEY, 'on');

// A probe module whose answers the test holds: the HEVC tokens at once, the
// PQ answer when the test settles it (or never).
function fakeProbe({ hevc = ALL, pq = true } = {}) {
    let settle;
    const pqPromise = pq === 'never' ? new Promise(() => {}) : pq === 'later' ? new Promise((r) => { settle = r; }) : Promise.resolve(pq);
    return {
        load: async () => ({ declarationSupport: () => ({ path: 'mse', hevc, pq: pqPromise }), envFromWindow: () => ({}) }),
        answerPQ: (v) => settle(v),
    };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

function startForm(win, { action = '/ru/stream-video', rid = 'res1', iid = 'item1' } = {}) {
    const f = win.document.createElement('form');
    f.setAttribute('action', action);
    f.setAttribute('method', 'post');
    f.innerHTML = `<input type="hidden" name="resource-id" value="${rid}"><input type="hidden" name="item-id" value="${iid}">`;
    win.document.body.appendChild(f);
    return f;
}
const decodeOf = (form) => {
    const el = form.querySelector('input[name="decode"]');
    return el ? el.value : null;
};

test('the token list is the probe\'s', () => {
    assert.deepEqual(TOKENS, DECODE_TOKENS);
});

// ---- who takes part --------------------------------------------------------

test('?passthrough=on opts this browser in, ?passthrough=off out; nothing else touches it', () => {
    let win = page({ url: 'https://webtor.io/?passthrough=on' });
    assert.equal(takesPart(win), false, 'not before the switch is read');
    assert.equal(applyUrlSwitch(win), 'on');
    assert.equal(takesPart(win), true);
    assert.equal(win.localStorage.getItem(OPTIN_KEY), 'on');
    // A later page of the same browser.
    const store = win.localStorage.getItem(OPTIN_KEY);
    win = page({ url: 'https://webtor.io/ru/some?x=1' });
    win.localStorage.setItem(OPTIN_KEY, store);
    assert.equal(applyUrlSwitch(win), null);
    assert.equal(takesPart(win), true);
    win = page({ url: 'https://webtor.io/?passthrough=off' });
    win.localStorage.setItem(OPTIN_KEY, 'on');
    assert.equal(applyUrlSwitch(win), 'off');
    assert.equal(takesPart(win), false);
    assert.equal(win.localStorage.getItem(OPTIN_KEY), 'off');
    win = page({ url: 'https://webtor.io/?passthrough=yes' });
    assert.equal(applyUrlSwitch(win), null);
    assert.equal(takesPart(win), false);
});

test('a throwing localStorage and no switch on the address: not taking part, and no exception', () => {
    const win = page({ storageThrows: true });
    assert.equal(takesPart(win), false);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), null);
    assert.equal(declaredTokens(win), null);
});

test('without storage the switch holds for the page', () => {
    const win = page({ url: 'https://webtor.io/?passthrough=on', storageThrows: true });
    assert.equal(applyUrlSwitch(win), 'on');
    assert.equal(takesPart(win), true);
});

test('a page that does not take part declares nothing and runs no probe', async () => {
    const win = page();
    initDecodeDeclaration(win, win.document);
    assert.equal(win.__wtDecode.probe, null, 'no probe was started');
    const f = startForm(win);
    const stale = win.document.createElement('input');
    stale.name = 'decode';
    stale.value = 'hevc8';
    stale.type = 'hidden';
    f.appendChild(stale);
    f.dispatchEvent(new win.Event('submit', { bubbles: true, cancelable: true }));
    assert.equal(decodeOf(f), null, 'a stale field is taken off');
    assert.equal(declaredTokens(win), null);
});

// ---- what it declares ------------------------------------------------------

test('a complete probe: its tokens, and the answer is cached for this browser', async () => {
    const win = page();
    optIn(win);
    await startProbe(win, fakeProbe({ pq: true }));
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq');
    assert.deepEqual(declaredTokens(win), [...ALL, 'hdr-pq']);
    const cache = JSON.parse(win.localStorage.getItem(CACHE_KEY));
    assert.equal(cache.ua, UA_A);
    assert.deepEqual(cache.tokens, [...ALL, 'hdr-pq']);
});

test('a browser that decodes nothing sends no field (not an empty one)', async () => {
    const win = page();
    optIn(win);
    await startProbe(win, fakeProbe({ hevc: [], pq: false }));
    assert.equal(declarationFor(win, {}), null);
    assert.deepEqual(declaredTokens(win), []);
});

// The rule the whole declaration is built around: a check that has not
// answered is not a browser that cannot decode.
test('HEVC known, PQ not answered yet: the cache of this browser, else unknown -- never HEVC without hdr-pq', async () => {
    let win = page();
    optIn(win);
    const p = fakeProbe({ pq: 'later' });
    startProbe(win, p);
    await tick();
    assert.equal(declarationFor(win, {}), 'unknown');
    assert.equal(declaredTokens(win), null);
    p.answerPQ(false);
    await win.__wtDecode.probe;
    assert.equal(declarationFor(win, {}), 'hevc8,hevc10,hevc8-2160,hevc10-2160', 'the answer, once it came');

    win = page();
    optIn(win);
    win.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: UA_A, tokens: ['hevc10', 'hdr-pq'], at: Date.now() - 1000 }));
    startProbe(win, fakeProbe({ pq: 'never' }));
    await tick();
    assert.equal(declarationFor(win, {}), 'hevc10,hdr-pq');
});

// Discover's switches ask what the browser decodes of every browser, not
// only of those that declare: the same probe, cache and memory, without the
// opt-in.
test('decodedTokens: the browser\'s answer whether or not the page takes part; null until there is one', async () => {
    let win = page();
    assert.equal(decodedTokens(win), null, 'no probe, no cache: not answered');
    await startProbe(win, fakeProbe({ pq: true }));
    assert.equal(takesPart(win), false);
    assert.deepEqual(decodedTokens(win), [...ALL, 'hdr-pq'], 'answered, though this page declares nothing');
    assert.equal(declaredTokens(win), null, 'and the declaration still waits for the opt-in');

    win = page();
    await startProbe(win, fakeProbe({ hevc: [], pq: false }));
    assert.deepEqual(decodedTokens(win), [], 'decodes none is an answer, not null');

    win = page();
    const p = fakeProbe({ pq: 'later' });
    startProbe(win, p);
    await tick();
    assert.equal(decodedTokens(win), null, 'HEVC known but PQ not: not answered yet');
    p.answerPQ(true);
    await win.__wtDecode.probe;
    assert.deepEqual(decodedTokens(win), [...ALL, 'hdr-pq']);

    win = page();
    win.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: UA_A, tokens: ['hevc8'], at: Date.now() - 1000 }));
    assert.deepEqual(decodedTokens(win), ['hevc8'], 'this browser\'s cached answer counts at once');
});

test('decodedTokens: the memory of failures takes its classes out here too', async () => {
    const win = page();
    await startProbe(win, fakeProbe({ pq: true }));
    const now = Date.now();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'hevc10-2160', strike: true }, now - 200);
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'hevc10-2160', strike: true }, now - 100);
    assert.deepEqual(decodedTokens(win, now), ['hevc8', 'hevc10', 'hevc8-2160', 'hdr-pq']);
});

test('decodedTokens: a throwing localStorage answers null, not an exception', () => {
    const win = page({ storageThrows: true });
    assert.equal(decodedTokens(win), null);
});

test('the cache of another browser, an old one or a future one is not used', async () => {
    const now = Date.now();
    for (const [name, c] of [
        ['another UA', { ua: UA_B, tokens: ['hevc10'], at: now - 1000 }],
        ['older than 30 days', { ua: UA_A, tokens: ['hevc10'], at: now - CACHE_TTL_MS - 1000 }],
        ['from the future', { ua: UA_A, tokens: ['hevc10'], at: now + 60000 }],
        ['garbage', 'not json'],
    ]) {
        const win = page();
        optIn(win);
        win.localStorage.setItem(CACHE_KEY, typeof c === 'string' ? c : JSON.stringify(c));
        assert.equal(declarationFor(win, {}, now), 'unknown', name);
    }
});

test('before any probe or cache: unknown; the cache keeps only known tokens', () => {
    const win = page();
    optIn(win);
    assert.equal(declarationFor(win, {}), 'unknown');
    win.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: UA_A, tokens: ['av1', 'hevc10', 'hdr-pq', 'x'], at: Date.now() }));
    assert.equal(declarationFor(win, {}), 'hevc10,hdr-pq');
});

test('whenDeclared: the probe\'s answer when it comes within the wait, else the wait', async () => {
    const win = page();
    optIn(win);
    const p = fakeProbe({ pq: 'later' });
    startProbe(win, p);
    setTimeout(() => p.answerPQ(true), 100);
    const t0 = Date.now();
    await whenDeclared(win, 300);
    const waited = Date.now() - t0;
    assert.ok(waited >= 80 && waited < 290, `waited ${waited} ms`);
    assert.equal(declarationFor(win, {}), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq');

    const win2 = page();
    optIn(win2);
    startProbe(win2, fakeProbe({ pq: 'never' }));
    const t1 = Date.now();
    await whenDeclared(win2, 120);
    assert.ok(Date.now() - t1 >= 110, 'a silent probe is waited for, up to the limit');
    assert.equal(declarationFor(win2, {}), 'unknown');

    const win3 = page();
    const t2 = Date.now();
    await whenDeclared(win3, 5000);
    assert.ok(Date.now() - t2 < 100, 'no probe: nothing to wait for');
});

// ---- the memory of failures ------------------------------------------------

test('a file that failed passthrough here is started without a declaration', async () => {
    const win = page();
    optIn(win);
    await startProbe(win, fakeProbe());
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'hevc10', strike: false });
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), null);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'other' }), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq');
});

test('strikes: two on different files take the covering tokens out; one, or two on one file, do not', async () => {
    const now = Date.now();
    const fresh = async () => {
        const win = page();
        optIn(win);
        await startProbe(win, fakeProbe());
        return win;
    };
    let win = await fresh();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'hevc10', strike: true }, now - 1000);
    assert.equal(declarationFor(win, {}, now), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq', 'one strike is the file\'s');
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'hevc10', strike: true }, now - 500);
    assert.equal(declarationFor(win, {}, now), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq', 'two on one file are still the file\'s');
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'hevc10', strike: true }, now - 100);
    assert.equal(declarationFor(win, {}, now), 'hevc8,hevc8-2160,hdr-pq', 'hevc10 failed on two files: it and the class above go');

    win = await fresh();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'hevc8', strike: true }, now - MEMORY_TTL_MS - 1000);
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'hevc8', strike: true }, now - 100);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'c' }, now), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq', 'a strike older than 7 days is gone');

    win = await fresh();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'hevc8', strike: true }, now - 2000);
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'hevc8', strike: true }, now - 1000);
    assert.equal(declarationFor(win, {}, now), 'hdr-pq', 'Main failing takes every HEVC token; hdr-pq stays');

    win = await fresh();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'hevc10-2160', strike: true }, now - 2000);
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'hevc10-2160', strike: true }, now - 1000);
    assert.equal(declarationFor(win, {}, now), 'hevc8,hevc10,hevc8-2160,hdr-pq');

    win = await fresh();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'unknown', strike: true }, now - 2000);
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'hdr-pq', strike: true }, now - 1000);
    assert.equal(declarationFor(win, {}, now), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq', 'no class, or a token that is not a class, strikes nothing');
});

test('without storage the memory lives on the page', async () => {
    const win = page({ storageThrows: true, url: 'https://webtor.io/?passthrough=on' });
    applyUrlSwitch(win);
    await startProbe(win, fakeProbe());
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'hevc10', strike: true });
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), null);
    assert.deepEqual(Object.keys(loadMemory(win).sources), ['r/i']);
});

test('the stored memory is merged with the page\'s, and survives to the next page', async () => {
    const win = page();
    optIn(win);
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'hevc8', strike: true });
    const stored = win.localStorage.getItem(MEMORY_KEY);
    const next = page();
    optIn(next);
    next.localStorage.setItem(MEMORY_KEY, stored);
    assert.equal(declarationFor(next, { resourceId: 'r', itemId: 'i' }), null);
});

// ---- the forms -------------------------------------------------------------

// Turnstile stops the first pass of an anonymous submit and submits again
// with a marker (turnstileAction.js). Whatever the order of the two capture
// listeners, the pass that gets through carries the declaration as it is at
// that moment -- written fresh, not left from an earlier pass.
for (const order of ['hook first', 'turnstile first']) {
    test(`two passes of a Turnstile submit, ${order}: the pass that goes through carries the current declaration`, async () => {
        const win = page();
        optIn(win);
        const through = [];
        const turnstile = (e) => {
            const form = e.target;
            if (form.dataset.ready) { delete form.dataset.ready; return; }
            e.preventDefault();
            e.stopImmediatePropagation();
            setTimeout(() => { form.dataset.ready = '1'; form.requestSubmit(); }, 0);
        };
        if (order === 'turnstile first') win.document.addEventListener('submit', turnstile, true);
        installSubmitHook(win.document, win);
        if (order === 'hook first') win.document.addEventListener('submit', turnstile, true);
        win.document.addEventListener('submit', (e) => { e.preventDefault(); through.push(decodeOf(e.target)); });
        const f = startForm(win);
        const p = fakeProbe({ pq: 'later' });
        startProbe(win, p);
        await tick();
        f.requestSubmit();
        // Between the passes the probe answers: the second pass says so.
        p.answerPQ(true);
        await win.__wtDecode.probe;
        await new Promise((r) => setTimeout(r, 10));
        assert.deepEqual(through, ['hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq']);
        installSubmitHook(win.document, win);
        f.dataset.ready = '1';
        f.requestSubmit();
        assert.equal(through.length, 2, 'the hook is installed once');
    });
}

test('only a video stream start is declared: not a download, not audio', async () => {
    const win = page();
    optIn(win);
    await startProbe(win, fakeProbe());
    installSubmitHook(win.document, win);
    for (const action of ['/download-file', '/ru/stream-audio', '/stream-video/subtitle', '/preview-image']) {
        const f = startForm(win, { action });
        f.addEventListener('submit', (e) => e.preventDefault());
        f.requestSubmit();
        assert.equal(decodeOf(f), null, action);
    }
    const v = startForm(win, { action: '/stream-video' });
    v.addEventListener('submit', (e) => e.preventDefault());
    v.requestSubmit();
    assert.equal(decodeOf(v), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq');
});

test('applyDeclaration updates in place and removes when there is nothing to say', async () => {
    const win = page();
    optIn(win);
    const f = startForm(win);
    applyDeclaration(f, win);
    assert.equal(decodeOf(f), 'unknown');
    await startProbe(win, fakeProbe({ pq: false }));
    applyDeclaration(f, win);
    assert.equal(decodeOf(f), 'hevc8,hevc10,hevc8-2160,hevc10-2160');
    assert.equal(f.querySelectorAll('input[name="decode"]').length, 1);
    rememberFallback(win, { resourceId: 'res1', itemId: 'item1' });
    applyDeclaration(f, win);
    assert.equal(decodeOf(f), null);
});

// The layout runs this on every page before Turnstile and the async
// navigation (app/layout.js): a browser whose storage throws must get
// through it without an exception, and still declare what it can.
test('a throwing localStorage: init does not throw, the hook still works on page memory', async () => {
    const win = page({ storageThrows: true, url: 'https://webtor.io/?passthrough=on' });
    startProbe(win, fakeProbe()); // the init below finds it running
    assert.doesNotThrow(() => initDecodeDeclaration(win, win.document));
    assert.equal(takesPart(win), true);
    await win.__wtDecode.probe;
    rememberFallback(win, { resourceId: 'res1', itemId: 'item1' });
    const failed = startForm(win);
    const other = startForm(win, { iid: 'item2' });
    for (const f of [failed, other]) {
        f.addEventListener('submit', (e) => e.preventDefault());
        f.requestSubmit();
    }
    assert.equal(decodeOf(failed), null, 'the file that failed, remembered on the page');
    assert.equal(decodeOf(other), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq');
});

// The layout runs this before Turnstile and the async navigation: whatever
// throws inside must stay inside (read() already catches storage; this is
// the net under everything else -- a document that refuses a listener here).
test('initDecodeDeclaration never throws into the layout', () => {
    const win = page({ url: 'https://webtor.io/?passthrough=on' });
    const errors = [];
    const origError = console.error;
    console.error = (...a) => errors.push(a);
    try {
        const doc = { addEventListener() { throw new Error('no listeners here'); } };
        assert.doesNotThrow(() => initDecodeDeclaration(win, doc));
    } finally {
        console.error = origError;
    }
    assert.equal(errors.length, 1, 'said once, on the console');
    assert.equal(takesPart(win), true, 'what ran before the throw stands');
});
