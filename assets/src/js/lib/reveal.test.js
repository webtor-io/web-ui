// lib/stickyStatus.js reveal: what a job start produces is brought into view
// -- as little as possible, and never against the viewer.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM(`<!doctype html><body>
    <div id="torrent-status-sticky" hidden></div>
    <div id="log"></div>
    <dialog open><div id="in-dialog"></div></dialog>
</body>`, { url: 'https://webtor.io/' });
const w = dom.window;
global.window = w;
global.document = w.document;

const { reveal, notePress, NAVBAR_H } = await import('./stickyStatus.js');

// jsdom has no layout: the box, the window and the scroll are given.
w.innerHeight = 844;
let reduced = false;
w.matchMedia = (q) => ({ matches: reduced && q.includes('reduce') });
const scrolls = [];
w.scrollTo = (opts) => scrolls.push(opts);
function box(el, top, height) {
    el.getBoundingClientRect = () => ({ top, bottom: top + height, height });
}
const log = w.document.getElementById('log');
function fresh() {
    scrolls.length = 0;
    reduced = false;
    notePress(w);
}

test('already wholly in view: nothing moves', () => {
    fresh();
    box(log, 700, 94);
    assert.equal(reveal(log), false);
    assert.deepEqual(scrolls, []);
});

test('below the fold: its bottom is brought to the fold, smoothly', () => {
    fresh();
    box(log, 775, 94); // the measured log on a 390x844 phone
    assert.equal(reveal(log), true);
    assert.deepEqual(scrolls, [{ top: 775 + 94 - (844 - 12), behavior: 'smooth' }]);
});

test('taller than the space: its top, not its bottom, under the navbar', () => {
    fresh();
    box(log, 692, 900); // taller than 844 - navbar
    reveal(log);
    assert.equal(scrolls[0].top, 692 - (NAVBAR_H + 12));
});

test('the status bar that will be up counts as covered space', () => {
    fresh();
    const bar = w.document.getElementById('torrent-status-sticky');
    bar.setAttribute('data-moving', '');
    Object.defineProperty(bar, 'offsetHeight', { value: 58, configurable: true });
    box(log, 692, 900);
    reveal(log);
    assert.equal(scrolls[0].top, 692 - (NAVBAR_H + 58 + 12));
    bar.removeAttribute('data-moving');
});

test('inside a dialog: its own box scrolls, the page does not', () => {
    fresh();
    const el = w.document.getElementById('in-dialog');
    box(el, 900, 100);
    assert.equal(reveal(el), false);
    assert.deepEqual(scrolls, []);
});

test('the viewer scrolled after the press: left where they went, until the next press', () => {
    fresh();
    box(log, 900, 94);
    w.dispatchEvent(new w.WheelEvent('wheel', { deltaY: 100 }));
    assert.equal(reveal(log), false, 'a wheel');
    notePress(w);
    w.dispatchEvent(new w.KeyboardEvent('keydown', { key: 'PageDown' }));
    assert.equal(reveal(log), false, 'a scroll key');
    notePress(w);
    assert.equal(reveal(log), true, 'a new press brings it back');
});

test('reduced motion: no smooth scroll', () => {
    fresh();
    reduced = true;
    box(log, 900, 94);
    reveal(log);
    assert.equal(scrolls[0].behavior, 'auto');
});
