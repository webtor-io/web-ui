import av from '../lib/av';

// Initialises the log host rendered by partials/load/progress.html — on the
// home page and whenever a retry re-renders the host into #log-load.
av(async function() {
    const progress = this.querySelector('.progress-alert');
    if (progress == null) return;
    const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
    initProgressLog(progress);
});

export {};
