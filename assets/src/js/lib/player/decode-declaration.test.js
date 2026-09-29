import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import {
    OPTIN_KEY, AUDIO_OPTIN_KEY, MMS_OPTIN_KEY, CACHE_KEY, AUDIO_CACHE_KEY, MEMORY_KEY, MEMORY_TTL_MS, CACHE_TTL_MS, TOKENS, VIDEO_TOKENS, AUDIO_TOKENS,
    AUDIO_STRUCK_BY_CLASS, AUDIO_DROP_BY_CLASS, STRUCK_BY_CLASS,
    applyUrlSwitch, applyAudioUrlSwitch, applyMmsUrlSwitch, iosPlaysHlsJs, takesPart, takesPartAudio, startProbe, whenDeclared, declaredTokens, declarationFor, decodedTokens,
    rememberFallback, loadMemory, installSubmitHook, applyDeclaration, initDecodeDeclaration,
    setPendingFallback, clearPendingFallback, declaresAudio, isAudioClass,
} from './decode-declaration.js';
import { DECODE_TOKENS, DECODE_VIDEO_TOKENS, DECODE_AUDIO_TOKENS } from './codec-support.js';

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
const optOut = (win) => win.localStorage.setItem(OPTIN_KEY, 'off');
// The audio part has an opt-in of its own (`?audio=on`): a test about the
// audio tokens opts into both.
const optInAudio = (win) => win.localStorage.setItem(AUDIO_OPTIN_KEY, 'on');
const optInBoth = (win) => { optIn(win); optInAudio(win); };

// A probe module whose answers the test holds: the HEVC tokens at once, the
// PQ answer and the audio part when the test settles them (or never). The
// audio part answers none unless the test says.
function fakeProbe({ hevc = ALL, pq = true, audio = [] } = {}) {
    let settle;
    let settleAudio;
    const later = (v, set) => (v === 'never' ? new Promise(() => {}) : v === 'later' ? new Promise((r) => { set(r); }) : Promise.resolve(v));
    const pqPromise = later(pq, (r) => { settle = r; });
    const audioPromise = later(audio, (r) => { settleAudio = r; });
    return {
        load: async () => ({ declarationSupport: () => ({ path: 'mse', hevc, pq: pqPromise, audio: audioPromise }), envFromWindow: () => ({}) }),
        answerPQ: (v) => settle(v),
        answerAudio: (v) => settleAudio(v),
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
    assert.deepEqual(VIDEO_TOKENS, DECODE_VIDEO_TOKENS);
    assert.deepEqual(AUDIO_TOKENS, DECODE_AUDIO_TOKENS);
});

// ---- who takes part --------------------------------------------------------

// Stage 5 (2026-09-28): every browser takes part unless it opted out.
test('every browser takes part by default; ?passthrough=off opts it out, ?passthrough=on back in; nothing else touches it', () => {
    let win = page();
    assert.equal(takesPart(win), true, 'no switch ever read: takes part');
    win = page({ url: 'https://webtor.io/?passthrough=on' });
    assert.equal(takesPart(win), true, 'before the switch is read too');
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
    // An opted-out browser stays out on its later pages.
    win = page({ url: 'https://webtor.io/ru/other' });
    win.localStorage.setItem(OPTIN_KEY, 'off');
    assert.equal(applyUrlSwitch(win), null);
    assert.equal(takesPart(win), false);
    win = page({ url: 'https://webtor.io/?passthrough=yes' });
    assert.equal(applyUrlSwitch(win), null);
    assert.equal(takesPart(win), true, 'a value that is not on/off changes nothing');
});

// No storage means no opt-out could have been saved: the browser takes
// part, and nothing throws.
test('a throwing localStorage and no switch on the address: takes part, and no exception', () => {
    const win = page({ storageThrows: true });
    assert.equal(takesPart(win), true);
    assert.doesNotThrow(() => declarationFor(win, { resourceId: 'r', itemId: 'i' }));
    assert.equal(declaredTokens(win), null, 'no probe answer yet');
});

test('without storage the switch holds for the page', () => {
    const win = page({ url: 'https://webtor.io/?passthrough=on', storageThrows: true });
    assert.equal(applyUrlSwitch(win), 'on');
    assert.equal(takesPart(win), true);
});

