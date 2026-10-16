// lib/landscapeFocus.js: a phone on its side, the film takes the window
// (owner, 2026-10-15). jsdom has no layout and no media queries: the
// orientation, the observer's readings and the stage's box are given.
import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { cssRules, findRule } from '../test/css.mjs';

const dom = new JSDOM(`<!doctype html><body>
    <div id="torrent-status" data-resource-id="r1"></div>
    <div id="torrent-status-sticky" hidden class="-translate-y-full"></div>
    <div id="content"><div id="log"><div class="wt-player-stage"><div class="wt-player-mount"><div class="wt-player wt-player--video"></div></div></div></div></div>
</body>`, { url: 'https://webtor.io/' });
const w = dom.window;
global.window = w;
global.document = w.document;

// The exact queries the code asks, nothing else matches: FOCUS_MEDIA (set
// once imported) as the phone is turned, reduced motion as asked for.
let FOCUS = null;
let landscape = false;
let reduced = false;
const mqListeners = new Set();
w.matchMedia = (q) => ({
    get matches() { return q === FOCUS ? landscape : q === '(prefers-reduced-motion: reduce)' ? reduced : false; },
    addEventListener: (_, f) => { if (q === FOCUS) mqListeners.add(f); },
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
const flush = () => { while (frames.length) frames.shift()(); };
w.innerHeight = 390;
const scrolls = [];
w.scrollTo = (o) => scrolls.push(o);
const events = [];
w.umami = { track: (name, props) => events.push([name, props]) };
let top = 0;
stage().getBoundingClientRect = () => ({ top, bottom: top + 390, height: 390 });
let fullscreen = null;
Object.defineProperty(w.document, 'fullscreenElement', { get: () => fullscreen });
const touch = (type, fingers) => {
    const e = new w.Event(type);
    e.touches = { length: fingers };
    w.dispatchEvent(e);
};

const { initLandscapeFocus } = await import('./landscapeFocus.js');
const { initStickyStatus, reveal, notePress, FOCUS_MEDIA, NAVBAR_H } = await import('./stickyStatus.js');
FOCUS = FOCUS_MEDIA;
const html = w.document.documentElement;
const on = () => html.classList.contains('wt-focus');

let stop = null;
function fresh({ turned = true } = {}) {
    if (stop) stop();
    landscape = turned;
    reduced = false;
    fullscreen = null;
    top = 0;
    delete stage().dataset.focusVia;
    stop = initLandscapeFocus(w.document);
    scrolls.length = 0;
    events.length = 0;
    frames.length = 0;
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

test('an audio player has no focus, not even while it switches tracks', () => {
    fresh();
    const p = w.document.querySelector('.wt-player');
    p.classList.replace('wt-player--video', 'wt-player--audio');
    try {
        seen(1);
        assert.equal(on(), false);
        stage().classList.add('wt-player-stage--switching');
        w.dispatchEvent(new w.Event('player_replaced'));
        assert.equal(on(), false, '--switching holds a focus, it does not open one');
    } finally {
        stage().classList.remove('wt-player-stage--switching');
        p.classList.replace('wt-player--audio', 'wt-player--video');
    }
});

test('turned with the film on screen: focus by the turn, the stage to the top at once', () => {
    fresh({ turned: false });
    seen(1); // upright, the player in view
    top = 160;
    turn(true);
    assert.equal(on(), true, 'on with the turn itself: the observer has nothing new to say');
    assert.equal(events.at(-1)[1].via, 'rotate');
    flush();
    assert.deepEqual(scrolls, [{ top: 160, behavior: 'instant' }]);
});

test('turned again on the late resize too, within a second only: iOS sizes the window after the turn', (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
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
    seen(0.5); // out
    seen(1);
    turn(false);
    turn(true);
    flush();
    scrolls.length = 0;
    t.mock.timers.tick(1000);
    w.dispatchEvent(new w.Event('resize'));
    assert.deepEqual(scrolls, [], 'a resize a second later is the viewer\'s, not the turn\'s');
});

test('a finger down after a turn cancels the late realign', () => {
    fresh({ turned: false });
    seen(1);
    turn(true);
    flush();
    scrolls.length = 0;
    touch('touchstart', 1);
    top = 40;
    w.dispatchEvent(new w.Event('resize'));
    assert.deepEqual(scrolls, []);
    touch('touchend', 0);
});

test('turned with the film off screen (reading the list): left where it is', () => {
    fresh({ turned: false });
    seen(0.2);
    top = -600;
    turn(true);
    flush();
    assert.deepEqual(scrolls, []);
    assert.equal(on(), false);
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
    try {
        w.dispatchEvent(new w.Event('player_replaced'));
        assert.equal(on(), true, 'held while switching');
    } finally {
        s.appendChild(mount);
        s.classList.remove('wt-player-stage--switching');
    }
    w.dispatchEvent(new w.Event('player_replaced'));
    assert.equal(on(), true);
    assert.equal(events.filter((e) => e[0] === 'player-focus').length, 1, 'one entry, no blink');
});

test('a swap that carried the film over puts it back at the top; one elsewhere does not', () => {
    fresh();
    seen(1);
    top = 24; // the new card above it is taller
    w.dispatchEvent(new w.CustomEvent('async', { detail: { target: w.document.getElementById('torrent-status') } }));
    assert.deepEqual(scrolls, [], 'the status block renewing is not this');
    w.dispatchEvent(new w.CustomEvent('async', { detail: { target: w.document.getElementById('content') } }));
    assert.deepEqual(scrolls, [{ top: 24, behavior: 'instant' }]);
    scrolls.length = 0;
    touch('touchstart', 1);
    w.dispatchEvent(new w.CustomEvent('async', { detail: { target: w.document.getElementById('content') } }));
    assert.deepEqual(scrolls, [], 'not against a finger');
    touch('touchend', 0);
});

test('fullscreen: a turn in it waits for its end, and the end is taken as the turn', () => {
    fresh({ turned: false });
    seen(1);
    fullscreen = stage();
    top = 160;
    turn(true);
    flush();
    assert.equal(on(), false, 'nothing while fullscreen');
    assert.deepEqual(scrolls, []);
    seen(1); // the stage is the screen in fullscreen
    assert.equal(on(), false);
    fullscreen = null;
    top = 300;
    w.document.dispatchEvent(new w.Event('fullscreenchange'));
    assert.equal(on(), true);
    assert.equal(events.at(-1)[1].via, 'fullscreen');
    flush();
    assert.deepEqual(scrolls, [{ top: 300, behavior: 'instant' }]);
});

test('in by a scroll of the viewer\'s own: the last px to the top once it rests, not under a finger', (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    fresh();
    touch('touchstart', 1);
    seen(0.95);
    top = 20;
    w.dispatchEvent(new w.Event('scroll'));
    t.mock.timers.tick(200);
    assert.deepEqual(scrolls, [], 'a finger is down');
    touch('touchend', 0);
    for (let i = 0; i < 3; i++) {
        t.mock.timers.tick(100);
        w.dispatchEvent(new w.Event('scroll')); // momentum
    }
    assert.deepEqual(scrolls, [], 'still moving');
    t.mock.timers.tick(150);
    assert.deepEqual(scrolls, [{ top: 20, behavior: 'smooth' }]);
    scrolls.length = 0;
    w.dispatchEvent(new w.Event('scroll'));
    t.mock.timers.tick(200);
    assert.deepEqual(scrolls, [], 'once per entry: a small scroll in focus is not pulled back');
});

test('the scroll that crossed in was the last one: pulled all the same', (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    fresh();
    seen(0.95);
    top = -27;
    t.mock.timers.tick(150);
    assert.deepEqual(scrolls, [{ top: -27, behavior: 'smooth' }]);
});

test('the pull is instant for who asked for less motion', (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    fresh();
    reduced = true;
    seen(0.95);
    top = 20;
    t.mock.timers.tick(150);
    assert.deepEqual(scrolls, [{ top: 20, behavior: 'instant' }]);
});

test('one finger lifted of two is still a finger down', (t) => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    fresh();
    touch('touchstart', 2);
    seen(0.95);
    top = 20;
    touch('touchend', 1);
    t.mock.timers.tick(200);
    assert.deepEqual(scrolls, []);
    touch('touchend', 0);
    t.mock.timers.tick(150);
    assert.deepEqual(scrolls, [{ top: 20, behavior: 'smooth' }]);
});

test('the status bar stays down while focus is on, and comes back after', () => {
    fresh();
    const bar = w.document.getElementById('torrent-status-sticky');
    const stopBar = initStickyStatus(w.document, { slideMs: 0 });
    const barIO = observers.at(-1);
    barIO.cb([{ isIntersecting: false, boundingClientRect: { bottom: -10 }, rootBounds: { top: NAVBAR_H } }]);
    w.document.dispatchEvent(new w.CustomEvent('torrent-status', { detail: { resourceId: 'r1', moving: true } }));
    flush();
    assert.equal(bar.classList.contains('-translate-y-full'), false, 'up: off screen and moving');
    seen(1);
    assert.equal(on(), true);
    assert.equal(bar.classList.contains('-translate-y-full'), true, 'slid away with focus');
    seen(0.5);
    flush();
    assert.equal(bar.classList.contains('-translate-y-full'), false, 'back on screen');
    stopBar();
});

test('torn down in focus: the class goes with the view, the navbar is not left out of reach', () => {
    fresh();
    seen(1);
    assert.equal(on(), true);
    stop();
    stop = null;
    assert.equal(on(), false);
    assert.equal(events.at(-1)[0], 'player-focus-exit');
    assert.equal(events.at(-1)[1].exit, 'gone');
});

test('reveal puts the stage at the very top in landscape, and says so', () => {
    fresh();
    notePress(w);
    top = 151;
    assert.equal(reveal(w.document.getElementById('log')), true);
    assert.deepEqual(scrolls, [{ top: 151, behavior: 'smooth' }]);
    assert.equal(stage().dataset.focusVia, 'reveal');
    top = 0;
    seen(1);
    assert.equal(events.at(-1)[1].via, 'reveal');
    assert.equal(stage().dataset.focusVia, undefined, 'used once');
});

test('a reveal while in focus does not label the next entry', () => {
    fresh();
    seen(1);
    notePress(w);
    reveal(w.document.getElementById('log')); // already at the top: a no-op that still tags
    seen(0.5);
    seen(0.95);
    assert.equal(events.at(-1)[1].via, 'scroll', 'the viewer scrolled in, whatever reveal said before');
});

test('upright, reveal is what it was: the log under the navbar, not the stage at the top', () => {
    fresh({ turned: false });
    notePress(w);
    const log = w.document.getElementById('log');
    log.getBoundingClientRect = () => ({ top: 300, bottom: 700, height: 400 });
    top = 300;
    try {
        reveal(log);
        assert.deepEqual(scrolls, [{ top: 300 - (NAVBAR_H + 12), behavior: 'smooth' }], 'taller than the space: its top under the navbar');
        assert.equal(stage().dataset.focusVia, undefined);
    } finally {
        delete log.getBoundingClientRect;
    }
});

test('player.css sizes the stage under the very query the script watches; the navbar slides out of reach', () => {
    const at = [`@media ${FOCUS_MEDIA}`];
    const rules = cssRules(new URL('../../styles/player.css', import.meta.url));
    const s = findRule(rules, 'body:not([data-iframe-height]) .wt-player-stage:has(.wt-player--video)', at);
    assert.ok(s, 'the stage rule, the embed left out');
    assert.equal(s.width, '100vw');
    assert.equal(s.height, '100dvh');
    const m = findRule(rules, 'body:not([data-iframe-height]) .wt-player-stage:has(.wt-player--video) .wt-player-mount', at);
    assert.equal(m.margin, '0', 'all of it: the phone rule\'s side bleed would pan the page');
    assert.equal(m.width, '100%');
    const style = cssRules(new URL('../../styles/style.css', import.meta.url));
    const nav = findRule(style, 'html.wt-focus .navbar-redesign');
    assert.equal(nav.transform, 'translateY(-100%)');
    assert.equal(nav.visibility, 'hidden', 'out of the tab order once slid');
});
