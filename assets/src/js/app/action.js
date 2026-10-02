import av from '../lib/av';
av(async function() {
    const self = this;
    if (self._actionTeardown) self._actionTeardown();
    const progress = self.querySelector('[data-async-progress-log]');
    if (!progress) return;
    const el = document.createElement('div');
    let gone = false;
    let rendered = false;
    let sdk;
    const changed = () => document.dispatchEvent(new CustomEvent('transfer-activity'));
    const endPreparation = () => {
        delete progress.dataset.transferPreparing;
        changed();
    };
    const unlisten = () => {
        window.removeEventListener('player_ready', reveal);
        window.removeEventListener('player_show', reveal);
    };
    const reveal = () => {
        unlisten();
        progress.classList.add('hidden');
        el.classList.remove('hidden');
        endPreparation();
    };
    const onClose = (ev) => {
        if (ev.target.closest('.closeable-close')) endPreparation();
    };
    self._actionTeardown = () => {
        gone = true;
        unlisten();
        progress.removeEventListener('click', onClose);
        if (sdk) sdk.destroy();
        endPreparation();
    };
    progress.dataset.transferPreparing = '';
    progress.addEventListener('click', onClose);
    changed();
    const initProgressLog = (await import('../lib/progressLog')).initProgressLog;
    if (gone) return;
    sdk = initProgressLog(progress, function(ev) {
        if (gone) return;
        if (ev.level === 'error' || ev.level === 'download' || ev.level === 'redirect' || (ev.level === 'close' && !rendered)) {
            endPreparation();
        }
        if (ev.level !== 'rendertemplate') return;
        rendered = true;
        // A finished job is not yet a ready player. Keep preparation until
        // canplay, or a player card that needs to be shown (player_show).
        window.addEventListener('player_ready', reveal);
        window.addEventListener('player_show', reveal);
        el.classList.add('hidden', 'mb-5');
        self.appendChild(el);
        ev.render(el);
        // Download cards and image previews have no player readiness event.
        if (!el.querySelector('video, audio')) reveal();
    });
}, function() {
    if (this._actionTeardown) {
        this._actionTeardown();
        this._actionTeardown = null;
    }
});

export {};
