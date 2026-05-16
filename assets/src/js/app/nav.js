import av from '../lib/av';
import { eventDefaults } from '../lib/trackContext';

av(async function() {
    if (window.umami) {
        const sessionData = eventDefaults();
        if (window._userId) {
            window.umami.identify(window._userId, sessionData);
        } else {
            window.umami.identify(sessionData);
        }
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
        // Re-identify the Umami session after OAuth/magic-link success so the
        // session-data row carries the authed user's distinct_id (and the
        // up-to-date tier/is_authed props). Without this, paid-tier sessions
        // stay attributed to the pre-login anonymous snapshot and cross-session
        // first-touch analysis cannot resolve them by user.
        if (window._userId && window.umami) {
            window.umami.identify(window._userId, eventDefaults());
        }
        self.reload();
    }, { once: true });
});

export {}

