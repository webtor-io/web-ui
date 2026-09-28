import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://x.test/ru/res' });
global.window = dom.window; global.document = dom.window.document;
global.FormData = dom.window.FormData; global.DOMParser = dom.window.DOMParser;
const { fetchStreamRender, progressText } = await import('./background-render.js');

function fakeEventSource(messages) {
    return class {
        constructor() {
            this.closed = false;
            setTimeout(() => {
                for (const m of messages) if (!this.closed && this.onmessage) this.onmessage({ data: JSON.stringify(m) });
            }, 0);
        }
        close() { this.closed = true; }
    };
}
const form = () => {
    document.body.innerHTML = `
        <div id="log-1" data-async-layout="L"></div>
        <form action="https://x.test/ru/stream-video" method="post" data-async-target="#log-1">
            <input type="hidden" name="resource-id" value="res"><input type="hidden" name="item-id" value="i2">
        </form>`;
    return document.querySelector('form');
};
const jobLog = '<template data-async-fragment="main"><div data-async-progress-log="/job/log/1"></div></template>';
const fetchImpl = async () => ({ ok: true, text: async () => jobLog });

test('the log of a background start is reported as it happens: the step, then its status under it', async () => {
    const seen = [];
    const doc = await fetchStreamRender(form(), {
        fetchImpl, onProgress: (t) => seen.push(t),
        EventSourceImpl: fakeEventSource([
            { level: 'open' },
            { level: 'inprogress', message: 'warming up torrent client, downloading 10 MB', tag: 'w' },
            { level: 'statusupdate', status: 'waiting for peers', tag: 'w' },
            { level: 'statusupdate', status: '37%', tag: 'w' },
            { level: 'done', tag: 'w' },
            { level: 'inprogress', message: 'probing media', tag: 'p' },
            { level: 'rendertemplate', body: '<video class="player"></video>' },
            { level: 'statusupdate', status: 'never seen: the stream is closed', tag: 'x' },
        ]),
    });
    assert.ok(doc.querySelector('.player'));
    assert.deepEqual(seen, [
        'warming up torrent client, downloading 10 MB',
        'warming up torrent client, downloading 10 MB — waiting for peers',
        'warming up torrent client, downloading 10 MB — 37%',
        'probing media',
    ]);
});

test('a silent caller passes no listener and nothing changes for it', async () => {
    const doc = await fetchStreamRender(form(), {
        fetchImpl,
        EventSourceImpl: fakeEventSource([{ level: 'inprogress', message: 'x' }, { level: 'rendertemplate', body: '<video class="player"></video>' }]),
    });
    assert.ok(doc.querySelector('.player'));
});

test('progressText', () => {
    assert.equal(progressText('step', ''), 'step');
    assert.equal(progressText('', '37%'), '37%');
    assert.equal(progressText('', ''), '');
});

// ---- the declaration (decode-declaration.js) --------------------------------

const { declare } = await import('./background-render.js');
const { OPTIN_KEY, CACHE_KEY, rememberFallback } = await import('./decode-declaration.js');

// A start made off the page -- the next episode's (a clone of this file's
// form), a settings restart -- gets the declaration a visible start of THAT
// file would get, whatever the form carried; and never the fields of a
// passthrough fallback, which belong to one visible restart.
test('a background start declares for its own file, and drops a fallback\'s fields', async () => {
    window.localStorage.setItem(OPTIN_KEY, 'on');
    window.localStorage.setItem(CACHE_KEY, JSON.stringify({ ua: window.navigator.userAgent, tokens: ['hevc8', 'hevc10'], at: Date.now() }));
    const f = form();
    for (const [name, value] of [['decode', 'hevc10-2160'], ['decode-fallback', 'decode_error'], ['decode-class', 'hevc10']]) {
        const i = document.createElement('input');
        i.type = 'hidden'; i.name = name; i.value = value;
        f.appendChild(i);
    }
    let body = new FormData(f);
    declare(body, f);
    assert.equal(body.get('decode'), 'hevc8,hevc10', 'the declaration of now, not the one the clone carried');
    assert.equal(body.get('decode-fallback'), null);
    assert.equal(body.get('decode-class'), null);

    // The next file failed passthrough here: no declaration for it.
    rememberFallback(window, { resourceId: 'res', itemId: 'i2' });
    body = new FormData(f);
    declare(body, f);
    assert.equal(body.get('decode'), null);

    // The whole path: what fetchStreamRender posts.
    let posted = null;
    await fetchStreamRender(f, {
        fetchImpl: async (u, opts) => { posted = opts.body; return { ok: true, text: async () => jobLog }; },
        EventSourceImpl: fakeEventSource([{ level: 'rendertemplate', body: '<video class="player"></video>' }]),
    });
    assert.ok(posted, 'posted');
    assert.equal(posted.get('decode'), null);
    assert.equal(posted.get('decode-fallback'), null);
    window.localStorage.clear();
    delete window.__wtDecode;
});

test('an audio start is never declared', () => {
    window.localStorage.setItem(OPTIN_KEY, 'on');
    const f = form();
    f.setAttribute('action', 'https://x.test/ru/stream-audio');
    const body = new FormData(f);
    body.set('decode', 'hevc8');
    declare(body, f);
    assert.equal(body.get('decode'), null);
    window.localStorage.clear();
    delete window.__wtDecode;
});
