import { useState, useRef, useCallback } from 'preact/hooks';
import { RATES, rateLabel, stepRate } from './player-prefs';
import { HAS_POPOVER, useAnchoredPopover } from './useAnchoredPopover';
import { t } from './i18n';

/**
 * Playback speed: the current rate as the button's face, a short menu above
 * it. The audio player is one row tall, so there the button steps through
 * the scale instead and wraps at the top.
 *
 * The menu is a top-layer popover placed from the button's rect
 * (useAnchoredPopover): .wt-player is overflow:hidden, and on a phone the
 * picture is shorter than seven rows -- the list was cut off by the player's
 * frame (owner, 2026-09-20). The top layer is also what keeps it visible in
 * fullscreen.
 */
export function SpeedControl({ rate, onRateChange, menu }) {
    const [open, setOpen] = useState(false);
    const rootRef = useRef(null);
    const btnRef = useRef(null);
    const menuRef = useRef(null);
    useAnchoredPopover(open, setOpen, rootRef, btnRef, menuRef);

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
                    class={`wt-player-menu wt-player-menu--narrow${open ? ' wt-player-menu--open' : ''}`}>
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