test('a page that does not take part (opted out) declares nothing and runs no probe', async () => {
    const win = page();
    optOut(win);
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
    optOut(win);
    assert.equal(decodedTokens(win), null, 'no probe, no cache: not answered');
    await startProbe(win, fakeProbe({ pq: true }));
    assert.equal(takesPart(win), false);
    assert.deepEqual(decodedTokens(win), [...ALL, 'hdr-pq'], 'answered, though this page declares nothing');
    assert.equal(declaredTokens(win), null, 'and an opted-out page declares nothing');

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

// An audio file's start declares nothing either, the audio tokens included:
// they are about the audio of a film (content-transcoder's decision is per
// session, and the audio player's sessions stay as they are).
test('only a video stream start is declared: not a download, not audio', async () => {
    const win = page();
    optInBoth(win);
    await startProbe(win, fakeProbe({ audio: ['aac51'] }));
    await win.__wtDecode.audio;
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
    assert.equal(decodeOf(v), 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq,aac51');
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

// ---- the audio part (aac51, ac3, ec3: multichannel audio) --------------------

const VIDEO_ALL = 'hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq';
const settledIn = (p, ms = 50) => Promise.race([p.then(() => 'settled'), new Promise((r) => setTimeout(() => r('pending'), ms))]);

test('the audio part follows the video part, and is cached for this browser on its own', async () => {
    const win = page();
    optInBoth(win);
    await startProbe(win, fakeProbe({ audio: ['ec3', 'aac51'] }));
    await win.__wtDecode.audio;
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), `${VIDEO_ALL},aac51,ec3`);
    const cache = JSON.parse(win.localStorage.getItem(AUDIO_CACHE_KEY));
    assert.equal(cache.ua, UA_A);
    assert.deepEqual(cache.tokens, ['aac51', 'ec3']);
    assert.deepEqual(JSON.parse(win.localStorage.getItem(CACHE_KEY)).tokens, [...ALL, 'hdr-pq'], 'the video cache holds the video part only');
});

test('a browser that decodes no HEVC declares its audio alone; Discover still reads "no HEVC"', async () => {
    const win = page();
    optInBoth(win);
    await startProbe(win, fakeProbe({ hevc: [], pq: false, audio: ['aac51', 'ac3', 'ec3'] }));
    await win.__wtDecode.audio;
    assert.equal(declarationFor(win, {}), 'aac51,ac3,ec3');
    assert.deepEqual(declaredTokens(win), [], 'the video it declares: none');
    assert.deepEqual(decodedTokens(win), []);
});

// A missing audio token is the stereo the transcoder has always made: the
// video goes on without waiting, and the audio is this browser's cached
// answer or nothing -- never `unknown`, which is about the video.
test('the audio part not answered: the video goes with the cached audio, else without; never unknown', async () => {
    let win = page();
    optInBoth(win);
    await startProbe(win, fakeProbe({ audio: 'never' }));
    assert.equal(declarationFor(win, {}), VIDEO_ALL);

    win = page();
    optInBoth(win);
    win.localStorage.setItem(AUDIO_CACHE_KEY, JSON.stringify({ ua: UA_A, tokens: ['aac51'], at: Date.now() - 1000 }));
    const p = fakeProbe({ audio: 'later' });
    await startProbe(win, p);
    assert.equal(declarationFor(win, {}), `${VIDEO_ALL},aac51`, 'the cache stands in');
    p.answerAudio(['aac51', 'ec3']);
    await win.__wtDecode.audio;
    assert.equal(declarationFor(win, {}), `${VIDEO_ALL},aac51,ec3`, 'the answer, once it came');

    win = page();
    optInBoth(win);
    await startProbe(win, fakeProbe({ hevc: [], pq: false, audio: 'never' }));
    assert.equal(declarationFor(win, {}), null, 'no HEVC and no audio answer yet: no field, as before');
});

// `unknown` says the video check had not answered. Audio tokens beside it
// would make the server drop it (models.ParseDecodeDeclaration), and audio
// tokens alone read at the transcoder as "no HEVC".
test('the video part not answered: unknown alone, whatever the audio answered', async () => {
    const win = page();
    optInBoth(win);
    const p = fakeProbe({ pq: 'later', audio: ['aac51', 'ec3'] });
    startProbe(win, p);
    assert.equal(await settledIn(win.__wtDecode.audio), 'settled', 'the audio part answers while the video part waits');
    assert.equal(declarationFor(win, {}), 'unknown');
    p.answerPQ(false);
    await win.__wtDecode.probe;
    assert.equal(declarationFor(win, {}), 'hevc8,hevc10,hevc8-2160,hevc10-2160,aac51,ec3');
});

// Discover re-reads the browser's answer when startProbe's promise
// settles (usePlaybackContext): the audio question must not hold it, and
// its answer must not change what Discover reads.
test('Discover\'s view is the video part: startProbe settles without the audio, which changes nothing there', async () => {
    let win = page();
    optInBoth(win);
    const p = fakeProbe({ audio: 'never' });
    assert.equal(await settledIn(startProbe(win, p)), 'settled', 'a silent audio check does not hold the video answer');
    assert.deepEqual(decodedTokens(win), [...ALL, 'hdr-pq']);
    assert.deepEqual(declaredTokens(win), [...ALL, 'hdr-pq']);

    win = page();
    optInBoth(win);
    await startProbe(win, fakeProbe({ audio: ['aac51', 'ac3', 'ec3'] }));
    await win.__wtDecode.audio;
    assert.deepEqual(decodedTokens(win), [...ALL, 'hdr-pq'], 'no audio token in it');
    assert.deepEqual(declaredTokens(win), [...ALL, 'hdr-pq']);

    win = page();
    optInBoth(win);
    startProbe(win, fakeProbe({ pq: 'never', audio: ['aac51'] }));
    assert.equal(await settledIn(win.__wtDecode.audio), 'settled');
    assert.equal(decodedTokens(win), null, 'the audio answered, the video not: not answered');
});

test('whenDeclared waits for both parts, within the limit', async () => {
    const win = page();
    optInBoth(win);
    const p = fakeProbe({ audio: 'later' });
    startProbe(win, p);
    setTimeout(() => p.answerAudio(['aac51']), 100);
    const t0 = Date.now();
    await whenDeclared(win, 300);
    const waited = Date.now() - t0;
    assert.ok(waited >= 80 && waited < 290, `waited ${waited} ms`);
    assert.equal(declarationFor(win, {}), `${VIDEO_ALL},aac51`);

    const win2 = page();
    optInBoth(win2);
    startProbe(win2, fakeProbe({ audio: 'never' }));
    const t1 = Date.now();
    await whenDeclared(win2, 120);
    assert.ok(Date.now() - t1 >= 110, 'a silent audio check is waited for, up to the limit');
    assert.equal(declarationFor(win2, {}), VIDEO_ALL);
});

test('an audio part that rejects, or a probe without one, answers none', async () => {
    let win = page();
    optInBoth(win);
    await startProbe(win, fakeProbe({ audio: Promise.reject(new Error('nope')) }));
    await win.__wtDecode.audio;
    assert.equal(declarationFor(win, {}), VIDEO_ALL);
    await whenDeclared(win, 5000);

    win = page();
    optInBoth(win);
    await startProbe(win, { load: async () => ({ declarationSupport: () => ({ path: 'mse', hevc: ['hevc8'], pq: Promise.resolve(false) }), envFromWindow: () => ({}) }) });
    await win.__wtDecode.audio;
    assert.equal(declarationFor(win, {}), 'hevc8');
    assert.deepEqual(JSON.parse(win.localStorage.getItem(AUDIO_CACHE_KEY)).tokens, []);
});

test('the audio cache: this browser\'s, fresh, and audio tokens only; the video cache brings no audio', () => {
    const now = Date.now();
    const video = JSON.stringify({ ua: UA_A, tokens: ['hevc10', 'aac51'], at: now - 1000 });
    for (const [name, c, want] of [
        ['this browser', { ua: UA_A, tokens: ['ec3', 'hevc8', 'x', 'aac51'], at: now - 1000 }, 'hevc10,aac51,ec3'],
        ['another UA', { ua: UA_B, tokens: ['aac51'], at: now - 1000 }, 'hevc10'],
        ['older than 30 days', { ua: UA_A, tokens: ['aac51'], at: now - CACHE_TTL_MS - 1000 }, 'hevc10'],
        ['from the future', { ua: UA_A, tokens: ['aac51'], at: now + 60000 }, 'hevc10'],
        ['garbage', 'not json', 'hevc10'],
    ]) {
        const win = page();
        optInBoth(win);
        win.localStorage.setItem(CACHE_KEY, video);
        win.localStorage.setItem(AUDIO_CACHE_KEY, typeof c === 'string' ? c : JSON.stringify(c));
        assert.equal(declarationFor(win, {}, now), want, name);
    }
    const bare = page();
    optInBoth(bare);
    bare.localStorage.setItem(AUDIO_CACHE_KEY, JSON.stringify({ ua: UA_A, tokens: ['aac51'], at: now - 1000 }));
    assert.equal(declarationFor(bare, {}, now), 'unknown', 'no video answer, cached or fresh: unknown, alone');
});

test('the memory of failures: a failed passthrough declares no audio either; a video class\'s strikes take no audio token', async () => {
    const now = Date.now();
    const win = page();
    optInBoth(win);
    await startProbe(win, fakeProbe({ audio: ['aac51', 'ec3'] }));
    await win.__wtDecode.audio;
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'hevc10' }, now - 3000);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }, now), null, 'the old route for the file, stereo included');
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'hevc8', strike: true }, now - 2000);
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'hevc8', strike: true }, now - 1000);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'c' }, now), 'hdr-pq,aac51,ec3');
});

