import av from '../lib/av';
import { trackImpressions } from '../lib/impression';

// Finishes what an anonymous visitor started: the banner's subscribe link
// sends them through /login with ?release_sub=1 on the return-url, and this
// submits the (now available) subscribe form for them. The param is stripped
// first so a reload does not subscribe twice; the server's unique constraint
// would refuse a duplicate anyway, but the user would see an error for it.
function resumeSubscribe(root) {
    const params = new URLSearchParams(window.location.search);
    if (params.get('release_sub') !== '1') return;
    const form = root.querySelector('form[data-release-sub-form]');
    params.delete('release_sub');
    const q = params.toString();
    history.replaceState(null, '', window.location.pathname + (q ? '?' + q : '') + window.location.hash);
    if (!form) return; // already subscribed, or not eligible: the banner says so itself
    if (window.umami) {
        window.umami.track('release-sub-created', { source: 'resource_banner', resumed: 1 });
    }
    form.requestSubmit();
}

// The banner is an async island (re-rendered on subscribe/unsubscribe), so
// this binding runs per render; trackImpressions dedupes per page load and
// resumeSubscribe strips its trigger on the first pass.
av(function() {
    trackImpressions(this);
    resumeSubscribe(this);
});

export {}
