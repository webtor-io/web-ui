import av from '../../lib/av';

// Fake-door experiment for release-level subscription. The server renders
// the banner only on resource pages of currently-airing series; this handler
// wires impression + click events into Umami and persists per-resource
// dismissal in localStorage so the banner stays gone across visits.
// No backend writes — the entire experiment lives in Umami. See
// docs/release_sub_fake_door.md for the decision gates and metrics plan.
av(function () {
    const el = document.getElementById('release-subscribe-banner');
    if (!el) return;

    const resourceId = el.dataset.resourceId || '';
    const dismissKey = 'release_sub_dismissed:' + resourceId;

    if (localStorage.getItem(dismissKey)) {
        el.remove();
        return;
    }

    const eventData = () => ({
        resource_id: resourceId,
        series_title: el.dataset.seriesTitle || '',
        series_video_id: el.dataset.seriesVideoId || '',
        season: parseInt(el.dataset.season || '0', 10) || 0,
        release_group_raw: el.dataset.releaseGroupRaw || '',
        is_anon: el.dataset.isAnon === '1' ? 1 : 0,
    });

    const track = (name) => {
        if (window.umami) window.umami.track(name, eventData());
    };

    // Impression once at least half the banner is on screen. Single-shot —
    // we want unique-viewer counts, not scroll-amplified noise.
    const io = new IntersectionObserver(
        (entries) => {
            for (const e of entries) {
                if (e.isIntersecting) {
                    track('release-subscribe-banner-shown');
                    io.disconnect();
                    return;
                }
            }
        },
        { threshold: 0.5 },
    );
    io.observe(el);

    const content = el.querySelector('[data-rsb-content]');
    const thanks = el.querySelector('[data-rsb-thanks]');

    const finishWithThanks = (eventName) => {
        track(eventName);
        localStorage.setItem(dismissKey, '1');
        if (content) content.classList.add('hidden');
        if (thanks) thanks.classList.remove('hidden');
        setTimeout(() => { el.remove(); }, 4000);
    };

    const yes = el.querySelector('[data-rsb-yes]');
    if (yes) yes.addEventListener('click', () => finishWithThanks('release-subscribe-banner-yes'));

    const no = el.querySelector('[data-rsb-no]');
    if (no) no.addEventListener('click', () => finishWithThanks('release-subscribe-banner-no'));

    const dismiss = el.querySelector('[data-rsb-dismiss]');
    if (dismiss) dismiss.addEventListener('click', () => {
        track('release-subscribe-banner-dismissed');
        localStorage.setItem(dismissKey, '1');
        el.remove();
    });

    const register = el.querySelector('[data-rsb-register]');
    if (register) register.addEventListener('click', () => {
        track('release-subscribe-banner-register-click');
    });
});
