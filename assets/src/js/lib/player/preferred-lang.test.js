import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://x.test/' });
global.window = dom.window;
global.document = dom.window.document;

global.DOMParser = dom.window.DOMParser;
global.FormData = dom.window.FormData;
const { AUTO_RESUME_KEY, fetchFreshDialog, findStreamForm, markAutoResume, takeAutoResume, wirePreferredLang } = await import('./preferred-lang.js');

const memory = () => {
    const m = new Map();
    return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) };
};

function mount(current = 'en') {
    document.body.innerHTML = `
        <div id="page">
            <select id="preferred-lang" data-current="${current}">
                <option value="en"${current === 'en' ? ' selected' : ''}>English</option>
                <option value="kk"${current === 'kk' ? ' selected' : ''}>Kazakh</option>
            </select>
            <span id="preferred-lang-busy" hidden></span>
        </div>`;
    return document.getElementById('page');
}
const pick = (page, value) => {
    const select = page.querySelector('#preferred-lang');
    select.value = value;
    select.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    return select;
};
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

test('a change is stored first, then the position is saved and the stream asked for again', async () => {
    const page = mount('en');
    const order = [];
    const calls = [];
    wirePreferredLang(page, {
        fetchImpl: async (url, params) => { calls.push({ url, params }); order.push('put'); return { ok: true, status: 204 }; },
        beforeRestart: () => order.push('save'),
        restart: () => order.push('restart'),
    });
    const select = pick(page, 'kk');
    assert.equal(select.disabled, true, 'no second change while the first is in flight');
    assert.equal(page.querySelector('#preferred-lang-busy').hidden, false);
    await flush();
    assert.deepEqual(order, ['put', 'save', 'restart']);
    assert.equal(calls[0].url, '/stream-video/preferred-lang');
    assert.equal(calls[0].params.method, 'PUT');
    assert.deepEqual(JSON.parse(calls[0].params.body), { lang: 'kk' });
});

test('a change that was not stored changes nothing on screen', async () => {
    const page = mount('en');
    let restarted = false;
    wirePreferredLang(page, {
        fetchImpl: async () => ({ ok: false, status: 500 }),
        restart: () => { restarted = true; },
    });
    const select = pick(page, 'kk');
    await flush();
    assert.equal(restarted, false);
    assert.equal(select.value, 'en', 'the select goes back to what is in force');
    assert.equal(select.disabled, false);
    assert.equal(page.querySelector('#preferred-lang-busy').hidden, true);
});

test('picking the language already in force asks for nothing', async () => {
    const page = mount('kk');
    let puts = 0;
    wirePreferredLang(page, { fetchImpl: async () => { puts++; return { ok: true, status: 204 }; }, restart: () => {} });
    pick(page, 'kk');
    await flush();
    assert.equal(puts, 0);
});

test('the auto-resume note is one-shot, for one file, and short-lived', () => {
    const s = memory();
    markAutoResume('res', '/a.mkv', s);
    assert.equal(takeAutoResume('res', '/b.mkv', s), false, 'another file');
    assert.equal(s.getItem(AUTO_RESUME_KEY), null, 'and the note is spent either way');

    markAutoResume('res', '/a.mkv', s);
    assert.equal(takeAutoResume('res', '/a.mkv', s), true);
    assert.equal(takeAutoResume('res', '/a.mkv', s), false, 'one shot');

    markAutoResume('res', '/a.mkv', s);
    const note = JSON.parse(s.getItem(AUTO_RESUME_KEY));
    note.until = Date.now() - 1;
    s.setItem(AUTO_RESUME_KEY, JSON.stringify(note));
    assert.equal(takeAutoResume('res', '/a.mkv', s), false, 'a stale note must not skip the prompt on a later visit');
});

test('the stream form is found by what it posts to, not by its class', () => {
    // The markup of partials/button.html on a resource page: the class is
    // the action name, and a language prefix leads the endpoint.
    document.body.innerHTML = `
        <form class="download" action="/ru/download-file" method="post"></form>
        <form class="stream" action="/ru/stream-video" method="post" data-async-target="#log-1"></form>`;
    const form = findStreamForm();
    assert.ok(form, 'found');
    assert.equal(form.getAttribute('action'), '/ru/stream-video');
    document.body.innerHTML = '<form class="download" action="/download-file"></form>';
    assert.equal(findStreamForm(), null);
});

