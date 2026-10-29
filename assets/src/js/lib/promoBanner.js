import { trackImpressions } from './impression';

const STORAGE_KEY = 'promo-dismissed';
const RESHOW_DAYS = 14;

export function initPromoBanner(banner) {
    const dismissed = localStorage.getItem(STORAGE_KEY);
    if (dismissed && (Date.now() - Number(dismissed)) < RESHOW_DAYS * 24 * 60 * 60 * 1000) {
        banner.style.display = 'none';
    } else {
        // promo-shown (data-umami-impression on the banner): the banner's
        // clicks (`promo`) had no denominator. A dismissed one is not shown.
        trackImpressions(banner);
    }
    const btn = banner.querySelector('[data-promo-dismiss]');
    if (btn) {
        btn.addEventListener('click', function(e) {
            e.preventDefault();
            banner.style.display = 'none';
            localStorage.setItem(STORAGE_KEY, String(Date.now()));
        });
    }
}