// The real probe (codec-support.js) behind the declaration, over a
// browser the test describes: the whole declaration in the transcoder's
// order (the order models.ParseDecodeDeclaration keeps: canonical as sent),
// and Firefox on Windows sending its audio without any HEVC.
test('with the real probe: every token in order; Firefox on Windows sends its audio alone', async () => {
    const H264 = 'video/mp4;codecs=avc1.42E01E,mp4a.40.2';
    const env = (ua) => ({
        userAgent: ua,
        MediaSource: Object.assign(function () {}, {
            isTypeSupported: (t) => t === H264 || t.startsWith('video/mp4;codecs=hvc1.') || t === 'audio/mp4;codecs=ac-3' || t === 'audio/mp4;codecs=ec-3',
        }),
        mediaCapabilities: { decodingInfo: async () => ({ supported: true, smooth: true, powerEfficient: true }) },
    });
    let win = page({ ua: UA_A });
    optInBoth(win);
    await startProbe(win, { env: env(UA_A) });
    await win.__wtDecode.audio;
    assert.equal(declarationFor(win, {}), DECODE_TOKENS.join(','));

    const ffWin = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:143.0) Gecko/20100101 Firefox/143.0';
    win = page({ ua: ffWin });
    optInBoth(win);
    await startProbe(win, { env: env(ffWin) });
    await win.__wtDecode.audio;
    assert.equal(declarationFor(win, {}), 'aac51,ac3,ec3');
    assert.deepEqual(declaredTokens(win), []);
});

