// lib/landscapeFocus.js: a phone on its side, the film takes the window
// (owner, 2026-10-15). jsdom has no layout and no media queries: the
// orientation, the observer's readings and the stage's box are given.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { cssRules } from '../test/css.mjs';

const dom = new JSDOM(`<!doctype html><body>
    <div id="torrent-status" data-resource-id="r1"></div>
    <div id="torrent-status-sticky" hidden class="-translate-y-full"></div>
    <div id="log"><div class="wt-player-stage"><div class="wt-player-mount"><div class="wt-player wt-player--video"></div></div></div></div>
</body>`, { url: 'https://webtor.io/' });
const w = dom.window;
global.window = w;
global.document = w.document;

// The one media query that matters, switchable; the rest never match.
let landscape = false;
const mqListeners = new Set();
w.matchMedia = (q) => ({
    get matches() { return q.includes('orientation: landscape') ? landscape : false; },
    addEventListener: (_, f) => { if (q.includes('orientation')) mqListeners.add(f); },
    removeEventListener: (_, f) => mqListeners.delete(f),
});
const turn = (to) => { landscape = to; for (const f of mqListeners) f(); };

// Every observer made, by what it watches.
const observers = [];
w.IntersectionObserver = class {
    constructor(cb) { this.cb = cb; this.targets = new Set(); observers.push(this); }
    observe(t) { this.targets.add(t); }
    unobserve(t) { this.targets.delete(t); }
    disconnect() { this.targets.clear(); }
};
const stage = () => w.document.querySelector('.wt-player-stage');
const focusIO = () => observers.find((o) => o.targets.has(stage()));
const seen = (ratio) => focusIO().cb([{ target: stage(), isIntersecting: ratio > 0, intersectionRatio: ratio }]);

// stickyStatus.js reaches for these as globals, landscapeFocus.js on the window.
global.IntersectionObserver = w.IntersectionObserver;
const frames = [];
w.requestAnimationFrame = global.requestAnimationFrame = (f) => frames.push(f);
w.innerHeight = 390;
const flush = () => { while (frames.length) frames.shift()(); };
const scrolls = [];
w.scrollTo = (o) => scrolls.push(o);
const events = [];
w.umami = { track: (name, props) => events.push([name, props]) };
let top = 0;
stage().getBoundingClientRect = () => ({ top, bottom: top + 390, height: 390 });

const { initLandscapeFocus } = await import('./landscapeFocus.js');
const { initStickyStatus, reveal, notePress, FOCUS_MEDIA, NAVBAR_H } = await import('./stickyStatus.js');
const html = w.document.documentElement;
const on = () => html.classList.contains('wt-focus');

let stop = null;
function fresh({ turned = true } = {}) {
    if (stop) stop();
    landscape = turned;
    top = 0;
    scrolls.length = 0;
    events.length = 0;
    frames.length = 0;
    stop = initLandscapeFocus(w.document);
}

test('in at 90% of the stage on screen, out under 70%: no flicker on one edge', () => {
    fresh();
    seen(0.85);
    assert.equal(on(), false, 'not yet');
    seen(0.95);
    assert.equal(on(), true);
    assert.deepEqual(events.at(-1), ['player-focus', { via: 'scroll', h: 390 }]);
    seen(0.75);
    assert.equal(on(), true, 'a small scroll in focus keeps it');
    seen(0.6);
    assert.equal(on(), false, 'scrolled on: out');
    assert.equal(events.at(-1)[0], 'player-focus-exit');
    assert.equal(events.at(-1)[1].exit, 'scroll');
});

test('upright, the stage on screen is nothing', () => {
    fresh({ turned: false });
    seen(1);
    assert.equal(on(), false);
    assert.deepEqual(events, []);
});

test('an audio player has no focus', () => {
    fresh();
    const p = w.document.querySelector('.wt-player');
    p.classList.replace('wt-player--video', 'wt-player--audio');
    seen(1);
    assert.equal(on(), false);
    p.classList.replace('wt-player--audio', 'wt-player--video');
});

test('turned with the film on screen: the stage to the top at once, focus by the turn', () => {
    fresh({ turned: false });
    seen(1); // upright, the player in view
    top = 160;
    turn(true);
    flush();
    assert.deepEqual(scrolls, [{ top: 160, behavior: 'instant' }]);
    top = 0;
    seen(1);
    assert.equal(on(), true);
    assert.equal(events.at(-1)[1].via, 'rotate');
});

test('turned again on the late resize too: iOS sizes the window after the turn', () => {
    fresh({ turned: false });
    seen(1);
    top = 160;
    turn(true);
    flush();
    top = 40;
    w.dispatchEvent(new w.Event('resize'));
    assert.deepEqual(scrolls.at(-1), { top: 40, behavior: 'instant' });
    scrolls.length = 0;
    w.dispatchEvent(new w.Event('resize'));
    assert.deepEqual(scrolls, [], 'once, not on every resize after');
});

