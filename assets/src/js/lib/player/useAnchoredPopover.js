import { useEffect, useRef } from 'preact/hooks';

// A menu that opens above a control and must not be cut by the player's frame
// (.wt-player is overflow:hidden; the audio player is one row tall). It is a
// `popover="manual"` element -- top layer, so neither overflow nor fullscreen
// clips it -- placed by hand from the button's rect: right edges aligned, above
// the button, pushed down just enough to stay on screen. Placed once: anything
// that moves the button (scroll, resize) closes it. Closes on a press outside
// and on Escape. Shared by the speed and the settings menus.
//
// A browser without the Popover API gets an in-flow menu (the `--open` class),
// clipped but usable; callers put the `popover` attribute on only when
// HAS_POPOVER, because the `[popover]` styles hide the element until
// :popover-open, which an older browser never reaches.
export const HAS_POPOVER = typeof HTMLElement !== 'undefined' && typeof HTMLElement.prototype.showPopover === 'function';

export function useAnchoredPopover(open, setOpen, rootRef, btnRef, menuRef) {
    useEffect(() => {
        if (!open) return undefined;
        const onDown = (e) => {
            if (rootRef.current && !rootRef.current.contains(e.target)) setOpen(false);
        };
        const onKey = (e) => { if (e.key === 'Escape') setOpen(false); };
        document.addEventListener('pointerdown', onDown, true);
        document.addEventListener('keydown', onKey);
        return () => {
            document.removeEventListener('pointerdown', onDown, true);
            document.removeEventListener('keydown', onKey);
        };
    }, [open]);

    useEffect(() => {
        const el = menuRef.current;
        if (!el || typeof el.showPopover !== 'function') return undefined;
        if (!open) {
            try { if (el.matches(':popover-open')) el.hidePopover(); } catch (e) { /* already closed */ }
            return undefined;
        }
        try { el.showPopover(); } catch (e) { /* already open */ }
        const b = btnRef.current.getBoundingClientRect();
        const m = el.getBoundingClientRect();
        const gap = 8;
        const top = Math.max(gap, b.top - gap - m.height);
        const left = Math.max(gap, Math.min(window.innerWidth - gap - m.width, b.right - m.width));
        el.style.top = `${Math.round(top)}px`;
        el.style.left = `${Math.round(left)}px`;
        const close = () => setOpen(false);
        window.addEventListener('scroll', close, true);
        window.addEventListener('resize', close);
        return () => {
            window.removeEventListener('scroll', close, true);
            window.removeEventListener('resize', close);
        };
    }, [open]);
}

// useDockedPopover: a card docked to the bottom-right corner of the player,
// above the control bar -- and allowed to stick out of the player's frame. On
// a phone-width player the "up next" card is taller than the room above the
// controls, and inside the frame (overflow:hidden) its top was cut off (owner,
// 2026-09-20). Same cure as the menus: the top layer, placed by hand.
//
// Unlike a menu it stays up while the page moves, so it is re-placed on
// scroll and resize instead of closed. And it is re-SHOWN when fullscreen
// changes: the top layer is ordered by arrival, so an element that goes
// fullscreen after the card came up would cover it.
const CONTROLS_CLEARANCE = 76; // the control bar, .wt-next-card's old `bottom`
const EDGE = 12;

// `relayoutKey`: anything whose change alters the card's height (a line of
// text appearing). The card is docked by its BOTTOM edge, so it has to be
// placed again, or it would grow down over the controls.
export function useDockedPopover(visible, anchorEl, cardRef, relayoutKey = '') {
    const placeRef = useRef(null);
    useEffect(() => { if (placeRef.current) placeRef.current(); }, [relayoutKey]);
    useEffect(() => {
        const el = cardRef.current;
        if (!el || !anchorEl || typeof el.showPopover !== 'function') return undefined;
        if (!visible) {
            try { if (el.matches(':popover-open')) el.hidePopover(); } catch (e) { /* already closed */ }
            return undefined;
        }
        const place = () => {
            const a = anchorEl.getBoundingClientRect();
            const c = el.getBoundingClientRect();
            const top = Math.max(EDGE, a.bottom - CONTROLS_CLEARANCE - c.height);
            const left = Math.max(EDGE, Math.min(window.innerWidth - EDGE - c.width, a.right - EDGE - c.width));
            el.style.top = `${Math.round(top)}px`;
            el.style.left = `${Math.round(left)}px`;
        };
        placeRef.current = place;
        const show = () => {
            try { if (el.matches(':popover-open')) el.hidePopover(); } catch (e) { /* not open */ }
            try { el.showPopover(); } catch (e) { /* already open */ }
            place();
        };
        show();
        window.addEventListener('scroll', place, true);
        window.addEventListener('resize', place);
        document.addEventListener('fullscreenchange', show);
        document.addEventListener('webkitfullscreenchange', show);
        return () => {
            window.removeEventListener('scroll', place, true);
            window.removeEventListener('resize', place);
            document.removeEventListener('fullscreenchange', show);
            document.removeEventListener('webkitfullscreenchange', show);
            placeRef.current = null;
            try { if (el.matches(':popover-open')) el.hidePopover(); } catch (e) { /* gone */ }
        };
    }, [visible, anchorEl]);
}