// ---- a failure of the multichannel audio (the audio classes) -----------------

const AUDIO_ALL = ['aac51', 'ac3', 'ec3'];
async function audioPage(opts = {}) {
    const win = page(opts);
    try { optInBoth(win); } catch (e) { applyUrlSwitch(win); applyAudioUrlSwitch(win); }
    await startProbe(win, fakeProbe({ audio: AUDIO_ALL }));
    await win.__wtDecode.audio;
    return win;
}

test('declaresAudio: an audio token, matched exactly', () => {
    for (const [decl, want] of [
        [null, false], [undefined, false], ['', false], ['unknown', false], [VIDEO_ALL, false],
        ['aac51', true], ['hevc8,ec3', true], ['hevc8, ac3', true], ['aac5', false], ['ec3x', false],
    ]) {
        assert.equal(declaresAudio(decl), want, String(decl));
    }
});

test('the audio classes: their own tokens, and never a video class', () => {
    assert.deepEqual(AUDIO_STRUCK_BY_CLASS, { dolby: ['ac3', 'ec3'], aac51: ['aac51'] });
    assert.deepEqual(AUDIO_DROP_BY_CLASS, { dolby: ['ac3', 'ec3'], aac51: ['aac51', 'ac3', 'ec3'] });
    for (const cls of Object.keys(STRUCK_BY_CLASS)) assert.equal(isAudioClass(cls), false, cls);
    for (const t of [...Object.values(AUDIO_STRUCK_BY_CLASS), ...Object.values(AUDIO_DROP_BY_CLASS)].flat()) {
        assert.ok(AUDIO_TOKENS.includes(t), t);
    }
    assert.equal(isAudioClass('constructor'), false, 'not a key of every object');
    assert.equal(isAudioClass(undefined), false);
});

test('a file whose Dolby failed: its next start keeps the video and AAC 5.1, drops Dolby; other files keep all', async () => {
    const win = await audioPage();
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'dolby' });
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), `${VIDEO_ALL},aac51`);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'other' }), `${VIDEO_ALL},aac51,ac3,ec3`);
    assert.deepEqual(loadMemory(win).sources, {}, 'not the file that failed passthrough: its video is fine');
    assert.deepEqual(decodedTokens(win), ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160', 'hdr-pq'], 'Discover reads the same');
});

