// lib/progressLog.js on a host rendered with partials/job_log.html (a
// .job-stage): one line for the running step, the steps folded, notes, cards
// and verdicts outside the fold. The embed's host keeps the plain log.
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.CustomEvent = dom.window.CustomEvent;
global.HTMLElement = dom.window.HTMLElement;

const { initProgressLog } = await import('./progressLog.js');

// partials/job_log.html itself, its template actions dropped, in a host
// without the SSE url: the test feeds the messages through the SDK.
const partial = readFileSync(new URL('../../../../templates/partials/job_log.html', import.meta.url), 'utf8');
const jobLog = partial.replace(/\{\{[\s\S]*?\}\}/g, (a) => (a.includes('showSteps') ? 'Show steps' : ''));

function stageHost() {
    document.body.innerHTML = `<div class="hidden progress-alert">${jobLog}</div>`;
    const el = document.querySelector('.progress-alert');
    const sdk = initProgressLog(el);
    const q = (s) => el.querySelector(s);
    return { el, sdk, q };
}

test('the running step is one line in words, its steps folded below', () => {
    const { sdk, q } = stageHost();
    sdk.inProgress('data', 'Getting the torrent\'s details');
    assert.equal(q('.job-stage').hidden, false);
    assert.equal(q('.job-stage-title').textContent, 'Getting the torrent\'s details');
    assert.equal(q('.job-stage-bar').hasAttribute('value'), false, 'indeterminate: no number yet');
    assert.equal(q('[data-job-announce]').textContent, 'Getting the torrent\'s details', 'said once');
    assert.ok(q('.job-steps .log-target pre[task-tag=data]'), 'and the step is in the folded list');
});

test('a countdown is its meta with the bar still open; a percent fills the bar', () => {
    const { sdk, q } = stageHost();
    sdk.inProgress('warm', 'Loading the start of the file');
    sdk.updateStatus('warm', 'looking for peers · 34 s left');
    assert.equal(q('.job-stage-meta').textContent, 'looking for peers · 34 s left');
    assert.equal(q('.job-stage-bar').hasAttribute('value'), false);
    sdk.updateStatus('warm', '37%');
    assert.equal(q('.job-stage-meta').textContent, '37%');
    assert.equal(q('.job-stage-bar').value, 37);
    assert.equal(q('[data-job-announce]').textContent, 'Loading the start of the file', 'the ticks are not said');
    // The next step starts from nothing again.
    sdk.inProgress('probe', 'Checking the video format');
    assert.equal(q('.job-stage-meta').textContent, '');
    assert.equal(q('.job-stage-bar').hasAttribute('value'), false);
    // A late tick of the previous step does not move this one.
    sdk.updateStatus('warm', '99%');
    assert.equal(q('.job-stage-meta').textContent, '');
});

test('a card or a verdict is the answer: outside the fold, and the line goes', () => {
    const { sdk, q } = stageHost();
    sdk.inProgress('link', 'Getting the download link');
    sdk.done('link', 'The file is ready to download');
    assert.equal(q('.job-stage').hidden, true);
    assert.ok(q('.job-results pre.done-summary'), 'the verdict, outside the fold');
    assert.equal(q('[data-job-announce]').textContent, 'The file is ready to download');
    sdk.push({ level: 'custom', tag: 'card', body: '<div class="card">start download</div>' });
    assert.ok(q('.job-results .card'), 'the card, outside the fold');
    assert.equal(q('.job-steps .card'), null);
});

test('over with nothing rendered: the line stops; a rendered player keeps it going', () => {
    let h = stageHost();
    h.sdk.inProgress('x', 'Preparing the stream');
    h.sdk.close();
    assert.ok(h.q('.job-stage').classList.contains('job-stage-done'));
    h = stageHost();
    h.sdk.inProgress('p', 'Starting the player');
    h.sdk.push({ level: 'rendertemplate', tag: 'r', body: '<div></div>' });
    h.sdk.close();
    assert.equal(h.q('.job-stage').classList.contains('job-stage-done'), false, 'the player is still loading');
});

