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
