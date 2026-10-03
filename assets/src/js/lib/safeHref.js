// Only our own paths and https links become an href. The URL is the server's
// (a /trial or /donate path, or the plan's checkout from the catalog), but a
// javascript: URL must never reach an anchor, whoever sent it. A module of
// its own: the transfer status (lib/transferStatus.js) and the player's
// buffering label (lib/player/buffering-label.js) both need it, and the
// player's chunk must not bring the status view in. A pure function: safe
// to have twice in two bundles (CLAUDE.md, shared JS state).
export function safeHref(url) {
    if (typeof url !== 'string' || !url) return '';
    if (url.startsWith('/') && !url.startsWith('//')) return url;
    try {
        return new URL(url).protocol === 'https:' ? url : '';
    } catch (e) {
        return '';
    }
}
