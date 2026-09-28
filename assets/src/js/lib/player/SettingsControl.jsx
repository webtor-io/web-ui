import { useState, useRef } from 'preact/hooks';
import { MoreIcon } from './icons';
import { HAS_POPOVER, useAnchoredPopover } from './useAnchoredPopover';
import { t } from './i18n';

/**
 * The "more" menu (three dots): settings that are about the player rather than about this moment
 * of the film. Autoplay of the next episode / track -- reachable at any time,
 * with its name next to it (owner, 2026-09-20: a bare switch in the audio bar
 * said nothing about what it switched) -- where there is a next file; and
 * "Compatible mode" where the transcoder passes the video through as it is
 * (passthrough.js): the viewer who sees a wrong picture no automatic check
 * catches (Dolby Vision without its metadata, HDR on an SDR screen) restarts
 * the file converted on our side. Each row only where it applies.
 */
export function SettingsControl({ autoplayNext, onToggleAutoplayNext, onCompatMode = null }) {
    const [open, setOpen] = useState(false);
    const rootRef = useRef(null);
    const btnRef = useRef(null);
    const menuRef = useRef(null);
    useAnchoredPopover(open, setOpen, rootRef, btnRef, menuRef);

    return (
        <div class="wt-player-settings" ref={rootRef}>
            <button type="button" ref={btnRef} class="wt-player-btn wt-player-btn--more" onClick={() => setOpen((v) => !v)}
                aria-label={t('player.settings')} title={t('player.settings')} aria-haspopup="menu" aria-expanded={open}>
                <MoreIcon />
            </button>
            <div ref={menuRef} popover={HAS_POPOVER ? 'manual' : undefined} role="menu" aria-label={t('player.settings')}
                class={`wt-player-menu${open ? ' wt-player-menu--open' : ''}`}>
                {onToggleAutoplayNext && (
                    <label class="wt-player-menu-row">
                        <span>{t('player.autoplayNext')}</span>
                        <input type="checkbox" role="switch" class="toggle toggle-soft toggle-sm" checked={autoplayNext} onChange={onToggleAutoplayNext} />
                    </label>
                )}
                {onCompatMode && (
                    <button type="button" role="menuitem" class="wt-player-menu-row wt-player-menu-row--compat"
                        title={t('player.compatModeHint')} onClick={() => { setOpen(false); onCompatMode(); }}>
                        <span>{t('player.compatMode')}</span>
                        <span class="wt-player-menu-hint">{t('player.compatModeHint')}</span>
                    </button>
                )}
            </div>
        </div>
    );
}
