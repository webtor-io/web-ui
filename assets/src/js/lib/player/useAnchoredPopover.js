import { useEffect } from 'preact/hooks';

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
