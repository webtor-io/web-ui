import av from '../../lib/av';
import { langPath } from '../../lib/i18n';
import { applyBadge, bindBadge } from '../../lib/statusBadge';
import { debugQuery } from '../../lib/statusDebug';

// The Vault page's live rows (views/vault/index.html "vault/pledges_table"):
// one status stream per pledge being vaulted -- the resource page's
// endpoint, without session=1, so the server sends the status and the badge
// alone (handlers/resource/status.go present) -- drawn into the row. The
// status cell is the transfer status's own badge (partials/status/badge.html),
// the server's words, colour and icon for every state, updated in place by
// its one renderer (lib/statusBadge.js); what is the Vault page's own stays
// here: the row's fill and percent, "Saved" for a vaulted torrent, the first
// cell's pulse, the token's renewal.

// rgba colors mirror the w-cyan / w-purple / green-500 tokens at low alpha
const TINTS = {
    caching:  'rgba(0, 206, 201, 0.10)',
    cached:   'rgba(0, 206, 201, 0.06)',
    vaulting: 'rgba(108, 92, 231, 0.12)',
    vault_waiting: 'rgba(108, 92, 231, 0.08)',
    vault_failed: 'rgba(250, 204, 21, 0.10)',
    vaulted:  'rgba(34, 197, 94, 0.08)',
    idle:     'rgba(0, 206, 201, 0.06)',
};

// Floor for caching/vaulting widths so 0–1 % progress is still visible.
const MIN_VISIBLE_PCT = 2;
// Below this fill width, the percent indicator can't fit inside the filled
// portion (translateX(-100%) would clip it off the left edge of the row), so
// it flips to the right side of the gradient edge instead.
const FLIP_PCT = 10;

function ensureIndicator(row) {
    let el = row.querySelector('[data-vault-progress-pct]');
    if (el) return el;
    const host = row.cells && row.cells[0];
    if (!host) return null;
    el = document.createElement('span');
    el.dataset.vaultProgressPct = '';
    el.className = 'vault-progress-pct hidden';
    host.appendChild(el);
    return el;
}

function applyRowFill(row, status) {
    const color = TINTS[status.state] || TINTS.idle;
    const rawPct = Math.round(status.progress || 0);
    let pct;
    let showPct = false;
    switch (status.state) {
        case 'caching':
        case 'vaulting':
            pct = Math.max(MIN_VISIBLE_PCT, rawPct);
            showPct = true;
            break;
        case 'cached':
            pct = 100;
            break;
        case 'vaulted':
        default:
            pct = 0;
            break;
    }
    row.style.backgroundImage = `linear-gradient(to right, ${color} ${pct}%, transparent ${pct}%)`;

    const indicator = ensureIndicator(row);
    if (!indicator) return;
    if (showPct) {
        indicator.textContent = `${rawPct}%`;
        indicator.style.left = `${pct}%`;
        // Below FLIP_PCT the fill is too narrow to host the indicator on its
        // left side (translateX(-100%) would clip past the row's left edge),
        // so flip it to the right side of the gradient edge.
        indicator.classList.toggle('vault-progress-pct--right', pct < FLIP_PCT);
        indicator.classList.remove('hidden');
    } else {
        indicator.classList.add('hidden');
    }
}

function settleVaultedIcon(row) {
    const icon = row.querySelector('[data-vault-progress-icon]');
    if (icon) icon.classList.remove('vault-pulse');
}

// rowBadge is the badge the row draws for a status: the server's, and for a
// vaulted torrent this page's word for a finished pledge ("Сохранён"/"Saved",
// data-vault-saved-label) in place of the resource page's ("В Vault") -- the
// badge the page renders for a pledge already vaulted, so a row that finishes
// looks as the finished ones do.
//
// A message without `badge` is a server from before it (a rolling deploy, a
// rollback). Vaulted is still the finished pledge's pill -- the stream closes
// on it, and nothing would ever repaint the row. Any other state leaves the
// badge as it is (undefined: applyBadge writes nothing): the row's fill and
// percent still move, and a badge made up here from `state` would be a
// tone/icon/words statusview never sends.
const SAVED = { tone: 'vault', icon: 'vault' };

function rowBadge(status, savedLabel) {
    const b = status.badge;
    if (status.state !== 'vaulted' || !savedLabel) return b;
    return { ...(b || SAVED), label: savedLabel };
}

// The status stream takes a page-issued, hash-bound token that lives an hour
// (handlers/resource/torrent_link.go); a vaulting is watched for longer. When
// a stream is refused, reload the table the async way: this.reload()
// (lib/async.js asyncLayout) re-fetches /vault with X-Layout
// "vault/pledges_table", swaps in rows with fresh tokens and re-runs this
// init. At most once per RELOAD_MIN_MS per view, so a dead stream never
// becomes a loop.
const RELOAD_MIN_MS = 60 * 1000;

function renew(root) {
    if (root._vaultProgressGone || typeof root.reload !== 'function') return;
    const last = root._vaultProgressReloadAt || 0;
    if (Date.now() - last < RELOAD_MIN_MS) return;
    root._vaultProgressReloadAt = Date.now();
    // loadAsyncView only destroys views *inside* the target; this view is the
    // target, so close our own streams before the swap re-inits it.
    closeAll(root);
    root.reload();
}

function closeAll(root) {
    if (root._vaultProgressSources) {
        root._vaultProgressSources.forEach((s) => s.close());
        root._vaultProgressSources = null;
    }
}

function attachRow(root, row) {
    const resourceId = row.dataset.resourceId;
    const csrf = row.dataset.csrf;
    if (!resourceId || !csrf) return null;

    const savedLabel = row.dataset.vaultSavedLabel || '';
    const badge = bindBadge(row.querySelector('[data-tx-badge]'));

    // Dev-only preview (lib/statusDebug.js): the page's debug_status/… ride
    // along, so every state can be looked at here too.
    let url = `${langPath(`/${resourceId}/status`)}?_csrf=${encodeURIComponent(csrf)}${debugQuery(window.location.search)}`;
    const statusToken = row.dataset.statusToken || '';
    if (statusToken) url += `&token=${encodeURIComponent(statusToken)}`;
    const source = new EventSource(url);

    source.onmessage = (e) => {
        let status;
        try {
            status = JSON.parse(e.data);
        } catch (err) {
            return;
        }
        applyRowFill(row, status);
        applyBadge(badge, rowBadge(status, savedLabel));
        if (status.state === 'vaulted') {
            settleVaultedIcon(row);
            source.close();
        }
    };
    source.onerror = () => {
        // A refused stream (403 — token expired) closes the EventSource for
        // good; network blips reconnect on their own with the same URL.
        if (statusToken && source.readyState === EventSource.CLOSED) renew(root);
    };

    return source;
}

av(async function () {
    const root = this;
    root._vaultProgressGone = false;
    const rows = root.querySelectorAll('[data-vault-progress]');
    if (!rows.length) return;

    const sources = [];
    rows.forEach((row) => {
        const s = attachRow(root, row);
        if (s) sources.push(s);
    });
    root._vaultProgressSources = sources;
}, function () {
    const root = this;
    root._vaultProgressGone = true;
    closeAll(root);
});

export {};
