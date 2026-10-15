import av from '../lib/av';
import { clearBusy } from '../lib/actionBusy';
import { reveal as bringIntoView, stillAtPress } from '../lib/stickyStatus';

// The button that started the job stays busy until the player is shown, not
// until its template is rendered (lib/progressLog.js): in between the player
// buffers out of sight, and a press there threw it away and replayed the job.
// A player that never says it can play (or has a card to show) must not hold
// the button for BUSY_MAX_MS: this releases it anyway.
const REVEAL_BACKSTOP_MS = 30 * 1000;
av(async function() {
    const self = this;
    if (self._actionTeardown) self._actionTeardown();
    const progress = self.querySelector('[data-async-progress-log]');
    // No job log: the request was refused (action/post's card). Into view,
    // like everything else a press produces (lib/stickyStatus.js reveal).
    if (!progress) { bringIntoView(self.firstElementChild); return; }
    const el = document.createElement('div');
    let gone = false;
    let rendered = false;
    let backstop = null;
    // The forms this job answers to: busy for this log when its response came
    // in (the pressed one, and the page's start form a card's retry marks).
    // Released by this job only while still busy from that press -- a newer
    // press into the same log (Download after Watch) is someone else's.
    const owners = Array.from(document.querySelectorAll('form[data-busy="true"]'))
        .filter((f) => f.getAttribute('data-async-target') === `#${self.id}`)
        .map((f) => [f, f.dataset.busySince]);
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
    const release = () => {
        clearTimeout(backstop);
        backstop = null;
        // Only while this job's log is still on the page: a new press into
        // the same #log replaces it (Turnstile's step) before this view is
        // torn down.
        if (!progress.isConnected) return;
        for (const [f, since] of owners) if (f.dataset.busySince === since) clearBusy(f);
    };
    const reveal = () => {
        unlisten();
        progress.classList.add('hidden');
        el.classList.remove('hidden');
        endPreparation();
        release();
        bringIntoView(el);
    };
    const onClose = (ev) => {
        if (ev.target.closest('.closeable-close')) endPreparation();
    };
    self._actionTeardown = () => {
        gone = true;
        unlisten();
        clearTimeout(backstop);
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
        // The log renders under the buttons, below the fold on most screens
        // (2026-10-14): each new step, card or verdict is brought into view --
        // never the per-second status ticks.
        if (ev.level === 'inprogress' || ev.level === 'custom' || ev.level === 'error' || ev.level === 'warn' || (ev.level === 'done' && ev.message)) {
            bringIntoView(progress);
        }
        // The download card: its "start download" is what the viewer came
        // for, and the link needs a second click -- focused, so Enter starts
        // it and a screen reader says where they are. Only while the viewer
        // is still at the press: not after they scrolled away, and not out of
        // a field they are typing in (their Enter would start the download).
        if (ev.level === 'custom') {
            const start = progress.querySelector('[data-transfer-download]');
            const ae = document.activeElement;
            const dialog = self.closest('dialog');
            const here = !ae || ae === document.body || self.contains(ae)
                || (ae.form && ae.form.getAttribute('data-async-target') === `#${self.id}`)
                || (dialog && dialog.contains(ae)); // TAR/ZIP: the dialog's ✕ had it
            if (start && here && stillAtPress(window)) start.focus({ preventScroll: true });
        }
        if (ev.level !== 'rendertemplate') return;
        rendered = true;
        // A finished job is not yet a ready player. Keep preparation -- and
        // the busy button -- until canplay, or a player card that needs to be
        // shown (player_show).
        window.addEventListener('player_ready', reveal);
        window.addEventListener('player_show', reveal);
        clearTimeout(backstop);
        backstop = setTimeout(release, REVEAL_BACKSTOP_MS);
        el.classList.add('hidden', 'mb-5');
        self.appendChild(el);
        ev.render(el);
        // Download cards and image previews have no player readiness event.
        // An image has no height until it loads: brought into view then.
        if (!el.querySelector('video, audio')) {
            reveal();
            const img = el.querySelector('img');
            if (img && !img.complete) img.addEventListener('load', () => bringIntoView(el), { once: true });
        }
    });
}, function() {
    if (this._actionTeardown) {
        this._actionTeardown();
        this._actionTeardown = null;
    }
});

export {};
