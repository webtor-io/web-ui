import test from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!doctype html><body></body>', { url: 'https://webtor.io/' });
global.window = dom.window;
global.document = dom.window.document;
// jsdom has no IntersectionObserver: a fake that hands the callback back, so
// the test can say "the status went off the top" the way a scroll would.
const NAVBAR_H = 72;   // the sticky bar's own offset, and the observer's rootMargin
const STATUS_H = 44;   // the real #torrent-status: badge + piece bar
let observers = [];
global.IntersectionObserver = class {
    constructor(cb, opts) { this.cb = cb; this.opts = opts; this.targets = []; observers.push(this); }
    observe(el) { if (!this.targets.includes(el)) this.targets.push(el); }
    unobserve(el) { this.targets = this.targets.filter((t) => t !== el); }
    disconnect() { this.disconnected = true; this.targets = []; }
    // Only what is actually being observed is delivered -- an observer left
    // on a detached element is exactly the bug these tests are about.
    // `top` is the element's viewport-relative top at the moment the browser
    // reports the crossing; the status is ~44px tall and the root is shrunk
    // by the 72px navbar, so the numbers a real scroll produces matter --
    // see the smooth-scroll test.
    fire(isIntersecting, top, el = document.querySelector('#torrent-status'), height = STATUS_H) {
        if (!this.targets.includes(el)) return false;
        this.cb([{
            isIntersecting,
            boundingClientRect: { top, bottom: top + height, height },
            rootBounds: { top: NAVBAR_H, bottom: 800 },
            target: el,
        }]);
        return true;
    }
};
dom.window.IntersectionObserver = global.IntersectionObserver;
global.requestAnimationFrame = (fn) => setTimeout(fn, 0);

const { initStickyStatus } = await import('./stickyStatus.js');

function page() {
    observers = [];
    document.body.innerHTML = `
        <div id="torrent-status" data-resource-id="res"></div>
        <div id="torrent-status-sticky" hidden class="-translate-y-full">
            <div data-status-badge-for="res"></div>
            <div data-piece-bar-for="res"></div>
        </div>`;
    const stop = initStickyStatus(document, { slideMs: 0 });
    return { stop, bar: document.querySelector('#torrent-status-sticky'), io: observers[0] };
}
const status = (detail) => document.dispatchEvent(new dom.window.CustomEvent('torrent-status', { detail }));
const settle = () => new Promise((r) => setTimeout(r, 5));

test('the mirror needs both: the status out of view AND a transfer moving', async () => {
    const p = page();
    assert.equal(p.bar.hidden, true, 'nothing to mirror yet');

    // Scrolled past the status, but the torrent is idle: still nothing.
    p.io.fire(false, -120);
    status({ resourceId: 'res', state: 'idle', moving: false });
    await settle();
    assert.equal(p.bar.hidden, true);

    // The transfer starts while the viewer is down among the files.
    status({ resourceId: 'res', state: 'caching', moving: true });
    await settle();
    assert.equal(p.bar.hidden, false, 'now it is worth showing');
    assert.equal(p.bar.classList.contains('-translate-y-full'), false, 'and it slides in');

    // Back up to the real status: the mirror steps aside.
    p.io.fire(true, 40);
    await settle();
    assert.equal(p.bar.hidden, true);
    assert.equal(p.bar.classList.contains('-translate-y-full'), true);

    // Down again, transfer still moving.
    p.io.fire(false, -300);
    await settle();
    assert.equal(p.bar.hidden, false);

    // The transfer ends: an answer, not progress.
    status({ resourceId: 'res', state: 'cached', moving: false });
    await settle();
    assert.equal(p.bar.hidden, true);
    p.stop();
});

test('a status that has not been scrolled to yet is not mirrored', async () => {
    const p = page();
    status({ resourceId: 'res', state: 'caching', moving: true });
    // Below the fold (a tall poster on a phone): out of view, but downwards.
    p.io.fire(false, 900);
    await settle();
    assert.equal(p.bar.hidden, true, 'only what left upwards counts');
    p.stop();
});

test('another resource on the page does not drive this mirror', async () => {
    const p = page();
    p.io.fire(false, -100);
    status({ resourceId: 'other', state: 'caching', moving: true });
    await settle();
    assert.equal(p.bar.hidden, true);
    p.stop();
});

