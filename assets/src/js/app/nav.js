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
        self.reload();
    }, { once: true });
});

export {}

