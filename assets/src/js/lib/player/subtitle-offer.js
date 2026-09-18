// The on-screen translation offer: a pill over the picture, shown when
// playback starts and the viewer's language has no subtitles. It is the
// picker's own offer moved to where the viewer is looking -- nothing here
// decides WHETHER a translation is on offer. The server does (helper.go:
// Offered for a viewer who can run it, Upsell for one who cannot), and the
// pill reads that answer off the chips.
//
// Two kinds, one slot:
//   start  -- the chip is Offered. A click is a click on the chip.
//   upsell -- the chip is Locked and marked Upsell. A click opens a small
//             card with the supporter CTA, "Not now" and "Don't offer".
//
// Only the upsell is remembered across pages: a viewer who cannot use the
// feature should be able to stop hearing about it. The start offer is an
// action the viewer can take, so closing it lasts for this page only.

import { offerNeedsHint } from './track-picker.js';

export const offerTiming = {
    // How long the pill stays up on its own after playback starts. After
    // that it is shown only together with the controls.
    lingerMs: 10000,
    // "×" and "Not now" on the upsell.
    snoozeMs: 30 * 24 * 60 * 60 * 1000,
};

export const OFFER_STORAGE_KEY = 'wt-subtitle-offer';

// pickOffer reads the offer off the picker's chips (readChips). The rule
// for both kinds is offerNeedsHint's: the language must hold nothing else
// the viewer could turn on, or "no subtitles in your language" is false.
export function pickOffer(chips) {
    const list = Array.isArray(chips) ? chips : [];
    const startId = offerNeedsHint(list);
    if (startId) {
        const c = list.find((o) => o && o.id === startId);
        return { kind: 'start', id: c.id, lang: c.lang, label: c.offerLabel || '' };
    }
    for (const c of list) {
        if (!c || !c.upsell || !c.locked) continue;
        const rival = list.some((o) => o && o !== c && o.id && o.id !== 'none' && !o.locked && o.lang === c.lang);
        if (!rival) return { kind: 'upsell', id: c.id, lang: c.lang, label: c.offerLabel || '' };
    }
    return null;
}

// upsellSuppressed / suppressUpsell keep the viewer's "not now" (30 days)
// and "don't offer" (for good). Storage that throws or is absent -- private
// mode, a sandboxed embed -- reads as "not suppressed" and writes nothing:
// the worst case is an offer shown again, never a player that breaks.
export function upsellSuppressed(storage, now) {
    try {
        const v = JSON.parse(storage.getItem(OFFER_STORAGE_KEY) || 'null');
        if (!v) return false;
        return v.never === true || (typeof v.until === 'number' && v.until > now);
    } catch (e) {
        return false;
    }
}

export function suppressUpsell(storage, now, { never = false } = {}) {
    try {
        storage.setItem(OFFER_STORAGE_KEY, JSON.stringify(never ? { never: true } : { until: now + offerTiming.snoozeMs }));
    } catch (e) { /* see above */ }
}

// offerVisible: the catch-up pill owns the slot while it is up (a
// translation is running, so there is nothing left to offer anyway); the
// card keeps its pill on screen while open.
export function offerVisible({ offer, lingering, controlsVisible, cardOpen, catchUp }) {
    if (!offer || catchUp) return false;
    return !!(lingering || controlsVisible || cardOpen);
}