test('teardown stops observing and listening', async () => {
    const p = page();
    p.stop();
    assert.equal(p.io.disconnected, true);
    status({ resourceId: 'res', state: 'caching', moving: true });
    await settle();
    assert.equal(p.bar.hidden, true, 'a stopped mirror stays put');
});

test('a page without the markup (or without IntersectionObserver) is left alone', () => {
    document.body.innerHTML = '<div id="torrent-status" data-resource-id="res"></div>';
    assert.equal(initStickyStatus(document), null);
    document.body.innerHTML = '<div id="torrent-status-sticky"></div>';
    assert.equal(initStickyStatus(document), null);
});

test('an async swap that replaces the status is picked up, and the bar sticks again', async () => {
    const p = page();
    status({ resourceId: 'res', state: 'caching', moving: true });
    p.io.fire(false, -100);
    await settle();
    assert.equal(p.bar.hidden, false, 'fixture: shown once');

    // Back up, then the status view reloads itself (its token expired, or an
    // async navigation swapped the page): a NEW #torrent-status element.
    p.io.fire(true, 40);
    await settle();
    const old = document.querySelector('#torrent-status');
    const fresh = old.cloneNode(true);
    old.replaceWith(fresh);
    window.dispatchEvent(new dom.window.CustomEvent('async', { detail: { target: fresh } }));

    // Scrolling down again must still show it: the observer follows the new
    // element, and the status event is still ours.
    assert.ok(p.io.targets.includes(fresh), 'the observer moved to the new element');
    assert.equal(p.io.fire(false, -220, fresh), true, 'and it is what the scroll is reported for');
    status({ resourceId: 'res', state: 'caching', moving: true });
    await settle();
    assert.equal(p.bar.hidden, false, 'the mirror still sticks after the status was replaced');
    p.stop();
});

test('a slow scroll shows it too: the crossing is reported while the top is still positive', async () => {
    const p = page();
    status({ resourceId: 'res', state: 'caching', moving: true });
    // What a smooth scroll actually delivers. The root's top edge is at 72
    // (rootMargin -72px), so the status stops intersecting the moment its
    // BOTTOM passes 72 -- its top is then 72 - 44 = +28, still positive. A
    // flick jumps further in one frame and lands past zero, which is why the
    // old `top < 0` test looked fine on a fast scroll and did nothing on a
    // slow one (owner, 2026-09-20).
    p.io.fire(false, NAVBAR_H - STATUS_H);
    await settle();
    assert.equal(p.bar.hidden, false, 'gone under the navbar counts as gone');
    p.stop();
});

test('still hidden while the status is only partly under the navbar', async () => {
    const p = page();
    status({ resourceId: 'res', state: 'caching', moving: true });
    // Half-way under: the browser would still call this intersecting, and
    // even if it did not, the bar must not double the status still on screen.
    p.io.fire(false, NAVBAR_H - Math.floor(STATUS_H / 2));
    await settle();
    assert.equal(p.bar.hidden, true, 'part of it is still visible');
    p.stop();
});

test('it slides out before it is hidden, and a quick return cancels the hiding', async () => {
    observers = [];
    document.body.innerHTML = `
        <div id="torrent-status" data-resource-id="res"></div>
        <div id="torrent-status-sticky" hidden class="-translate-y-full"></div>`;
    const stop = initStickyStatus(document, { slideMs: 40 });
    const bar = document.querySelector('#torrent-status-sticky');
    const io = observers[0];
    status({ resourceId: 'res', state: 'caching', moving: true });
    io.fire(false, -100);
    await settle();
    assert.equal(bar.hidden, false);

    // Scrolled back up: the slide starts, the bar is still in the tree.
    io.fire(true, 100);
    await settle();
    assert.equal(bar.classList.contains('-translate-y-full'), true, 'sliding away');
    assert.equal(bar.hidden, false, 'not yanked out mid-slide');

    // And down again before the slide ended: it must come back, not stay
    // parked off-screen, and the pending hide must not fire later.
    io.fire(false, -100);
    await new Promise((r) => setTimeout(r, 80));
    assert.equal(bar.hidden, false, 'the cancelled hide never lands');
    assert.equal(bar.classList.contains('-translate-y-full'), false, 'and it is back in view');

    io.fire(true, 100);
    await new Promise((r) => setTimeout(r, 80));
    assert.equal(bar.hidden, true, 'left alone, the slide ends in hidden');
    stop();
});
