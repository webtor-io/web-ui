// The torrent status, kept on screen while the viewer waits.
//
// Measured 2026-09-19: the median wait from pressing Watch to the first frame
// is 58 s, and only 4.8% are playing within 10 s -- the swarm, not our code.
// The one sign that anything is happening is the status badge and its piece
// bar, and both live in the page header: scroll down to the files and they are
// gone. 59% of streaming sessions pressed Watch more than once.
//
// So when the real status leaves the viewport and something is moving, a
// mirror of it appears under the navbar, full width. It carries no state of
// its own: it is a copy of the card's status block, updated by the status
// view's own SSE (resource/status.js clones the block into
// [data-status-mirror-for] and writes every message into both), and this
// module only decides when it is on screen.
//
// Two conditions, both required:
//   - the real status is out of view (IntersectionObserver, not a scroll
//     listener: no work on the scroll thread, and it is right on the first
//     frame rather than after the first scroll). Note for anyone testing this
//     with browser automation: a hidden tab runs neither IntersectionObserver
//     nor requestAnimationFrame, so the bar never appears there and the tab's
//     silence proves nothing;
//   - something is moving (the `moving` of the torrent-status event, the
//     view's `sticky`): bytes from the swarm, bytes to the viewer, or the
//     plan's cap binding -- a finished or idle torrent that nobody is
//     receiving has nothing to report, and the bar would be furniture. A
//     gap in the data (the seeder's stats a second away) is held by the
//     status view itself: `moving` is the view the bar keeps through it
//     (app/resource/status.js), so the bar does not blink with it (owner,
//     2026-09-20) and goes once the gap is believed.

// The navbar the mirror sits under (`top-[72px]` in the markup); the same
// number shrinks the observer's root so the status counts as gone when it
// slides under the navbar rather than when it leaves the window. A plain
// constant: the status view imports it too (two bundles, no state).
export const NAVBAR_H = 72;

// Matches `duration-200` on the bar: how long it takes to slide away before
// `hidden` may take it out of the tree.
const SLIDE_MS = 200;

export function initStickyStatus(root = document, { slideMs = SLIDE_MS } = {}) {
    const bar = root.querySelector('#torrent-status-sticky');
    let real = root.querySelector('#torrent-status');
    if (!bar || !real || typeof IntersectionObserver !== 'function') return null;

    let offScreen = false;
    let moving = false;
    // What was last asked for, not `bar.hidden`: while the bar is sliding
    // away it is still un-hidden, and reading the attribute there would take
    // "show it again" for "already shown" and leave it parked off-screen.
    let shown = false;
    let hideTimer = null;
    const html = bar.ownerDocument.documentElement;
    // Keyboard focus and anchors stop below what is fixed at the top: the
    // navbar alone is style.css's html scroll-padding-top; while the bar is
    // up its height joins it, measured on every call -- the box in it comes
    // and goes.
    const pad = () => { html.style.scrollPaddingTop = shown ? `${stickyBottom(root)}px` : ''; };
    const apply = () => {
        const show = offScreen && moving;
        if (show === shown) { pad(); return; }
        shown = show;
        pad();
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
        // `hidden` is what takes it out of the a11y tree and off the screen;
        // the transform is the movement. Unhidden first, so the transition
        // has something to run on -- and hidden last, so the way out is a
        // slide as well and not a blink.
        if (show) {
            bar.hidden = false;
            // Next frame: an element unhidden and untransformed in the same
            // frame slides from nowhere.
            requestAnimationFrame(() => { if (shown) bar.classList.remove('-translate-y-full'); });
        } else {
            // Its details popover sits in the top layer, anchored to a chain
            // that is leaving: closed with it, or it would stay open,
            // unanchored, and come back open with the bar.
            const Ev = bar.ownerDocument.defaultView.CustomEvent;
            for (const d of bar.querySelectorAll('[data-tx-details]')) d.dispatchEvent(new Ev('tx-close'));
            bar.classList.add('-translate-y-full');
            hideTimer = setTimeout(() => { hideTimer = null; if (!shown) bar.hidden = true; }, slideMs);
        }
    };
    const setMoving = (now) => {
        moving = now;
        bar.toggleAttribute('data-moving', now);
    };

    const io = new IntersectionObserver((entries) => {
        for (const e of entries) {
            // Only what leaves upwards counts: the status is above the files,
            // so a viewer reading the file list has it off the top. One that
            // has not been scrolled to yet (below the fold, a tall poster on a
            // phone) is not something to mirror.
            //
            // Measured against the ROOT's top edge, not against zero (owner,
            // 2026-09-19: "works on a fast scroll, nothing on a slow one").
            // The observer fires at the crossing itself, and the status is a
            // ~24px badge under a 72px margin: at that moment its top is still
            // about +48, so `top < 0` was false and -- with no further
            // threshold to cross -- stayed false for the rest of the scroll. A
            // flick jumped far enough in one frame to land past zero, which is
            // why a fast scroll looked fine.
            const rootTop = e.rootBounds ? e.rootBounds.top : NAVBAR_H;
            offScreen = !e.isIntersecting && e.boundingClientRect.bottom <= rootTop;
        }
        apply();
    }, { threshold: 0, rootMargin: `-${NAVBAR_H}px 0px 0px 0px` });
    io.observe(real);

    const onStatus = (e) => {
        if (!e.detail || e.detail.resourceId !== real.dataset.resourceId) return;
        setMoving(!!e.detail.moving);
        apply();
    };
    document.addEventListener('torrent-status', onStatus);

    // The status is an async view: its own token renewal reloads it, and any
    // async swap of the page around it can hand back a NEW #torrent-status.
    // An observer left on the old, detached one never fires again -- the bar
    // hid on the way up and never came back (owner, 2026-09-19). So every
    // swap re-resolves the element and re-observes it.
    const onSwap = () => {
        const fresh = document.querySelector('#torrent-status');
        if (!fresh || fresh === real) return;
        io.unobserve(real);
        real = fresh;
        io.observe(real);
    };
    window.addEventListener('async', onSwap);

    return () => {
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
        html.style.scrollPaddingTop = '';
        io.disconnect();
        document.removeEventListener('torrent-status', onStatus);
        window.removeEventListener('async', onSwap);
    };
}

