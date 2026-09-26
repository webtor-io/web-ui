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
//     receiving has nothing to report, and the bar would be furniture.

// The navbar the mirror sits under (`top-[72px]` in the markup); the same
// number shrinks the observer's root so the status counts as gone when it
// slides under the navbar rather than when it leaves the window.
const NAVBAR_H = 72;

// Matches `duration-200` on the bar: how long it takes to slide away before
// `hidden` may take it out of the tree.
const SLIDE_MS = 200;

// How long a transfer may look stopped before the bar believes it.
const HOLD_MS = 8000;

export function initStickyStatus(root = document, { slideMs = SLIDE_MS, holdMs = HOLD_MS } = {}) {
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
    const apply = () => {
        const show = offScreen && moving;
        if (show === shown) return;
        shown = show;
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

    // A transfer does not stop being one because a single status said so.
    // The stream reports `unknown` when the seeder's stats are briefly
    // unavailable and `idle` when a stats event is missing, then `caching`
    // again a second later -- and the bar blinked out and back with it
    // (owner, 2026-09-20). Those two are held for a grace period before they
    // count; an answer (`cached`, `vaulted`, `vault_failed`) ends it at once.
    const HOLD = new Set(['unknown', 'idle']);
    let stopTimer = null;
    const onStatus = (e) => {
        if (!e.detail || e.detail.resourceId !== real.dataset.resourceId) return;
        const now = !!e.detail.moving;
        if (now || !HOLD.has(e.detail.state)) {
            if (stopTimer) { clearTimeout(stopTimer); stopTimer = null; }
            setMoving(now);
            apply();
            return;
        }
        if (!moving || stopTimer) return;
        stopTimer = setTimeout(() => { stopTimer = null; setMoving(false); apply(); }, holdMs);
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
        if (stopTimer) { clearTimeout(stopTimer); stopTimer = null; }
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
