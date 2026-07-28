// Pointer reactivity for the hero backdrop (glow + wave/dust band).
//
// The decoration itself stays pure CSS — this module only publishes the
// pointer state as custom properties on the hero <section> and lets the
// stylesheet decide what moves how far:
//
//   --hx, --hy   pointer offset from the hero centre, normalized to -1..1
//
// Values are eased toward their target in a rAF loop that parks itself as
// soon as everything has settled, so an idle or absent pointer costs zero
// frames. Coarse pointers (no hover) and prefers-reduced-motion get the
// static hero — the module bails out before attaching anything.

const EASE = 0.09;   // per-frame approach to the target
const EPS = 0.0008;  // closer than this counts as settled

const noop = () => {};

function clamp(v) {
    return v < -1 ? -1 : v > 1 ? 1 : v;
}

export function initHeroPointer(root) {
    if (!root) return noop;
    if (!window.matchMedia('(hover: hover) and (pointer: fine)').matches) return noop;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return noop;

    const target = {x: 0, y: 0};
    const cur = {x: 0, y: 0};
    let frame = null;

    function start() {
        if (frame === null) frame = requestAnimationFrame(tick);
    }

    function tick() {
        frame = null;
        let moving = false;

        for (const k of ['x', 'y']) {
            const d = target[k] - cur[k];
            if (Math.abs(d) > EPS) {
                cur[k] += d * EASE;
                moving = true;
            } else {
                cur[k] = target[k];
            }
        }
        root.style.setProperty('--hx', cur.x.toFixed(4));
        root.style.setProperty('--hy', cur.y.toFixed(4));

        if (moving) start();
    }

    function onMove(e) {
        const r = root.getBoundingClientRect();
        if (!r.width || !r.height) return;
        target.x = clamp(((e.clientX - r.left) / r.width) * 2 - 1);
        target.y = clamp(((e.clientY - r.top) / r.height) * 2 - 1);
        start();
    }

    // Settle back to the centred state whenever the pointer is gone — leaving
    // the hero, or the window losing focus mid-hover (tab switch, cmd-tab),
    // which never fires pointerleave.
    function onLeave() {
        target.x = 0;
        target.y = 0;
        start();
    }

    root.addEventListener('pointermove', onMove, {passive: true});
    root.addEventListener('pointerleave', onLeave, {passive: true});
    window.addEventListener('blur', onLeave);

    return function destroy() {
        root.removeEventListener('pointermove', onMove);
        root.removeEventListener('pointerleave', onLeave);
        window.removeEventListener('blur', onLeave);
        if (frame !== null) cancelAnimationFrame(frame);
        frame = null;
        root.style.removeProperty('--hx');
        root.style.removeProperty('--hy');
    };
}
