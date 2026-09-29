// web-ui's CSRF middleware (handlers/session) answers 400 with this body when
// the page's token does not belong to the session the request carries: the
// session cookie was dropped, expired or failed its signature check. The SDK
// throws the raw Response for any status >= 300, and over HTTP/2 its
// statusText is empty, so without this check the viewer saw "unknown error".
const CSRF_MISMATCH_BODY = 'CSRF token mismatch';

const RELOAD_MARK = 'auth.csrfReloadAt';
const RELOAD_WINDOW_MS = 60 * 1000;

export async function isCsrfMismatch(err) {
    if (!err || err.status !== 400 || typeof err.clone !== 'function') return false;
    try {
        return (await err.clone().text()).trim() === CSRF_MISMATCH_BODY;
    } catch {
        return false;
    }
}

// A reloaded page brings a fresh session and token, so one reload is the fix.
// A second mismatch within the window means reloading does not help, and the
// viewer gets a message instead of a reload loop. Without storage there is no
// way to tell the second time from the first, so no reload either.
export function claimReload(storage, now) {
    try {
        const last = Number(storage.getItem(RELOAD_MARK));
        if (last && now - last < RELOAD_WINDOW_MS) return false;
        storage.setItem(RELOAD_MARK, String(now));
        return true;
    } catch {
        return false;
    }
}

// Returns the message for the progress entry and whether the page is about to
// reload. env is injectable for tests.
export async function describeAuthError(err, {t, tf}, env = {}) {
    const {
        storage = globalThis.sessionStorage,
        now = Date.now(),
        reload = () => globalThis.location.reload(),
        reloadDelayMs = 1500,
    } = env;
    if (await isCsrfMismatch(err)) {
        if (claimReload(storage, now)) {
            setTimeout(reload, reloadDelayMs);
            return {message: t('auth.progress.sessionExpiredReloading'), reloading: true};
        }
        return {message: t('auth.progress.sessionExpired'), reloading: false};
    }
    if (err?.statusText) return {message: err.statusText.toLowerCase(), reloading: false};
    if (err?.message) return {message: err.message.toLowerCase(), reloading: false};
    if (err?.status) return {message: tf('auth.progress.requestFailed', err.status), reloading: false};
    return {message: t('auth.progress.unknownError'), reloading: false};
}
