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
