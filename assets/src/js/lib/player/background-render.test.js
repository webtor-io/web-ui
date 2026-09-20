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