// A scripted EventSource: the job log of a stream action.
function fakeEventSource(messages) {
    return class {
        constructor(url) {
            this.url = url;
            this.closed = false;
            setTimeout(() => {
                for (const m of messages) if (!this.closed && this.onmessage) this.onmessage({ data: JSON.stringify(m) });
            }, 0);
        }
        close() { this.closed = true; }
    };
}
const streamForm = () => {
    document.body.innerHTML = `
        <div id="log-1" data-async-layout="L"></div>
        <form class="stream" action="https://x.test/ru/stream-video" method="post" data-async-target="#log-1">
            <input type="hidden" name="resource-id" value="res"><input type="hidden" name="item-id" value="item">
        </form>`;
    return document.querySelector('form');
};
const jobLog = '<template data-async-fragment="main"><div data-async-progress-log="/job/log/1"></div></template>';

test('the fresh dialog comes from the job\u2019s last message, off the page', async () => {
    const form = streamForm();
    const calls = [];
    const fresh = await fetchFreshDialog(form, {
        fetchImpl: async (url, params) => { calls.push({ url, params }); return { ok: true, text: async () => jobLog }; },
        EventSourceImpl: fakeEventSource([
            { level: 'inprogress', message: 'exporting' },
            { level: 'rendertemplate', body: '<div><video class="player"></video><dialog id="subtitles" data-preferred-lang="kk"><div class="modal-box">new</div></dialog></div>' },
        ]),
    });
    assert.ok(fresh, 'a dialog');
    assert.equal(fresh.getAttribute('data-preferred-lang'), 'kk');
    assert.equal(calls[0].params.method, 'POST');
    assert.equal(calls[0].params.headers['X-Layout'], 'L', 'asked the way the async library asks');
    assert.equal(document.querySelector('#subtitles'), null, 'and nothing was put on the page');
});

test('an answer that is not a rendered player is left to the visible path', async () => {
    const form = streamForm();
    const fetchImpl = async () => ({ ok: true, text: async () => jobLog });
    assert.equal(await fetchFreshDialog(form, { fetchImpl, EventSourceImpl: fakeEventSource([{ level: 'custom', body: 'cap modal' }]) }), null);
    assert.equal(await fetchFreshDialog(form, { fetchImpl, EventSourceImpl: fakeEventSource([]), timeoutMs: 30 }), null, 'a silent job');
    assert.equal(await fetchFreshDialog(form, { fetchImpl: async () => ({ ok: false, status: 403 }), EventSourceImpl: fakeEventSource([]) }), null, 'a refused start');
});

test('the quiet swap is tried first; the restart is what is left when it cannot be done', async () => {
    const page = mount('en');
    const order = [];
    const opts = (swapResult, background) => ({
        fetchImpl: async (url) => (String(url).endsWith('/preferred-lang')
            ? { ok: true, status: 204 }
            : { ok: true, text: async () => jobLog }),
        EventSourceImpl: fakeEventSource([{ level: 'rendertemplate', body: '<dialog id="subtitles"><div class="modal-box"></div></dialog>' }]),
        getToken: async () => (background ? '' : null),
        swap: () => { order.push('swap'); return swapResult; },
        beforeRestart: () => order.push('save'),
        restart: () => order.push('restart'),
    });
    // findStreamForm looks at the document.
    page.insertAdjacentHTML('beforeend', '<div id="log-1"></div><form action="https://x.test/stream-video" data-async-target="#log-1"></form>');
    wirePreferredLang(page, opts(true, true));
    pick(page, 'kk');
    await new Promise((r) => setTimeout(r, 20));
    assert.deepEqual(order, ['swap'], 'swapped: no save, no restart, the player is never touched');
});

test('a viewer Turnstile wants a click from gets the visible restart', async () => {
    const page = mount('en');
    const order = [];
    wirePreferredLang(page, {
        fetchImpl: async () => ({ ok: true, status: 204 }),
        getToken: async () => null,
        swap: () => { order.push('swap'); return true; },
        beforeRestart: () => order.push('save'),
        restart: () => order.push('restart'),
    });
    pick(page, 'kk');
    await new Promise((r) => setTimeout(r, 20));
    assert.deepEqual(order, ['save', 'restart']);
});

test('an anonymous start made off the page carries its own fresh token', async () => {
    const form = streamForm();
    let sent = null;
    await fetchFreshDialog(form, {
        token: 'tok-1',
        fetchImpl: async (url, params) => { sent = params.body.get('cf-turnstile-response'); return { ok: false }; },
        EventSourceImpl: fakeEventSource([]),
    });
    assert.equal(sent, 'tok-1');
});
