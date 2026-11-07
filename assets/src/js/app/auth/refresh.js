// Only interstitials rendered before 2026-11 load this: the refresh now runs
// from auth.js (views/auth/refresh.html has no script of its own). Kept for
// one release so a page that is open while web-ui rolls out does not ask a
// new pod for a script it no longer has and stay blank. Delete with the next
// release.
import av from '../../lib/av';
import {settleRefresh} from '../../lib/auth/nextLocation.js';
av(async function() {
    const {next, error} = await settleRefresh(async () => {
        const {refresh} = await import('../../lib/supertokens');
        return refresh(window._CSRF);
    }, window.location);
    if (error) {
        // The refresh did not answer: an outage, not a missing session. Say
        // so instead of sending a signed-in visitor to the login form.
        console.error(error);
        const failed = document.getElementById('auth-refresh-failed');
        failed?.classList.remove('hidden');
        failed?.classList.add('flex');
        return;
    }
    if (next === null) {
        window.location.reload();
    } else {
        window.location.replace(next);
    }
});

export {}
