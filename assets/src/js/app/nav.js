import av from '../lib/av';

av(async function() {
    if (window.umami) {
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
        // Re-identify so the now-authed tier/is_authed/user_id props refresh
        // in session_data under the same server session id (which stays stable
        // across the anon→authed transition).
        if (window.umami && window._sessionID) {
            window.umami.identify(window._sessionID);
        }
        self.reload();
    }, { once: true });
});

export {}

