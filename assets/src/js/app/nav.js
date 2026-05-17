import av from '../lib/av';
import { eventDefaults } from '../lib/trackContext';

function identifySession() {
    if (!window.umami || !window._sessionID) return;
    // Always use the server-side session cookie ID as the Umami distinct_id —
    // for anons too. The cookie is HttpOnly, MaxAge=30d, and is created on the
    // very first request, so every session of the same browser carries the same
    // distinct_id from anonymous first-touch through auth → Patreon → return.
    // window._userId rides along as a property for cross-reference with
    // SuperTokens; tier/is_authed stay live via eventDefaults().
    window.umami.identify(window._sessionID, eventDefaults());
}

av(async function() {
    if (window.umami) {
        identifySession();
        if (window._isNewUser) {
            window.umami.track('signup');
        }
        if (window._tierUpdated && window._tier !== 'free') {
            window.umami.track('subscription-started', {
                tier: window._tier,
            });
        }
    }
    const self = this;
    window.addEventListener('auth', function() {
        // Re-identify so the now-authed tier/is_authed/user_id props land in
        // session_data under the same server session id. distinct_id stays
        // stable across the anon→authed transition.
        identifySession();
        self.reload();
    }, { once: true });
});

export {}