test('a file whose AAC 5.1 failed: its next start declares no audio token, and the video as before', async () => {
    const win = await audioPage();
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'aac51' });
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), VIDEO_ALL);
    // A browser without HEVC declares nothing at all for it.
    const bare = page();
    optInBoth(bare);
    await startProbe(bare, fakeProbe({ hevc: [], audio: AUDIO_ALL }));
    await bare.__wtDecode.audio;
    rememberFallback(bare, { resourceId: 'r', itemId: 'i', cls: 'aac51' });
    assert.equal(declarationFor(bare, { resourceId: 'r', itemId: 'i' }), null);
    assert.equal(declarationFor(bare, { resourceId: 'r', itemId: 'j' }), 'aac51,ac3,ec3');
});

test('audio strikes: two files take the class\'s own tokens out of every declaration; the video is untouched', async () => {
    const now = Date.now();
    let win = await audioPage();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'dolby', strike: true }, now - 2000);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'c' }, now), `${VIDEO_ALL},aac51,ac3,ec3`, 'one strike is the file\'s');
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'dolby', strike: true }, now - 1000);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'c' }, now), `${VIDEO_ALL},aac51`);
    assert.deepEqual(decodedTokens(win, now), VIDEO_ALL.split(','));

    win = await audioPage();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'aac51', strike: true }, now - 2000);
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'aac51', strike: true }, now - 1000);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'c' }, now), `${VIDEO_ALL},ac3,ec3`, 'aac51 goes; Dolby is another decoder');

    win = await audioPage();
    rememberFallback(win, { resourceId: 'r', itemId: 'a', cls: 'dolby', strike: true }, now - MEMORY_TTL_MS - 1000);
    rememberFallback(win, { resourceId: 'r', itemId: 'b', cls: 'dolby', strike: true }, now - 1000);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'c' }, now), `${VIDEO_ALL},aac51,ac3,ec3`, 'a strike older than 7 days is gone');
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'a' }, now), `${VIDEO_ALL},aac51,ac3,ec3`, 'and so is the file\'s own drop');
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'b' }, now), `${VIDEO_ALL},aac51`);
});

test('the audio memory survives to the next page, merges with the page\'s, and ignores what it does not know', async () => {
    const now = Date.now();
    const win = await audioPage();
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'dolby', strike: true }, now - 1000);
    const stored = JSON.parse(win.localStorage.getItem(MEMORY_KEY));
    assert.deepEqual(stored.audio, { 'r/i': { dolby: now - 1000 } });
    const next = await audioPage();
    stored.audio['r/x'] = { hevc10: now - 10, other: now - 10 };
    stored.audio['r/y'] = 'garbage';
    stored.audio['r/z'] = { aac51: now + 60000 };
    next.localStorage.setItem(MEMORY_KEY, JSON.stringify(stored));
    rememberFallback(next, { resourceId: 'r', itemId: 'j', cls: 'aac51' }, now - 500);
    const m = loadMemory(next, now);
    assert.deepEqual(m.audio, { 'r/i': { dolby: now - 1000 }, 'r/j': { aac51: now - 500 } });
    assert.equal(declarationFor(next, { resourceId: 'r', itemId: 'i' }, now), `${VIDEO_ALL},aac51`);
    assert.equal(declarationFor(next, { resourceId: 'r', itemId: 'x' }, now), `${VIDEO_ALL},aac51,ac3,ec3`);
});

test('without storage the audio memory lives on the page', async () => {
    const win = await audioPage({ storageThrows: true, url: 'https://webtor.io/?passthrough=on&audio=on' });
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'dolby' });
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), `${VIDEO_ALL},aac51`);
});

test('the restart note: an audio class declares the rest, a video class nothing; both carry why', async () => {
    const win = await audioPage();
    const f = startForm(win);
    setPendingFallback(win, { resourceId: 'res1', itemId: 'item1', reason: 'decode_error', cls: 'dolby' });
    applyDeclaration(f, win);
    assert.equal(decodeOf(f), `${VIDEO_ALL},aac51`, 'before the memory knows: the note alone leaves Dolby out');
    assert.equal(f.querySelector('input[name="decode-fallback"]').value, 'decode_error');
    assert.equal(f.querySelector('input[name="decode-class"]').value, 'dolby');
    setPendingFallback(win, { resourceId: 'res1', itemId: 'item1', reason: 'media_error', cls: 'aac51' });
    applyDeclaration(f, win);
    assert.equal(decodeOf(f), VIDEO_ALL);
    setPendingFallback(win, { resourceId: 'res1', itemId: 'item1', reason: 'decode_error', cls: 'hevc10' });
    applyDeclaration(f, win);
    assert.equal(decodeOf(f), null, 'a video class: nothing, as before');
    clearPendingFallback(win);
    applyDeclaration(f, win);
    assert.equal(decodeOf(f), `${VIDEO_ALL},aac51,ac3,ec3`);
    assert.equal(f.querySelector('input[name="decode-fallback"]'), null);
});

