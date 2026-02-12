const STORAGE_KEY = 'promo-dismissed';
const RESHOW_DAYS = 14;

export function initPromoBanner(banner) {
    const dismissed = localStorage.getItem(STORAGE_KEY);
    if (dismissed && (Date.now() - Number(dismissed)) < RESHOW_DAYS * 24 * 60 * 60 * 1000) {
        banner.style.display = 'none';
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