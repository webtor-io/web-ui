import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { RATES, rateLabel, stepRate } from './player-prefs';
import { t } from './i18n';

/**
 * Playback speed: the current rate as the button's face, a short menu above
 * it. The audio player is one row tall, so there the button steps through
 * the scale instead and wraps at the top.
 *
 * The menu is a popover (top layer), placed by hand from the button's rect:
 * .wt-player is overflow:hidden, and on a phone the picture is shorter than
 * seven rows -- the list was cut off by the player's frame (owner,
 * 2026-09-20). The top layer is also what keeps it visible in fullscreen.
 * A browser without the Popover API gets the old in-flow menu, clipped but
 * usable.
 */
// Only with the API does the attribute go on: the `[popover]` styles hide the
// menu until :popover-open, which an older browser would never reach.
const HAS_POPOVER = typeof HTMLElement !== 'undefined' && typeof HTMLElement.prototype.showPopover === 'function';

export function SpeedControl({ rate, onRateChange, menu }) {
    const [open, setOpen] = useState(false);
    const rootRef = useRef(null);
    const btnRef = useRef(null);
    const menuRef = useRef(null);

    // Show/hide and place the popover. Fixed coordinates: right edges
    // aligned, the menu above the button; if the space above is too short it
    // is pushed down just enough to stay on screen.
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
        // It is placed once: anything that moves the button closes it.
        const close = () => setOpen(false);
        window.addEventListener('scroll', close, true);
        window.addEventListener('resize', close);
        return () => {
            window.removeEventListener('scroll', close, true);
            window.removeEventListener('resize', close);
        };
    }, [open]);

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

    const onButton = useCallback(() => {
        if (menu) { setOpen((v) => !v); return; }
        const next = stepRate(rate, +1);
        onRateChange(next === rate ? RATES[0] : next);
    }, [menu, rate, onRateChange]);

    const pick = useCallback((r) => {
        onRateChange(r);
        setOpen(false);
    }, [onRateChange]);

    return (
        <div class="wt-player-speed" ref={rootRef}>
            <button type="button" ref={btnRef} class={`wt-player-btn wt-player-btn--speed${rate !== 1 ? ' wt-player-btn--speed-on' : ''}`}
                onClick={onButton} aria-label={t('player.speed')} title={t('player.speed')}
                aria-haspopup={menu ? 'menu' : undefined} aria-expanded={menu ? open : undefined}>
                {rateLabel(rate)}
            </button>
            {menu && (
                <div ref={menuRef} popover={HAS_POPOVER ? 'manual' : undefined} role="menu" aria-label={t('player.speed')}
                    class={`wt-player-speed-menu${open ? ' wt-player-speed-menu--open' : ''}`}>
                    {RATES.map((r) => (
                        <button type="button" role="menuitemradio" aria-checked={r === rate}
                            class={`wt-player-speed-item${r === rate ? ' wt-player-speed-item--active' : ''}`}
                            onClick={() => pick(r)}>
                            {rateLabel(r)}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}
