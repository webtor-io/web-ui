import av from '../lib/av';
av(async function() {
    if (window.umami) {
        if (window._userId) window.umami.identify(window._userId);
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

