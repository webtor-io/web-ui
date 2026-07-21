import av from '../lib/av';

// Binds the payment-watch progress log on /donate/crypto/success: the server
// job streams status over SSE and emits a redirect once the payment reaches a
// terminal state (handled by progressLog's built-in redirect support).
av(async function() {
    const progress = this.querySelector('.progress-alert[data-async-progress-log]');
    if (!progress) return;
    const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
    initProgressLog(progress);
});

export {}
