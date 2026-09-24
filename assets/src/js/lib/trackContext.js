// Shared helpers for Umami event/session context. Exposes flags that should
// stick to all events/identify-calls in the current session (referral
// provenance, tier, auth state, language).

export function isReferralVisit() {
    // Persist the referral flag in localStorage (not sessionStorage) so a share
    // link visit survives tab-close → return → Patreon-OAuth → paid. Otherwise
    // the flag is wiped before the conversion event and we lose attribution.
    try {
        const ls = window.localStorage;
        const cached = ls.getItem('webtor.is_referral');
        if (cached !== null) return cached === '1' ? 1 : 0;
        const params = new URLSearchParams(window.location.search);
        const isRef = params.get('utm_campaign') === 'resource_share' ? 1 : 0;
        ls.setItem('webtor.is_referral', isRef ? '1' : '0');
        return isRef;
    } catch (e) {
        return 0;
    }
}

export function eventDefaults() {
    return {
        tier: window._tier || 'anon',
        is_authed: window._userId ? 1 : 0,
        user_id: window._userId || '',
        is_referral: isReferralVisit(),
        lang: document.documentElement.lang || 'en',
    };
}

const FIRST_TOUCH_KEY = 'webtor.first_touch';
const MAX_VALUE = 64;

function isOwnHost(host) {
    return host === 'webtor.io' || host.endsWith('.webtor.io');
}

// computeFirstTouch describes one page load as a first touch (docs/analytics.md):
//   ft_source  — utm_source, else the referring host without "www.",
//                "self" for webtor.io itself (a Cloudflare challenge in front
//                of the page leaves the page as its own referrer), "direct"
//                for none;
//   ft_medium  — utm_medium, or "";
//   ft_path    — the landing path, a torrent's info hash as ":hash";
//   ft_day     — the date it was recorded, YYYY-MM-DD.
// Values are cut to 64 characters: they become Umami session data.
export function computeFirstTouch(href, referrer, now) {
    const cut = s => (s || '').slice(0, MAX_VALUE);
    let url;
    try {
        url = new URL(href);
    } catch (e) {
        return null;
    }
    const q = url.searchParams;
    let source = q.get('utm_source') || '';
    if (!source) {
        let host = '';
        try {
            host = referrer ? new URL(referrer).hostname.toLowerCase().replace(/\.$/, '') : '';
        } catch (e) {
            host = '';
        }
        if (!host) source = 'direct';
        else if (isOwnHost(host)) source = 'self';
        else source = host.replace(/^www\./, '');
    }
    return {
        ft_source: cut(source),
        ft_medium: cut(q.get('utm_medium') || ''),
        ft_path: cut(url.pathname.replace(/[0-9a-fA-F]{40}/g, ':hash')),
        ft_day: now.toISOString().slice(0, 10),
    };
}

// firstTouch is where this browser first came to Webtor from, recorded on the
// first page load that runs analytics and never overwritten. It rides on
// umami.identify as session data, so a conversion on a later visit (search →
// leave → come back directly → pay) can still be put down to the channel that
// brought the visitor. Browsers that were here before it shipped (2026-09-24)
// record their next visit instead, and ft_day cannot tell those apart: every
// record starts on or after that day.
export function firstTouch(win = window, now = new Date()) {
    try {
        const ls = win.localStorage;
        const cached = ls.getItem(FIRST_TOUCH_KEY);
        if (cached) {
            let ft = null;
            try {
                ft = JSON.parse(cached);
            } catch (e) {
                ft = null; // a damaged record is recorded afresh
            }
            if (ft && typeof ft === 'object' && ft.ft_source) return ft;
        }
        const ft = computeFirstTouch(win.location.href, win.document.referrer, now);
        if (!ft) return {};
        ls.setItem(FIRST_TOUCH_KEY, JSON.stringify(ft));
        return ft;
    } catch (e) {
        return {};
    }
}
