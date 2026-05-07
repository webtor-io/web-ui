import av from '../../lib/av';
import { langPath } from '../../lib/i18n';

// rgba colors mirror the w-cyan / w-purple / green-500 tokens at low alpha
const TINTS = {
    caching:  'rgba(0, 206, 201, 0.10)',
    cached:   'rgba(0, 206, 201, 0.06)',
    vaulting: 'rgba(108, 92, 231, 0.12)',
    vaulted:  'rgba(34, 197, 94, 0.08)',
    idle:     'rgba(0, 206, 201, 0.06)',
};

// Floor for caching/vaulting widths so 0–1 % progress is still visible.
const MIN_VISIBLE_PCT = 2;

const BADGE_CONFIG = {
    idle: {
        classes: 'badge badge-sm bg-base-200/50 border-w-line/30 text-w-muted gap-1.5',
        icon: '<span class="loading loading-dots loading-xs"></span>',
    },
    caching: {
        classes: 'badge badge-sm bg-w-cyan/10 border-w-cyan/30 text-w-cyan gap-1.5',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3" /></svg>',
    },
    cached: {
        classes: 'badge badge-sm bg-w-cyan/10 border-w-cyan/30 text-w-cyan gap-1.5',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
    },
    vaulting: {
        classes: 'badge badge-sm bg-w-purple/10 border-w-purple/30 text-w-purpleL gap-1.5',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M12 16.5V9.75m0 0 3 3m-3-3-3 3M6.75 19.5a4.5 4.5 0 0 1-1.41-8.775 5.25 5.25 0 0 1 10.233-2.33 3 3 0 0 1 3.758 3.848A3.752 3.752 0 0 1 18 19.5H6.75Z" /></svg>',
    },
    vaulted: {
        // no gap-1.5: label-only, no leading icon
        classes: 'badge badge-sm bg-w-purple/10 border-w-purple/30 text-w-purpleL',
        icon: '',
    },
};

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
        indicator.classList.remove('hidden');
    } else {
        indicator.classList.add('hidden');
    }
}

function settleVaultedIcon(row) {
    const icon = row.querySelector('[data-vault-progress-icon]');
    if (icon) icon.classList.remove('vault-pulse');
}

function renderBadge(status) {
    const config = BADGE_CONFIG[status.state] || BADGE_CONFIG.idle;
    const label = status.label || '';
    let peers = '';
    if ((status.state === 'caching' || status.state === 'vaulting') && status.seeders > 0) {
        peers = `<span class="opacity-70">(${status.seeders})</span>`;
    }
    // status.label is a server-translated i18n string (closed set of state keys);
    // safe to interpolate as HTML.
    const inner = [config.icon, label, peers].filter(Boolean).join(' ');
    return `<span class="${config.classes}">${inner}</span>`;
}

function attachRow(row) {
    const resourceId = row.dataset.resourceId;
    const csrf = row.dataset.csrf;
    if (!resourceId || !csrf) return null;

    const badge = row.querySelector('[data-vault-progress-badge]');

    const url = `${langPath(`/${resourceId}/status`)}?_csrf=${encodeURIComponent(csrf)}`;
    const source = new EventSource(url);

    source.onmessage = (e) => {
        let status;
        try {
            status = JSON.parse(e.data);
        } catch (err) {
            return;
        }
        applyRowFill(row, status);
        if (badge) badge.innerHTML = renderBadge(status);
        if (status.state === 'vaulted') {
            settleVaultedIcon(row);
            source.close();
        }
    };

    return source;
}

av(async function () {
    const root = this;
    const rows = root.querySelectorAll('[data-vault-progress]');
    if (!rows.length) return;

    const sources = [];
    rows.forEach((row) => {
        const s = attachRow(row);
        if (s) sources.push(s);
    });
    root._vaultProgressSources = sources;
}, function () {
    const root = this;
    if (root._vaultProgressSources) {
        root._vaultProgressSources.forEach((s) => s.close());
        root._vaultProgressSources = null;
    }
});

export {};
