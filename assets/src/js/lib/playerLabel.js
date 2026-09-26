// The channel between the transfer status and the page's player: whether a
// stall now is the plan's cap, and what the player's buffering label says
// about it (lib/transferStatus.js playerLabel builds it, lib/player/
// Player.jsx draws it -- docs/player.md "Buffering label").
//
// Two bundles: the status view is app/resource/status.js, the player lives in
// the stream action's chunk, and a module imported by both is two copies with
// two module scopes (CLAUDE.md, shared JS state). So the label lives on
// window, and the news of a change is an event on the document -- a player
// mounted after the status has spoken reads the label as it stands.

const KEY = '_txPlayerLabel';
export const PLAYER_LABEL_EVENT = 'tx-player-label';

// publishPlayerLabel sets the label (null for none) and tells the page, only
// when it changed: the status draws every second while a plan is up, and a
// player re-rendering every second for the same label would be waste.
// Returns whether it changed.
export function publishPlayerLabel(label, win = window) {
    const next = label || null;
    const was = win[KEY] || null;
    if (JSON.stringify(was) === JSON.stringify(next)) return false;
    win[KEY] = next;
    win.document.dispatchEvent(new win.CustomEvent(PLAYER_LABEL_EVENT, { detail: next }));
    return true;
}

// currentPlayerLabel is the label as it stands (null for none).
export function currentPlayerLabel(win = window) {
    return (win && win[KEY]) || null;
}

// onPlayerLabel calls fn with every new label; returns the unsubscribe.
export function onPlayerLabel(fn, win = window) {
    const handler = (e) => fn((e && e.detail) || null);
    win.document.addEventListener(PLAYER_LABEL_EVENT, handler);
    return () => win.document.removeEventListener(PLAYER_LABEL_EVENT, handler);
}
