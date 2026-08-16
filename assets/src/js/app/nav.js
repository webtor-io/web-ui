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
    // Nav quick-search dropzone: the label opens the picker natively (for=),
    // JS only submits the form once a file is chosen. Unlike the home-page
    // dropzone (lib/drop.js) there are no document-level drag/drop handlers —
    // hijacking every drop on every page is home-hero behavior, not nav's.
    const dropzone = this.querySelector('.nav-dropzone');
    if (dropzone) {
        dropzone.querySelector('.dropzone-input').addEventListener('change', function() {
            dropzone.requestSubmit();
        });
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

