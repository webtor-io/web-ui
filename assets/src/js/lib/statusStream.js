// The life of the status streams a view keeps open (the resource page's one,
// app/resource/status.js; one per live row of /vault, app/vault/progress.js)
// -- renewing them when they are refused, giving up out loud when renewing
// does not help, letting them go while nobody looks. The streams and what
// they draw are the view's: it hands over how to open and close them.
//
// Refused (the EventSource CLOSED: a 403 -- the hour-long status token has
// expired, or the CSRF pair no longer matches the session cookie): the view
// is renewed the async way, host.reload() (lib/async.js asyncLayout: the
// page URL with the view's X-Layout, a fresh block with a fresh token and
// CSRF, the view's init again). Same URL, so the edge's challenge clearance a
// person already holds lets it through, and a client that never loaded the
// page stops right here. At most once per RELOAD_MIN_MS: a refusal sooner
// waits for the rest of the minute (and a few seconds of jitter -- the pages
// a rollout cut off do not all come back in the same millisecond). It used to
// be dropped there, and the status stood still for good (129 of 4218 page
// views in 6 h, 2026-10-03). RELOAD_TRIES renewals in a row with no message
// after them: the stream is not coming back, dead() -- the view says so
// rather than freezing (CLAUDE.md: a missing capability is explained).
//
// Hidden for HIDDEN_CLOSE_MS (a tab left in the background): close(). The
// server keeps a stream's loop, its Vault polling and its seeder and proxy
// subscriptions for as long as it is open, and streams of an hour or more
// were 73% of all stream time (3 h of logs, 2026-10-03). Visible again:
// open() -- or a renewal, where the token it would open with has expired.
//
// The count and the minute are on the host element: it outlives the view's
// re-init (CLAUDE.md: shared JS state lives in the DOM).
export const RELOAD_MIN_MS = 60 * 1000;
export const RELOAD_TRIES = 2;
export const JITTER_MS = 5 * 1000;
export const HIDDEN_CLOSE_MS = 60 * 1000;
// handlers/resource/torrent_link.go statusTokenTTL: rendered with the block,
// so as old as this view's init.
export const TOKEN_TTL_MS = 60 * 60 * 1000;

// watchStatusStream wires a view's streams on host:
//   open()      (re)opens them
//   close()     closes them for a while (a hidden tab)
//   teardown()  the view's own teardown before host.reload() swaps it
//   dead()      renewing does not help: the view says so
// Returns { refused, spoke, stop }: the view calls refused() for a stream
// the browser has CLOSED, spoke() for every message, stop() in its teardown.
// Renewing and giving up stop it themselves; refused() after that is a
// no-op (the other rows' streams of /vault, refused in the same second).
export function watchStatusStream(host, { open, close, teardown, dead, doc = document }) {
    host._streamTokenAt = Date.now();
    let retry = null;
    let hiddenTimer = null;
    let asleep = false;
    let stopped = false;
    const onVisibility = () => {
        if (doc.visibilityState === 'hidden') {
            if (!asleep && !hiddenTimer) {
                hiddenTimer = setTimeout(() => {
                    hiddenTimer = null;
                    asleep = true;
                    close();
                }, HIDDEN_CLOSE_MS);
            }
            return;
        }
        clearTimeout(hiddenTimer);
        hiddenTimer = null;
        if (!asleep) return;
        asleep = false;
        if (Date.now() - host._streamTokenAt >= TOKEN_TTL_MS) renew();
        else open();
    };
    const stop = () => {
        stopped = true;
        clearTimeout(retry);
        clearTimeout(hiddenTimer);
        doc.removeEventListener('visibilitychange', onVisibility);
    };
    const renew = () => {
        stop();
        host._streamTries = (host._streamTries || 0) + 1;
        host._streamReloadAt = Date.now();
        teardown();
        host.reload();
    };
    const refused = () => {
        if (stopped || retry) return;
        if (typeof host.reload !== 'function' || (host._streamTries || 0) >= RELOAD_TRIES) {
            stop();
            dead();
            return;
        }
        const wait = (host._streamReloadAt || 0) + RELOAD_MIN_MS - Date.now();
        if (wait <= 0) renew();
        else retry = setTimeout(renew, wait + Math.random() * JITTER_MS);
    };
    doc.addEventListener('visibilitychange', onVisibility);
    onVisibility();
    return {
        refused,
        spoke() {
            host._streamTries = 0;
        },
        stop,
    };
}
