import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;

const { BUSY_MAX_MS, clearBusy, clearBusyFor, isActionForm, isBusy, markBusy } = await import('./actionBusy.js');

function card({ action = '/ru/stream-video', label = 'Смотреть' } = {}) {
    document.body.innerHTML = `
        <div class="card">
            <form class="stream" action="${action}" method="post" data-async-target="#log-1" data-busy-text="готовим…">
                <input type="hidden" name="resource-id" value="res">
                <button type="submit" class="btn btn-soft"><svg></svg>${label}</button>
            </form>
            <form class="download" action="/ru/download-file" method="post" data-async-target="#log-1" data-busy-text="готовим…">
                <button type="submit">Скачать</button>
            </form>
            <div id="log-1"><div class="progress-alert"><div class="log-target"></div></div></div>
        </div>`;
    return {
        stream: document.querySelector('form.stream'),
        download: document.querySelector('form.download'),
        btn: document.querySelector('form.stream button'),
        logTarget: document.querySelector('#log-1 .log-target'),
    };
}

test('only the five job-start endpoints are treated as action forms', () => {
    const p = card();
    assert.equal(isActionForm(p.stream), true);
    assert.equal(isActionForm(p.download), true);
    // The watched toggle and friends answer instantly: a busy state would
    // only flash.
    card({ action: '/ru/user-video-status/mark' });
    assert.equal(isActionForm(document.querySelector('form.stream')), false);
    assert.equal(isActionForm(null), false);
});

test('a busy button says what it is doing and cannot be pressed again', () => {
    const p = card();
    assert.equal(markBusy(p.stream), true);
    assert.equal(isBusy(p.stream), true);
    assert.equal(p.btn.disabled, true);
    assert.equal(p.btn.getAttribute('aria-busy'), 'true');
    assert.match(p.btn.textContent, /готовим…/);
    assert.ok(p.btn.querySelector('.loading-spinner'), 'with a spinner');
    // A second press changes nothing -- and does not lose the original label.
    assert.equal(markBusy(p.stream), false);
    assert.match(p.btn.dataset.busyLabel, /Смотреть/);
    // The other button of the same card is untouched: changing your mind is
    // not a double press.
    assert.equal(isBusy(p.download), false);
    assert.equal(p.download.querySelector('button').disabled, false);

    clearBusy(p.stream);
    assert.equal(isBusy(p.stream), false);
    assert.equal(p.btn.disabled, false);
    assert.match(p.btn.textContent, /Смотреть/);
    assert.ok(p.btn.querySelector('svg'), 'the icon comes back with the label');
    assert.equal(p.btn.querySelector('.loading-spinner'), null);
    assert.equal(p.btn.hasAttribute('aria-busy'), false);
});

test("the job log releases the button that started it, from anywhere inside the target", () => {
    const p = card();
    markBusy(p.stream);
    assert.equal(clearBusyFor(p.logTarget), true, 'walks up to #log-1');
    assert.equal(isBusy(p.stream), false);
    // Nothing to release, and nothing thrown.
    assert.equal(clearBusyFor(p.logTarget), false);
    assert.equal(clearBusyFor(document.body), false);
});

test('the safety timeout ends a busy state nobody else ended', async () => {
    const p = card();
    markBusy(p.stream, { timeoutMs: 20 });
    assert.equal(isBusy(p.stream), true);
    await new Promise((r) => setTimeout(r, 50));
    assert.equal(isBusy(p.stream), false, 'a job that died quietly must not leave a dead button');
    assert.equal(p.btn.disabled, false);
    // Long by default: a stream job may legitimately run for minutes.
    assert.ok(BUSY_MAX_MS >= 10 * 60 * 1000);
});

test('clearing a form that was never busy is a no-op', () => {
    const p = card();
    assert.equal(clearBusy(p.stream), false);
    assert.equal(p.btn.disabled, false);
});