test('turned with the film off screen (reading the list): left where it is', () => {
    fresh({ turned: false });
    seen(0.2);
    top = -600;
    turn(true);
    flush();
    assert.deepEqual(scrolls, []);
});

test('turned back upright: focus off, the player under what is fixed at the top', () => {
    fresh();
    seen(1);
    assert.equal(on(), true);
    top = 30;
    turn(false);
    assert.equal(on(), false);
    assert.equal(events.at(-1)[1].exit, 'rotate');
    flush();
    assert.deepEqual(scrolls, [{ top: 30 - (NAVBAR_H + 12), behavior: 'instant' }]);
});

test('between two episodes (the stage held empty) focus stays', () => {
    fresh();
    seen(1);
    const s = stage();
    const mount = s.firstElementChild;
    s.classList.add('wt-player-stage--switching');
    mount.remove();
    w.dispatchEvent(new w.Event('player_replaced'));
    assert.equal(on(), true, 'held while switching');
    s.appendChild(mount);
    s.classList.remove('wt-player-stage--switching');
    w.dispatchEvent(new w.Event('player_replaced'));
    assert.equal(on(), true);
    assert.equal(events.filter((e) => e[0] === 'player-focus').length, 1, 'one entry, no blink');
});

test('in by a scroll of the viewer\'s own: the last px to the top once it rests, not under a finger', async () => {
    fresh();
    seen(0.95);
    top = 20;
    w.dispatchEvent(new w.Event('touchstart'));
    w.dispatchEvent(new w.Event('scroll'));
    await new Promise((r) => setTimeout(r, 200));
    assert.deepEqual(scrolls, [], 'a finger is down');
    w.dispatchEvent(new w.Event('touchend'));
    await new Promise((r) => setTimeout(r, 200));
    assert.deepEqual(scrolls, [{ top: 20, behavior: 'smooth' }]);
    scrolls.length = 0;
    w.dispatchEvent(new w.Event('scroll'));
    await new Promise((r) => setTimeout(r, 200));
    assert.deepEqual(scrolls, [], 'once per entry: a small scroll in focus is not pulled back');
});

test('the status bar stays down while focus is on, and comes back after', () => {
    fresh();
    const bar = w.document.getElementById('torrent-status-sticky');
    const stopBar = initStickyStatus(w.document, { slideMs: 0 });
    const barIO = observers.at(-1);
    barIO.cb([{ isIntersecting: false, boundingClientRect: { bottom: -10 }, rootBounds: { top: NAVBAR_H } }]);
    w.document.dispatchEvent(new w.CustomEvent('torrent-status', { detail: { resourceId: 'r1', moving: true } }));
    assert.equal(bar.hidden, false, 'up: off screen and moving');
    seen(1);
    assert.equal(on(), true);
    assert.equal(bar.classList.contains('-translate-y-full'), true, 'slid away with focus');
    seen(0.5);
    assert.equal(bar.hidden, false);
    stopBar();
});

test('reveal puts the stage at the very top in landscape, and says so', () => {
    fresh();
    notePress(w);
    top = 151;
    scrolls.length = 0;
    assert.equal(reveal(w.document.getElementById('log')), true);
    assert.deepEqual(scrolls, [{ top: 151, behavior: 'smooth' }]);
    assert.equal(stage().dataset.focusVia, 'reveal');
    top = 0;
    seen(1);
    assert.equal(events.at(-1)[1].via, 'reveal');
});

test('upright, reveal is what it was: the log under the navbar, not the stage at the top', () => {
    fresh({ turned: false });
    notePress(w);
    const log = w.document.getElementById('log');
    log.getBoundingClientRect = () => ({ top: 300, bottom: 700, height: 400 });
    top = 300;
    scrolls.length = 0;
    reveal(log);
    assert.deepEqual(scrolls, [{ top: 300 - (NAVBAR_H + 12), behavior: 'smooth' }], 'taller than the space: its top under the navbar');
    assert.equal(stage().dataset.focusVia, undefined);
    delete log.getBoundingClientRect;
});

test('player.css sizes the stage under the very query the script watches', () => {
    const rules = cssRules(new URL('../../styles/player.css', import.meta.url));
    const r = rules.find((x) => x.sel.includes('.wt-player-stage:has(.wt-player--video)') && x.decls.height);
    assert.ok(r, 'the stage rule');
    assert.deepEqual(r.at, [`@media ${FOCUS_MEDIA}`]);
    const style = cssRules(new URL('../../styles/style.css', import.meta.url));
    assert.ok(style.find((x) => x.sel === 'html.wt-focus .navbar-redesign' && x.decls.transform), 'the navbar slides away');
});
