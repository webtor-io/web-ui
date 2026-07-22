import av from '../lib/av';

// /donate + /donate/crypto/success bindings.
av(async function() {
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

    // Membership page: unavailable monthly plans render a disabled-looking
    // button that explains itself in a modal instead of submitting.
    const modal = this.querySelector('#plan-unavailable-modal');
    if (modal) {
        for (const btn of this.querySelectorAll('[data-plan-unavailable]')) {
            btn.addEventListener('click', (e) => {
                e.preventDefault();
                modal.showModal();
            });
        }
    }
});

export {}
