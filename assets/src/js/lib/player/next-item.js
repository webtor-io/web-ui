// "What plays next", the player's side. The server says which file follows
// this one (data-next-* on the player element, jobs/scripts/next_item.go);
// everything here is pure -- what to read, what to send, when to act -- so it
// can be tested without a player. The orchestration (DOM, history, the
// background render) lives in next-item-go.js.
//
// Why it exists: 86% of viewers who finish an episode open the next one by
// hand, and that transition costs them about a minute (list, click, a cold
// warm-up). See docs/superpowers/specs/2026-09-20-next-episode-design.md.

export function readNext(el) {
    const d = el && el.dataset;
    if (!d || !d.nextItemId || !d.nextPath) return null;
    return { itemId: d.nextItemId, path: d.nextPath, kind: d.nextKind || '', label: d.nextLabel || '' };
}

// readCarry turns the picker's CURRENT selection into the carry-* form fields
// of the next start (models.TrackCarry). Track ids are per file, so what
// travels is the intent: a language, an origin, "off". Read from the chips
// and not from a saved value: the chips are what is playing right now.
export function readCarry(scope) {
    const out = {};
    if (!scope || typeof scope.querySelector !== 'function') return out;
    const audio = scope.querySelector('.audio[data-default="true"]');
    if (audio && audio.getAttribute('data-srclang')) {
        out['carry-audio-lang'] = audio.getAttribute('data-srclang');
        const label = audio.getAttribute('data-label');
        if (label) out['carry-audio-label'] = label;
    }
    const sub = scope.querySelector('.subtitle[data-default="true"]');
    if (sub) {
        if ((sub.getAttribute('data-id') || '') === 'none') {
            out['carry-sub'] = 'off';
        } else if (sub.getAttribute('data-srclang')) {
            out['carry-sub'] = 'on';
            out['carry-sub-lang'] = sub.getAttribute('data-srclang');
            const provider = sub.getAttribute('data-provider');
            if (provider) out['carry-sub-provider'] = provider;
        }
    }
    return out;
}

// When to get the next file ready, and when to put the card up.
//
// Prewarm at 90% -- the point past which 86% go on -- but never earlier than
// PREWARM_MAX_LEAD_S before the end: the prepared render and its transcoder
// session live about ten minutes, and 10% of a long episode is more than
// that. Only while the film is actually being watched.
export const PREWARM_AT = 0.9;
export const PREWARM_MAX_LEAD_S = 300;
export const CARD_LEAD_S = 25;
export const COUNTDOWN_S = 10;
// After this many automatic transitions in a row with no sign of a viewer,
// ask instead of playing on: a sleeper would otherwise warm up and transcode
// a season overnight.
export const STILL_WATCHING_AFTER = 3;

export function advancePlan({ currentTime, duration, playing, hidden, prewarmed, kind }) {
    const plan = { prewarm: false, card: false };
    if (!(duration > 0) || !(currentTime >= 0)) return plan;
    const remaining = duration - currentTime;
    const threshold = Math.max(duration * PREWARM_AT, duration - PREWARM_MAX_LEAD_S);
    plan.prewarm = !prewarmed && playing && !hidden && currentTime >= threshold && remaining > 0;
    // Music has no credits to sit through and no picture to cover: the next
    // track simply plays. The card is for video.
    plan.card = kind !== 'track' && remaining <= CARD_LEAD_S && remaining >= 0 && duration > CARD_LEAD_S * 2;
    return plan;
}

// atEnd decides what `ended` means.
export function atEnd({ autoplay, autoStreak, cancelled }) {
    if (cancelled) return 'stay';
    if (!autoplay) return 'offer';
    if (autoStreak >= STILL_WATCHING_AFTER) return 'ask';
    return 'go';
}

// resumeAt: an automatic transition never stops to ask "continue from 12:40?"
// -- it starts where the viewer left the next file, unless they had all but
// finished it, in which case from the top.
export function resumeAt(position, duration) {
    if (!(position > 0)) return 0;
    // An unknown duration cannot say "finished": resume, as a settings
    // restart always has (Player.wiring.test.js).
    if (!(duration > 0)) return position;
    return position / duration >= 0.9 ? 0 : position;
}

const STREAK_KEY = 'wt-next-auto-streak';

export function readStreak(storage) {
    try { return Math.max(0, parseInt(storage.getItem(STREAK_KEY) || '0', 10) || 0); } catch (e) { return 0; }
}

export function writeStreak(storage, n) {
    try { storage.setItem(STREAK_KEY, String(Math.max(0, n))); } catch (e) { /* no storage */ }
}
