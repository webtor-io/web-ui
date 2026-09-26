import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createGraceHold } from './grace-hold.js';

// A media element as the hold touches it: paused, play(), pause(), the
// `play` event (dispatched synchronously here -- the hold does not care
// when it comes), a dataset.
function element({ paused = false } = {}) {
    const listeners = new Set();
    const el = {
        paused,
        dataset: {},
        plays: 0,
        pauses: 0,
        addEventListener: (name, fn) => { if (name === 'play') listeners.add(fn); },
        removeEventListener: (name, fn) => { if (name === 'play') listeners.delete(fn); },
        play() {
            el.plays++;
            el.paused = false;
            for (const fn of [...listeners]) fn();
            return Promise.resolve();
        },
        pause() {
            el.pauses++;
            el.paused = true;
        },
        listeners,
    };
    return el;
}

test('a playing film is paused by the popup and resumed by the answer', () => {
    const v = element();
    const hold = createGraceHold(v);
    hold.start();
    assert.equal(v.paused, true);
    assert.equal(hold.held(), true);
    assert.ok('graceCtaHold' in v.dataset, 'the element says the pause is the page’s');
    assert.equal(hold.release(), true);
    assert.equal(v.paused, false);
    assert.equal('graceCtaHold' in v.dataset, false);
    assert.equal(v.listeners.size, 0, 'the guard is off');
});

test('a film the viewer had paused stays paused -- unless the answer is Play', () => {
    const v = element({ paused: true });
    const hold = createGraceHold(v);
    hold.start();
    assert.equal(hold.held(), false);
    assert.equal('graceCtaHold' in v.dataset, false, 'their pause, not the page’s');
    assert.equal(hold.release(), false);
    assert.equal(v.plays, 0);
    const w = element({ paused: true });
    const again = createGraceHold(w);
    again.start();
    assert.equal(again.release({ play: true }), true, 'Play is an answer that plays');
    assert.equal(w.paused, false);
});

test('whatever starts the film behind the popup is put back, and counted as wanted', () => {
    const v = element({ paused: true });
    const hold = createGraceHold(v);
    hold.start();
    v.play(); // the subtitle catch-up, the embed's player_play...
    assert.equal(v.paused, true);
    assert.equal(hold.held(), true);
    hold.release();
    assert.equal(v.paused, false, 'the answer starts it');
    v.pause();
    v.play();
    assert.equal(v.paused, false, 'released: nobody holds it any more');
});

test('a session seek asks before it plays: held, and the answer starts it', () => {
    const v = element({ paused: true });
    const hold = createGraceHold(v);
    assert.equal(hold.holds(), false, 'no popup: the seek plays');
    hold.start();
    assert.equal(hold.holds(), true);
    assert.equal(v.plays, 0);
    assert.ok('graceCtaHold' in v.dataset);
    hold.release();
    assert.equal(v.plays, 1);
});

test('disposed (the next file, a teardown): nothing resumes', () => {
    const v = element();
    const hold = createGraceHold(v);
    hold.start();
    hold.dispose();
    assert.equal(hold.release(), false);
    assert.equal(v.paused, true);
    assert.equal(v.plays, 0);
    assert.equal('graceCtaHold' in v.dataset, false);
    assert.equal(v.listeners.size, 0);
});
