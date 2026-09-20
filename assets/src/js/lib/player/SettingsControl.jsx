import { useState, useRef } from 'preact/hooks';
import { GearIcon } from './icons';
import { HAS_POPOVER, useAnchoredPopover } from './useAnchoredPopover';
import { t } from './i18n';

/**
 * The gear: settings that are about the player rather than about this moment
 * of the film. Today one -- autoplay of the next episode / track -- reachable
 * at any time, with its name next to it (owner, 2026-09-20: a bare switch in
 * the audio bar said nothing about what it switched).
 */
export function SettingsControl({ autoplayNext, onToggleAutoplayNext }) {
    const [open, setOpen] = useState(false);
    const rootRef = useRef(null);
    const btnRef = useRef(null);
    const menuRef = useRef(null);
    useAnchoredPopover(open, setOpen, rootRef, btnRef, menuRef);

    return (
        <div class="wt-player-settings" ref={rootRef}>
            <button type="button" ref={btnRef} class="wt-player-btn" onClick={() => setOpen((v) => !v)}
                aria-label={t('player.settings')} title={t('player.settings')} aria-haspopup="menu" aria-expanded={open}>
                <GearIcon />
            </button>
            <div ref={menuRef} popover={HAS_POPOVER ? 'manual' : undefined} role="menu" aria-label={t('player.settings')}
                class={`wt-player-menu${open ? ' wt-player-menu--open' : ''}`}>
                <label class="wt-player-menu-row">
                    <span>{t('player.autoplayNext')}</span>
                    <input type="checkbox" role="switch" class="toggle toggle-soft toggle-sm" checked={autoplayNext} onChange={onToggleAutoplayNext} />
                </label>
            </div>
        </div>
    );
}