// stickyBottom is where what is fixed at the top of the page ends once the
// status block has scrolled away: the navbar, and under it the sticky status
// whenever something moves -- it will be up by the time a scroll lands. The
// bar mirrors the whole block (plan box included), so its height is measured,
// not assumed: unhidden and hidden again in the same task, nothing painted.
export function stickyBottom(root = document) {
    const bar = root.querySelector('#torrent-status-sticky');
    if (!bar || !bar.hasAttribute('data-moving')) return NAVBAR_H;
    const was = bar.hidden;
    bar.hidden = false;
    const h = bar.offsetHeight;
    bar.hidden = was;
    return NAVBAR_H + h;
}

// fixedBottom is where what is fixed at the top ends right now: the sticky
// bar's bottom while it is up, the navbar's otherwise -- unlike stickyBottom,
// not where it will end once a scroll lands.
export function fixedBottom(root = document) {
    const bar = root.querySelector('#torrent-status-sticky');
    if (!bar || bar.hidden || bar.classList.contains('-translate-y-full')) return NAVBAR_H;
    return bar.getBoundingClientRect().bottom;
}

// reveal brings what a job start just produced into view: the Turnstile step,
// the job's log, its card, the player (app/action.js, lib/turnstileAction.js).
// Measured on prod 2026-10-14: the buttons sit at y=640-680 on every screen
// from 390x844 to 1920x1080 and all of that rendered below them, under the
// fold at 768-844 px, with nothing moving the page toward it.
//
// As little as possible, and never against the viewer:
//   - nothing when it is already wholly in view (between what is fixed at
//     the top -- stickyBottom: the navbar, and the status bar once it will
//     be up -- and the window's bottom);
//   - otherwise its bottom to the fold, but its top never under the bars: a
//     player taller than the space shows from its top;
//   - nothing inside a <dialog> (its own box scrolls; TAR/ZIP);
//   - nothing once the viewer has scrolled themselves since the press
//     (wheel, touch, scroll keys): they went somewhere on purpose. Not seen:
//     dragging the scrollbar, Tab moving focus, a wheel over the Turnstile
//     iframe -- and a press anywhere on the page counts for every job on it.
// Smooth, unless the viewer asked for reduced motion: then 'instant' -- not
// 'auto', which defers to <html class="scroll-smooth"> and animates anyway.
const GAP = 12;
const SCROLL_KEYS = new Set(['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', 'Home', 'End', ' ']);

// One record per page, on window: this module is in more than one bundle.
// Order, not time: a press and a wheel in the same millisecond still have a
// "before".
function record(w) {
    if (w.__reveal) return w.__reveal;
    const r = w.__reveal = { seq: 0, pressAt: 0, scrollAt: 0 };
    const mark = () => { r.scrollAt = ++r.seq; };
    w.addEventListener('wheel', mark, { passive: true, capture: true });
    w.addEventListener('touchmove', mark, { passive: true, capture: true });
    w.addEventListener('keydown', (e) => {
        if (!SCROLL_KEYS.has(e.key)) return;
        const t = e.target;
        if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) return;
        // Space on a button or a link presses it -- the busy Watch pressed
        // again -- and scrolls nothing.
        if (e.key === ' ' && t && t.closest && t.closest('button, a[href], summary, [role=button]')) return;
        mark();
    }, { capture: true });
    return r;
}

// notePress: a job start was pressed now (lib/actionBusy.js markBusy).
export function notePress(w = window) {
    const r = record(w);
    r.pressAt = ++r.seq;
}

// stillAtPress: the viewer has not scrolled away since the last press.
export function stillAtPress(w = window) {
    const r = record(w);
    return r.scrollAt <= r.pressAt;
}

export function reveal(el, root = document) {
    if (!el || !el.isConnected || el.closest('dialog')) return false;
    const w = el.ownerDocument.defaultView;
    if (!stillAtPress(w)) return false;
    const box = el.getBoundingClientRect();
    if (!box.height) return false;
    const top = stickyBottom(root) + GAP;
    const bottom = w.innerHeight - GAP;
    let dy = 0;
    if (box.bottom > bottom) dy = box.bottom - bottom;
    if (box.top - dy < top) dy = box.top - top;
    if (Math.abs(dy) < 1) return false;
    const reduce = typeof w.matchMedia === 'function' && w.matchMedia('(prefers-reduced-motion: reduce)').matches;
    w.scrollTo({ top: w.scrollY + dy, behavior: reduce ? 'instant' : 'smooth' });
    return true;
}
