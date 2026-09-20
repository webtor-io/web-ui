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
            <form class="stream" action="${action}" method="post" data-async-target="#log-1" >
                <input type="hidden" name="resource-id" value="res">
                <button type="submit" class="btn btn-soft"><svg class="icon"></svg>${label}</button>
            </form>
            <form class="download" action="/ru/download-file" method="post" data-async-target="#log-1" >
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

test('a busy button keeps its label, gains a spinner, and cannot be pressed again', () => {
    const p = card();
    assert.equal(markBusy(p.stream, { timeoutMs: 50 }), true);
    assert.equal(isBusy(p.stream), true);
    // Dimmed, not disabled: the button is working, not refusing.
    assert.equal(p.btn.classList.contains('btn-busy'), true);
    assert.equal(p.btn.disabled, false, 'never the disabled attribute');
    assert.equal(p.btn.getAttribute('aria-busy'), 'true');
    assert.equal(p.btn.getAttribute('aria-disabled'), 'true');
    // The label stays what it was -- it is what says which of the two
    // buttons was pressed -- and a spinner joins it, first.
    assert.match(p.btn.textContent, /Смотреть/);
    // The spinner takes the icon's place rather than standing next to it.
    assert.ok(p.btn.querySelector('.loading-spinner'), 'with a spinner');
    assert.equal(p.btn.querySelector('svg.icon'), null, 'the icon steps aside');
    assert.equal(p.btn.firstElementChild.className.includes('loading-spinner'), true, 'in its place');
    // A second press changes nothing -- and does not lose the original label.
    assert.equal(markBusy(p.stream, { timeoutMs: 50 }), false);
    assert.match(p.btn.dataset.busyLabel, /Смотреть/);
    // The other button of the same card is untouched: changing your mind is
    // not a double press.
    assert.equal(isBusy(p.download), false);
    assert.equal(p.download.querySelector('button').classList.contains('btn-busy'), false);

    clearBusy(p.stream);
    assert.equal(isBusy(p.stream), false);
    assert.equal(p.btn.classList.contains('btn-busy'), false);
    assert.match(p.btn.textContent, /Смотреть/);
    assert.ok(p.btn.querySelector('svg.icon'), 'the icon comes back with the label');
    assert.equal(p.btn.querySelector('.loading-spinner'), null);
    assert.equal(p.btn.hasAttribute('aria-busy'), false);
    assert.equal(p.btn.hasAttribute('aria-disabled'), false);
});

test("the job log releases the button that started it, from anywhere inside the target", () => {
    const p = card();
    markBusy(p.stream, { timeoutMs: 50 });
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
    assert.equal(p.btn.classList.contains('btn-busy'), false);
    // Long by default: a stream job may legitimately run for minutes.
    assert.ok(BUSY_MAX_MS >= 10 * 60 * 1000);
});

test('clearing a form that was never busy is a no-op', () => {
    const p = card();
    assert.equal(clearBusy(p.stream), false);
    assert.equal(p.btn.classList.contains('btn-busy'), false);
});

test('a job log in another bundle releases the button that started it', async () => {
    // The real shape of the 2026-09-20 bug: async.js marks the form busy from
    // the page entry, progressLog.js clears it from a lazy chunk, and webpack
    // hands each of them its own copy of this module. A second import under a
    // different specifier is the same split here -- two instances, no shared
    // module state between them. The DOM is what they have in common, so the
    // register lives there.
    const other = await import('./actionBusy.js?bundle=two');
    const p = card();
    assert.equal(markBusy(p.stream, { timeoutMs: 50 }), true);
    assert.equal(other.clearBusyFor(p.logTarget), true, 'the other copy finds it');
    assert.equal(isBusy(p.stream), false, 'and this copy agrees it is done');
    assert.equal(p.btn.querySelector('.loading'), null, 'spinner gone');
    assert.equal(p.btn.querySelector('svg.icon') !== null, true, 'icon back');
});

test('a second job into the same log container releases the first button', () => {
    // Watch is working; the viewer presses Download, whose response replaces
    // the log in #log-1. Nothing will ever report the end of the first job
    // to this page again, so its button must not be left spinning.
    const p = card();
    assert.equal(markBusy(p.stream, { timeoutMs: 50 }), true);
    assert.equal(markBusy(p.download, { timeoutMs: 50 }), true);
    assert.equal(isBusy(p.stream), false, 'the orphaned button is released');
    assert.equal(isBusy(p.download), true);
    // And the job log now releases the one that is actually running.
    assert.equal(clearBusyFor(p.logTarget), true);
    assert.equal(isBusy(p.download), false);
});