test('not taking part: no declaration, whatever the audio memory says', async () => {
    const win = await audioPage();
    rememberFallback(win, { resourceId: 'r', itemId: 'i', cls: 'dolby', strike: true });
    win.localStorage.setItem(OPTIN_KEY, 'off');
    win.__wtDecode.optin = undefined;
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), null);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'j' }), null);
});

// ---- the audio opt-in (`?audio=on|off`, wt-audio) ----------------------------

test('?audio=on opts this browser into the audio tokens, ?audio=off out; independent of ?passthrough=', () => {
    let win = page({ url: 'https://webtor.io/?audio=on' });
    assert.equal(takesPartAudio(win), false, 'not before the switch is read');
    assert.equal(applyAudioUrlSwitch(win), 'on');
    assert.equal(takesPartAudio(win), true);
    assert.equal(win.localStorage.getItem(AUDIO_OPTIN_KEY), 'on');
    assert.equal(win.localStorage.getItem(OPTIN_KEY), null, 'the video switch untouched');
    assert.equal(applyUrlSwitch(win), null, 'and ?audio= is not the video switch');
    // A later page of the same browser.
    const store = win.localStorage.getItem(AUDIO_OPTIN_KEY);
    win = page({ url: 'https://webtor.io/ru/other' });
    win.localStorage.setItem(AUDIO_OPTIN_KEY, store);
    assert.equal(applyAudioUrlSwitch(win), null);
    assert.equal(takesPartAudio(win), true);
    win = page({ url: 'https://webtor.io/?audio=off' });
    win.localStorage.setItem(AUDIO_OPTIN_KEY, 'on');
    assert.equal(applyAudioUrlSwitch(win), 'off');
    assert.equal(takesPartAudio(win), false);
    assert.equal(win.localStorage.getItem(AUDIO_OPTIN_KEY), 'off');
    // Absent, or a value that is not on/off: not opted in.
    for (const url of ['https://webtor.io/', 'https://webtor.io/?audio=yes', 'https://webtor.io/?audio=']) {
        win = page({ url });
        assert.equal(applyAudioUrlSwitch(win), null, url);
        assert.equal(takesPartAudio(win), false, url);
    }
    // ?passthrough=on leaves the audio out; both switches on one address set both.
    win = page({ url: 'https://webtor.io/?passthrough=on' });
    applyUrlSwitch(win);
    applyAudioUrlSwitch(win);
    assert.deepEqual([takesPart(win), takesPartAudio(win)], [true, false]);
    assert.equal(win.localStorage.getItem(AUDIO_OPTIN_KEY), null);
    win = page({ url: 'https://webtor.io/?passthrough=on&audio=on' });
    applyUrlSwitch(win);
    applyAudioUrlSwitch(win);
    assert.deepEqual([takesPart(win), takesPartAudio(win)], [true, true]);
});

test('the audio opt-in with a throwing localStorage: the page\'s own, and no exception', () => {
    let win = page({ storageThrows: true });
    assert.equal(takesPartAudio(win), false);
    win = page({ storageThrows: true, url: 'https://webtor.io/?audio=on' });
    assert.doesNotThrow(() => applyAudioUrlSwitch(win));
    assert.equal(takesPartAudio(win), true, 'held for the page');
    assert.doesNotThrow(() => initDecodeDeclaration(win, win.document));
});

test('initDecodeDeclaration reads ?audio= too', () => {
    const win = page({ url: 'https://webtor.io/ru/some?audio=on' });
    initDecodeDeclaration(win, win.document);
    assert.equal(takesPartAudio(win), true);
    assert.equal(win.localStorage.getItem(AUDIO_OPTIN_KEY), 'on');
});

