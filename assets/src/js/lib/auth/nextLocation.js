import {claimReload} from './errors.js';

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
//
// The login form says why it is there (from=session-ended), and return-url
// keeps the fragment too: the visitor asked for this page, not a session.
export function nextLocation(refreshed, loc) {
    if (refreshed) {
        return null;
    }
    return loginLocation(loc);
}

export function loginLocation({pathname, search, hash = ''}) {
    return '/login?return-url=' + encodeURIComponent(pathname + search + hash) + '&from=session-ended';
}

// Runs the refresh attempt and settles where the interstitial goes next.
// attemptRefreshingSession() rejects -- rather than resolving false -- when the
// refresh request failed while the SDK still holds the session: an answer
// >= 300 other than 401, or none at all (supertokens-website 20.1.5 fetch.js,
// onUnauthorisedResponse -> API_ERROR). A 500 there is a SuperTokens core or
// Postgres that does not answer; the session may be perfectly valid. That is
// an outage, not "no session", so it settles on {error} and never on the
// login form, which only a resolved false leads to.
export async function settleRefresh(attempt, loc) {
    let refreshed;
    try {
        refreshed = await attempt();
    } catch (error) {
        return {error};
    }
    return {next: nextLocation(refreshed, loc)};
}

const RETRY_MARK = 'auth.refreshRetryAt';
const RELOADED_MARK = 'auth.refreshReloadAt';

// What the interstitial does once the refresh settles; env is injectable for
// tests ({location, storage, now, failed}).
//
// - No answer (outage, network, or a CSRF mismatch once the gin session
//   behind the page expired): reload, at most once a minute. A reload brings
//   a fresh CSRF token and outlasts a blip; past that, failed() says so
//   instead of looping.
// - No session: the login form, back to this page.
// - Refreshed: reload into the page. A second success in this tab within a
//   minute means the reload came straight back here -- the new cookies were
//   not stored -- and reloading again would loop. Without storage the reload
//   still happens: it is the only way to show the refreshed page.
export async function resumeRefresh(attempt, {location, storage, now, failed}) {
    const {next, error} = await settleRefresh(attempt, location);
    if (error) {
        if (claimReload(storage, now, RETRY_MARK)) location.reload();
        else failed(error);
        return;
    }
    if (next !== null) {
        location.replace(next);
        return;
    }
    if (claimReload(storage, now, RELOADED_MARK, true)) location.reload();
    else failed(new Error('refreshed twice within a minute: the session cookies are not being stored'));
}
