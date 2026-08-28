// Where the refresh interstitial sends the browser once the SDK's refresh
// attempt settles. attemptRefreshingSession() resolves false — it does not
// throw, and makes no network call — when the SDK's local session state says
// no session exists while the server still holds an expired access-token
// cookie it cannot resolve. Reloading in that state renders this same
// interstitial again, forever; the only exit is a fresh login, carrying the
// original destination as return-url the way every other login entry point
// does.
export function nextLocation(refreshed, {href, pathname, search}) {
    if (refreshed) {
        return href;
    }
    return '/login?return-url=' + encodeURIComponent(pathname + search);
}