// The re-review's condition for merging with stage 5: a browser that
// declares its video (by the opt-in now, by default after stage 5) sends no
// audio token, is asked nothing about audio and waits for none, unless it
// opted into audio itself.
test('without the audio opt-in a declaring browser sends no audio token, and is asked nothing about audio', async () => {
    const win = page();
    optIn(win);
    const asked = [];
    const probe = {
        load: async () => ({
            declarationSupport: (env, opts) => {
                asked.push(opts);
                return { path: 'mse', hevc: ALL, pq: Promise.resolve(true), audio: Promise.resolve(['aac51', 'ec3']) };
            },
            envFromWindow: () => ({}),
        }),
    };
    await startProbe(win, probe);
    await win.__wtDecode.audio;
    assert.deepEqual(asked, [{ audio: false }], 'the probe was told not to ask');
    assert.equal(win.__wtDecode.audio, null, 'no audio part on this page');
    assert.equal(win.__wtDecode.fresh.audio, null);
    assert.equal(win.localStorage.getItem(AUDIO_CACHE_KEY), null, 'nothing remembered for audio');
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), VIDEO_ALL);
    // An audio answer this browser gave while it was opted in stays unsent.
    win.localStorage.setItem(AUDIO_CACHE_KEY, JSON.stringify({ ua: UA_A, tokens: ['aac51', 'ac3', 'ec3'], at: Date.now() }));
    assert.equal(declarationFor(win, {}), VIDEO_ALL);
    // The start form says the same.
    installSubmitHook(win.document, win);
    const f = startForm(win);
    f.addEventListener('submit', (e) => e.preventDefault());
    f.requestSubmit();
    assert.equal(decodeOf(f), VIDEO_ALL);
    // Opting out of audio after opting in is the same.
    const out = page({ url: 'https://webtor.io/?audio=off' });
    optInBoth(out);
    applyAudioUrlSwitch(out);
    await startProbe(out, fakeProbe({ audio: ['aac51'] }));
    assert.equal(declarationFor(out, {}), VIDEO_ALL);
});

test('the audio opt-in alone declares nothing: the page must take part at all (opted out: nothing)', async () => {
    const win = page();
    optOut(win);
    optInAudio(win);
    assert.equal(takesPart(win), false);
    const asked = [];
    await startProbe(win, { load: async () => ({ declarationSupport: (env, opts) => { asked.push(opts); return { path: 'mse', hevc: ALL, pq: Promise.resolve(true), audio: Promise.resolve(['aac51']) }; }, envFromWindow: () => ({}) }) });
    assert.deepEqual(asked, [{ audio: false }], 'Discover\'s probe on a page that declares nothing asks no audio');
    assert.equal(declarationFor(win, {}), null);
});

// Stage 5 made the video declaration every browser's default; the audio
// stays opt-in. A browser that never touched either switch: the video
// part, no audio token, no audio question, no audio cache.
test('stage 5: a browser that never opened either switch declares its video and no audio', async () => {
    const win = page();
    assert.deepEqual([takesPart(win), takesPartAudio(win)], [true, false]);
    const asked = [];
    await startProbe(win, { load: async () => ({ declarationSupport: (env, opts) => { asked.push(opts); return { path: 'mse', hevc: ALL, pq: Promise.resolve(true), audio: Promise.resolve(['aac51', 'ac3', 'ec3']) }; }, envFromWindow: () => ({}) }) });
    assert.deepEqual(asked, [{ audio: false }]);
    assert.equal(declarationFor(win, { resourceId: 'r', itemId: 'i' }), VIDEO_ALL);
    assert.equal(win.localStorage.getItem(AUDIO_CACHE_KEY), null);
    // The same browser after ?audio=on.
    const next = page({ url: 'https://webtor.io/?audio=on' });
    initDecodeDeclaration(next, next.document);
    assert.deepEqual([takesPart(next), takesPartAudio(next)], [true, true]);
});

test('whenDeclared: a page that does not declare audio waits for the video part only', async () => {
    const win = page();
    optIn(win);
    const p = fakeProbe({ pq: 'later', audio: 'never' });
    startProbe(win, p);
    setTimeout(() => p.answerPQ(true), 60);
    const t0 = Date.now();
    await whenDeclared(win, 300);
    const waited = Date.now() - t0;
    assert.ok(waited >= 40 && waited < 200, `waited ${waited} ms, not the 300 ms an unasked audio part would cost`);
    assert.equal(declarationFor(win, {}), VIDEO_ALL);
    // Complete already: at once.
    const t1 = Date.now();
    await whenDeclared(win, 300);
    assert.ok(Date.now() - t1 < 50);
});

