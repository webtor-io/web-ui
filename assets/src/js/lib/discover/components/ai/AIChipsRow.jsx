import { useState } from 'preact/hooks';
import { tf } from '../../i18n';

// AI suggestion chips shown above the free-form query input.
// Clicking a chip sends its pre-expanded `query` string to the recommender.
//
// Mobile: shows the first 2 chips at full width (full label, no truncate)
// plus a "+N more" expander that reveals the rest. This keeps the AI
// section short enough that the catalog grid stays in view, while still
// letting curious users see every suggestion. Desktop unchanged: flex-wrap
// row sized to content.

const MOBILE_INITIAL = 2;

export function AIChipsRow({ chips, onSelect, disabled }) {
    const [expanded, setExpanded] = useState(false);
    if (!chips || chips.length === 0) return null;

    const hiddenCount = Math.max(0, chips.length - MOBILE_INITIAL);

    return (
        <div class="grid grid-cols-1 gap-2 mt-3 sm:flex sm:flex-wrap">
            {chips.map((chip, i) => {
                // Past the initial window we tag the chip as mobile-hidden.
                // On desktop the .sm:inline-flex override always wins, so
                // expand state only matters for the narrow viewport.
                const hideOnMobile = !expanded && i >= MOBILE_INITIAL;
                return (
                    <button
                        key={chip.id}
                        type="button"
                        disabled={disabled}
                        onClick={() => onSelect(chip)}
                        class={`items-center gap-1.5 min-w-0 rounded-full bg-w-cyan/10 text-w-cyan border border-w-cyan/30 hover:bg-w-cyan/20 hover:border-w-cyan/60 transition-colors px-3.5 py-1.5 text-sm font-medium cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${hideOnMobile ? 'hidden sm:inline-flex' : 'inline-flex'}`}
                        title={chip.query}
                    >
                        {chip.icon && <span class="text-base leading-none shrink-0">{chip.icon}</span>}
                        <span>{chip.label}</span>
                    </button>
                );
            })}
            {!expanded && hiddenCount > 0 && (
                <button
                    type="button"
                    onClick={() => setExpanded(true)}
                    class="sm:hidden inline-flex items-center justify-center gap-1 rounded-full bg-w-surface/40 text-w-sub border border-w-line hover:border-w-cyan/30 hover:text-w-cyan transition-colors px-3.5 py-1.5 text-xs cursor-pointer"
                >
                    <span>{tf('discover.ai.moreChips', hiddenCount)}</span>
                    <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="6 9 12 15 18 9" />
                    </svg>
                </button>
            )}
        </div>
    );
}
