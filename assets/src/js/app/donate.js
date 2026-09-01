import av from '../lib/av';

// One impression per full page load, same rationale as onboardingChecklist.js:
// async navigation re-runs the binding, and double-counting inflates the
// denominator for every click event on the page.
let donateShownReported = false;

// /donate + /donate/crypto/success bindings.
av(async function() {
    // Impression with the offers this render actually made (tier cards,
    // Patreon links, crypto links) — the denominator for donate-* clicks.
    const shown = this.querySelector('[data-donate-shown]');
    if (shown && !donateShownReported && window.umami) {
        donateShownReported = true;
        window.umami.track('donate-shown', {
            cards: Number(shown.dataset.donateCards || 0),
            patreon: shown.dataset.donatePatreon === 'true',
            crypto: shown.dataset.donateCrypto === 'true',
        });
    }
    // Success page: payment-watch progress log — the server job streams
    // status over SSE and emits a redirect once the payment reaches a
    // terminal state (handled by progressLog's built-in redirect support).
    const progress = this.querySelector('.progress-alert[data-async-progress-log]');
    if (progress) {
        const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
        initProgressLog(progress);
    }

    // Payment history: timestamps arrive as UTC (server has no idea of the
    // browser's timezone) — re-render them locally.
    for (const el of this.querySelectorAll('time[data-localize-datetime]')) {
        const d = new Date(el.getAttribute('datetime'));
        if (isNaN(d)) continue;
        el.textContent = new Intl.DateTimeFormat(document.documentElement.lang || undefined, {
            day: '2-digit', month: '2-digit', year: 'numeric',
            hour: '2-digit', minute: '2-digit',
        }).format(d);
    }
});

export {}