test('a note is read under the line, and the line stays', () => {
    // The home page's "we don't have this torrent yet" before the magnet
    // step; the payment page's "still waiting, you can close this page".
    // As the server sends it: services/job tags an info with the step before
    // it, so the folded list takes it for that step and drops it.
    const note = { level: 'info', tag: 'check', message: 'We don\'t have this torrent yet, so we\'re getting it from peers' };
    const { sdk, q } = stageHost();
    sdk.inProgress('check', 'Checking the link');
    sdk.done('check');
    sdk.push(note);
    assert.equal(q('.job-results pre.info-summary').textContent, note.message);
    assert.equal(q('.job-stage').hidden, false, 'a note is not the answer');
    assert.equal(q('.job-stage-title').textContent, 'Checking the link');
    sdk.inProgress('peers', 'Getting the torrent from peers');
    assert.equal(q('.job-stage-title').textContent, 'Getting the torrent from peers');
    // A replayed log (EventSource reconnects and gets it from the start)
    // brings the same note again: one is enough.
    sdk.push(note);
    assert.equal(q('.job-results').querySelectorAll('pre.info-summary').length, 1);
    sdk.clear();
    assert.equal(q('.job-results pre'), null, 'a new run starts clean');
});

test('the embed\'s plain host keeps the plain log', () => {
    document.body.innerHTML = '<div class="progress-alert"><div class="log-target"></div></div>';
    const el = document.querySelector('.progress-alert');
    const sdk = initProgressLog(el);
    sdk.inProgress('a', 'checking magnet');
    sdk.info('a note');
    sdk.done('a', 'success! redirecting');
    sdk.push({ level: 'custom', tag: 'c', body: '<div class="card">card</div>' });
    const lt = el.querySelector('.log-target');
    assert.ok(lt.querySelector('pre[task-tag=a]'));
    assert.ok(lt.querySelector('pre.done-summary'));
    assert.ok(lt.querySelector('.card'));
    assert.equal(lt.querySelectorAll('pre.info-summary').length, 0, 'its notes are lines of the log');
});

test('the Discover stream modal draws what partials/job_log.html renders', () => {
    // StreamModal.jsx cannot call the Go partial; a retry inside the modal
    // swaps in the server's version (load/progress), so the two must match.
    const jsx = readFileSync(new URL('./discover/components/StreamModal.jsx', import.meta.url), 'utf8');
    const host = jsx.slice(jsx.indexOf('<div class="job-stage"'), jsx.indexOf('data-job-announce') + 1);
    const classes = (src) => [...src.matchAll(/class="([^"]+)"/g)].map((m) => m[1]).join('|');
    assert.ok(host.length > 1, 'the modal has a stage');
    assert.equal(classes(host), classes(partial.slice(partial.indexOf('<div class="job-stage"'))));
});

test('a step whose words have an apostrophe (the tag is the words) still gets its line, its ticks and its card', () => {
    // services/job tags every message with the step's own text; built into a
    // selector, "torrent's" or "dell'inizio" made querySelector throw and the
    // message -- the no-peers card here -- was lost.
    const { sdk, q } = stageHost();
    const step = 'Caricamento dell\'inizio del file, 12 MB';
    sdk.inProgress(step, step);
    sdk.updateStatus(step, '37%');
    assert.equal(q('.job-stage-title').textContent, step);
    assert.equal(q('.job-stage-bar').value, 37);
    sdk.push({ level: 'error', tag: step });
    sdk.push({ level: 'custom', tag: step, body: '<div class="card"><h3>No active seeders</h3></div>' });
    assert.ok(q('.job-results .card'), 'the card is there');
    sdk.inProgress('Getting the torrent\'s details', 'Getting the torrent\'s details');
    assert.ok(q('.job-steps pre[task-tag]'));
});

test('a card is said too: its heading, once', () => {
    const { sdk, q } = stageHost();
    sdk.inProgress('w', 'Loading the start of the file, 10 MB');
    sdk.push({ level: 'custom', tag: 'w', body: '<div><h3>The torrent has no active seeders</h3><p>Save it to Vault…</p></div>' });
    assert.equal(q('[data-job-announce]').textContent, 'The torrent has no active seeders');
});

test('a card without a heading is not read out -- the download card starts with a script holding its link', () => {
    const { sdk, q } = stageHost();
    sdk.inProgress('l', 'Getting the download link');
    sdk.done('l', 'The file is ready to download');
    sdk.push({ level: 'custom', tag: 'l', body: '<script>var url = "https://x/f?api-key=k&token=t";</script><div>start download</div>' });
    assert.equal(q('[data-job-announce]').textContent, 'The file is ready to download', 'the verdict stands; no script, no key');
});
