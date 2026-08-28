// Where the refresh interstitial sends the browser once the SDK's refresh
// attempt settles. attemptRefreshingSession() resolves false — it does not
// throw, and makes no network call — when the SDK's local session state says
// no session exists while the server still holds an expired access-token
// cookie it cannot resolve. Reloading in that state renders this same
// interstitial again, forever; the only exit is a fresh login, carrying the
// original destination as return-url the way every other login entry point
// does.
//
// A successful refresh answers null — reload in place — rather than the
// current href: location.replace() of a URL that equals the current one and
// carries a fragment (e.g. /profile#subscriptions out of a subscription
// email) is a same-document fragment navigation per the HTML spec, so the
// page is never re-requested and the blank interstitial stays up even though
// the session was refreshed. location.reload() re-requests unconditionally
// and keeps the whole URL, fragment included.
export function nextLocation(refreshed, {pathname, search}) {
    if (refreshed) {
        return null;
    }
    return '/login?return-url=' + encodeURIComponent(pathname + search);
}