test('the real probe without the audio opt-in: no audio question at all', async () => {
    let calls = 0;
    const env = {
        userAgent: UA_A,
        MediaSource: Object.assign(function () {}, { isTypeSupported: (t) => { if (t.startsWith('audio/')) calls++; return t.startsWith('video/mp4;codecs=hvc1.') || t === 'video/mp4;codecs=avc1.42E01E,mp4a.40.2'; } }),
        mediaCapabilities: { decodingInfo: async (c) => { if (c.audio && !c.video) calls++; return { supported: true, smooth: true, powerEfficient: true }; } },
    };
    const win = page();
    optIn(win);
    await startProbe(win, { env });
    assert.equal(calls, 0, 'no isTypeSupported for audio, no decodingInfo for audio');
    assert.equal(declarationFor(win, {}), DECODE_VIDEO_TOKENS.join(','));
    // The same browser opted into audio is asked, and declares it.
    const both = page();
    optInBoth(both);
    await startProbe(both, { env });
    await both.__wtDecode.audio;
    assert.ok(calls > 0);
    assert.equal(declarationFor(both, {}), [...DECODE_VIDEO_TOKENS, 'aac51'].join(','));
});

// ---- how an iPhone plays HLS (`?mms=on|off`, wt-mms) ------------------------

test('an iPhone plays HLS with hls.js by default; ?mms=off natively, ?mms=on back; the other switches untouched', () => {
    let win = page({ url: 'https://webtor.io/?mms=off' });
    assert.equal(iosPlaysHlsJs(win), true, 'hls.js by default (2026-09-30)');
    assert.equal(applyMmsUrlSwitch(win), 'off');
    assert.equal(iosPlaysHlsJs(win), false);
    assert.equal(win.localStorage.getItem(MMS_OPTIN_KEY), 'off');
    assert.equal(win.localStorage.getItem(OPTIN_KEY), null);
    assert.equal(win.localStorage.getItem(AUDIO_OPTIN_KEY), null);
    assert.equal(applyUrlSwitch(win), null, '?mms= is neither the video switch');
    assert.equal(applyAudioUrlSwitch(win), null, 'nor the audio one');
    // A later page of the same browser.
    win = page({ url: 'https://webtor.io/ru/other' });
    win.localStorage.setItem(MMS_OPTIN_KEY, 'off');
    assert.equal(applyMmsUrlSwitch(win), null);
    assert.equal(iosPlaysHlsJs(win), false);
    win = page({ url: 'https://webtor.io/?mms=on' });
    win.localStorage.setItem(MMS_OPTIN_KEY, 'off');
    assert.equal(applyMmsUrlSwitch(win), 'on');
    assert.equal(iosPlaysHlsJs(win), true);
});

test('initDecodeDeclaration reads ?mms= too', () => {
    const win = page({ url: 'https://webtor.io/?mms=off' });
    initDecodeDeclaration(win, win.document);
    assert.equal(iosPlaysHlsJs(win), false);
});

test('the ?mms= switch is read once per page: another tab switching does not split this page', () => {
    const win = page({ url: 'https://webtor.io/ru/other' });
    win.localStorage.setItem(MMS_OPTIN_KEY, 'off');
    assert.equal(iosPlaysHlsJs(win), false);
    win.localStorage.setItem(MMS_OPTIN_KEY, 'on');
    assert.equal(iosPlaysHlsJs(win), false, 'the probe asked the native path on this page; createHls must take it');
});

test('a cached answer from the other HLS path is not sent', () => {
    let win = page({ url: 'https://webtor.io/ru/other' });
    optIn(win);
    win.localStorage.setItem(MMS_OPTIN_KEY, 'off');
    win.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: UA_A, mms: true, tokens: ['hevc10', 'hdr-pq'], at: Date.now() - 1000 }));
    assert.equal(declarationFor(win, {}), 'unknown', '?mms=off since: that answer was the MSE path\'s -- no answer yet');
    win = page({ url: 'https://webtor.io/ru/other' });
    optIn(win);
    win.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: UA_A, mms: true, tokens: ['hevc10', 'hdr-pq'], at: Date.now() - 1000 }));
    assert.equal(declarationFor(win, {}), 'hevc10,hdr-pq', 'the same path: kept');
    win = page({ url: 'https://webtor.io/ru/other' });
    optIn(win);
    win.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: UA_A, tokens: ['hevc10'], at: Date.now() - 1000 }));
    assert.equal(declarationFor(win, {}), 'hevc10', 'an entry from before `mms` says nothing of its path: taken');
    win = page({ url: 'https://webtor.io/ru/other' });
    optIn(win);
    win.localStorage.setItem(MMS_OPTIN_KEY, 'off');
    win.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: UA_A, mms: false, tokens: ['hevc10'], at: Date.now() - 1000 }));
    assert.equal(declarationFor(win, {}), 'hevc10', 'the native path\'s own entry, where the browser plays natively');
});
