import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;

const { initStreamToggle, livePlayer } = await import('./streamToggle.js');
initStreamToggle(document);
// A second copy of the module (splitChunks is off: two bundles importing it
// each get their own, web-ui/CLAUDE.md) must not add a second listener: one
// press would pause and play again.
const second = await import('./streamToggle.js?bundle=two');
second.initStreamToggle(document);

// card renders a file card the way partials/file.html does: the stream
// form, its log, and in the log a player in the given state (or none).
function card({ player = 'playing', busy = false } = {}) {
    document.body.innerHTML = `
        <div class="group/card">
            <form class="stream" action="/ru/stream-video" method="post" data-async-target="#log-1"${busy ? ' data-busy="1"' : ''}>
                <button type="submit" class="btn btn-soft">Смотреть</button>
            </form>
            <div id="log-1">${player ? '<video class="player"></video>' : ''}</div>
        </div>`;
    const form = document.querySelector('form.stream');
    const video = document.querySelector('video.player');
    const calls = [];
    let submits = 0;
    form.addEventListener('submit', (e) => { e.preventDefault(); submits++; });
    if (video) {
        let paused = player !== 'playing';
        Object.defineProperty(video, 'paused', { get: () => paused });
        Object.defineProperty(video, 'ended', { get: () => false });
        Object.defineProperty(video, 'currentSrc', { get: () => (player === 'nosrc' ? '' : 'blob:https://webtor.io/x') });
        Object.defineProperty(video, 'error', { get: () => (player === 'failed' ? { code: 3 } : null) });
        video.play = () => { calls.push('play'); paused = false; return Promise.resolve(); };
        video.pause = () => { calls.push('pause'); paused = true; };
    }
    return { form, video, btn: form.querySelector('button'), calls, submits: () => submits };
}

test('a press while the player plays pauses it and starts nothing', () => {
    const c = card({ player: 'playing' });
    c.btn.click();
    assert.deepEqual(c.calls, ['pause']);
    assert.equal(c.submits(), 0);
});

test('a press while it is paused plays it', () => {
    const c = card({ player: 'paused' });
    c.btn.click();
    assert.deepEqual(c.calls, ['play']);
    assert.equal(c.submits(), 0);
});

test('two presses: pause, then play -- one listener only', () => {
    const c = card({ player: 'playing' });
    c.btn.click();
    c.btn.click();
    assert.deepEqual(c.calls, ['pause', 'play']);
});

test('no player, a player with no source or a failed one: a new stream', () => {
    for (const player of [null, 'nosrc', 'failed']) {
        const c = card({ player });
        c.btn.click();
        assert.equal(c.submits(), 1, `player=${player}`);
        assert.deepEqual(c.calls, [], `player=${player}`);
    }
});

test('a busy form is actionBusy\'s to answer', () => {
    const c = card({ player: 'playing', busy: true });
    c.btn.click();
    assert.deepEqual(c.calls, []);
    assert.equal(c.submits(), 1);
});

test('code restarting the stream (requestSubmit) is not a press', () => {
    const c = card({ player: 'playing' });
    c.form.requestSubmit();
    assert.equal(c.submits(), 1);
    assert.deepEqual(c.calls, []);
});

test('the player carries data-playing between play and pause/ended/emptied/error', () => {
    const c = card({ player: 'paused' });
    for (const [type, playing] of [['play', true], ['pause', false], ['play', true], ['ended', false], ['play', true], ['emptied', false], ['play', true], ['error', false]]) {
        c.video.dispatchEvent(new window.Event(type));
        assert.equal(c.video.hasAttribute('data-playing'), playing, type);
    }
});

test('livePlayer reads the log the form reports into, not another card\'s', () => {
    const c = card({ player: 'playing' });
    assert.equal(livePlayer(c.form), c.video);
    c.form.setAttribute('data-async-target', '#log-2');
    assert.equal(livePlayer(c.form), null);
});
