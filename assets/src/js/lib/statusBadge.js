// The status badge's one renderer: the element partials/status/badge.html
// renders -- the resource page's transfer status while nothing moves
// (lib/transferStatus.js applyView) and every live row on the Vault page
// (app/vault/progress.js) -- updated in place from a statusview.Badge the
// server sends ({ tone, icon, pulse, label, extra }, already localized: the
// resource page's `view.badge`, the Vault dashboard's `badge`). Tone, icon
// and pulse are the element's attributes (style.css .tx-badge[data-tone],
// #tx-b-<icon> in the page's sprite), the words its two spans' text and
// their whole text the words' title (a badge the column cuts can still be
// read; the partial says why it is on the words). No node is ever added or
// removed: re-created with innerHTML every second, the badge restarted its
// pulse and dropped focus.

import { attr, text } from './inPlace';

// bindBadge finds the badge's slots once; cached on the element. null for
// no element.
export function bindBadge(el) {
    if (!el) return null;
    if (el._badge) return el._badge;
    el._badge = {
        el,
        use: el.querySelector('.tx-bi use'),
        words: el.querySelector('.badge-text'),
        label: el.querySelector('[data-tx-blabel]'),
        extra: el.querySelector('[data-tx-bextra]'),
    };
    return el._badge;
}

// badgeTitle is the badge's whole text, as the partial writes its title.
function badgeTitle(b) {
    const label = (b && b.label) || '';
    return b && b.extra ? `${label} ${b.extra}` : label;
}

// applyBadge writes a badge into a bound element, only what changed.
export function applyBadge(refs, b) {
    if (!refs || !b) return;
    attr(refs.el, 'data-tone', b.tone || '');
    attr(refs.el, 'data-icon', b.icon || '');
    attr(refs.el, 'data-pulse', !!b.pulse);
    if (b.icon) attr(refs.use, 'href', `#tx-b-${b.icon}`);
    text(refs.label, b.label || '');
    text(refs.extra, b.extra || '');
    attr(refs.words, 'title', badgeTitle(b));
}
