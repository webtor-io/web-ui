// The channel between the transfer status and the page's player: whether a
// stall now is the plan's cap, and what the player's buffering label says
// about it (lib/transferStatus.js playerLabel builds it, lib/player/
// Player.jsx draws it -- docs/player.md "Buffering label"). With the label,
// the cause the status names when a wait is NOT the cap's (lib/
// transferStatus.js playerCause: 'swarm', 'noseed', 'stalled', ...; '' for
// none) -- the player's own lock after its grace answer gives way to it.
//
// Two bundles: the status view is app/resource/status.js, the player lives in
// the stream action's chunk, and a module imported by both is two copies with
// two module scopes (CLAUDE.md, shared JS state). So the label lives on
// window, and the news of a change is an event on the document -- a player
// mounted after the status has spoken reads the label as it stands.

const KEY = '_txPlayerLabel';
const CAUSE = '_txPlayerCause';
export const PLAYER_LABEL_EVENT = 'tx-player-label';

// publishPlayerLabel sets the label (null for none) and the cause (''
// for none) and tells the page, only when either changed: the status draws
// every second while a plan is up, and a player re-rendering every second
// for the same word would be waste. Returns whether it changed.
export function publishPlayerLabel(label, win = window, cause = '') {
    const next = label || null;
    const nextCause = cause || '';
    const was = win[KEY] || null;
    if ((win[CAUSE] || '') === nextCause && JSON.stringify(was) === JSON.stringify(next)) return false;
    win[KEY] = next;
    win[CAUSE] = nextCause;
    win.document.dispatchEvent(new win.CustomEvent(PLAYER_LABEL_EVENT, { detail: next }));
    return true;
}

// currentPlayerLabel is the label as it stands (null for none).
export function currentPlayerLabel(win = window) {
    return (win && win[KEY]) || null;
}

// currentPlayerCause is the cause as it stands ('' for none).
export function currentPlayerCause(win = window) {
    return (win && win[CAUSE]) || '';
}

// onPlayerLabel calls fn(label, cause) with every new word; returns the
// unsubscribe.
export function onPlayerLabel(fn, win = window) {
    const handler = (e) => fn((e && e.detail) || null, currentPlayerCause(win));
    win.document.addEventListener(PLAYER_LABEL_EVENT, handler);
    return () => win.document.removeEventListener(PLAYER_LABEL_EVENT, handler);
}
