// lib/progressLog.js on the action log's host (views/action/post.html, with
// a .job-stage): one line for the running step, the steps folded, cards
// and verdicts outside the fold. Other hosts keep the plain log.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
global.CustomEvent = dom.window.CustomEvent;
global.HTMLElement = dom.window.HTMLElement;

const { initProgressLog } = await import('./progressLog.js');

// The markup of views/action/post.html, without the SSE url: the test feeds
// the messages through the SDK.
function stageHost() {
    document.body.innerHTML = `
        <div class="hidden progress-alert">
          <div class="job-stage" hidden>
            <div class="job-stage-row"><span class="job-stage-spin"></span><span class="job-stage-title"></span><span class="job-stage-meta"></span></div>
            <progress class="job-stage-bar" max="100"></progress>
          </div>
          <div class="job-results"></div>
          <details class="job-steps"><summary>Show steps</summary><div class="log-target"></div></details>
          <p class="sr-only" role="status" data-job-announce></p>
        </div>`;
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

test('a plain host (home page, embed) keeps the plain log', () => {
    document.body.innerHTML = '<div class="progress-alert"><div class="log-target"></div></div>';
    const el = document.querySelector('.progress-alert');
    const sdk = initProgressLog(el);
    sdk.inProgress('a', 'checking magnet');
    sdk.done('a', 'success! redirecting');
    sdk.push({ level: 'custom', tag: 'c', body: '<div class="card">card</div>' });
    const lt = el.querySelector('.log-target');
    assert.ok(lt.querySelector('pre[task-tag=a]'));
    assert.ok(lt.querySelector('pre.done-summary'));
    assert.ok(lt.querySelector('.card'));
});
