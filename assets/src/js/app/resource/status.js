import av from '../../lib/av';

const BADGE_CONFIG = {
    idle: {
        classes: 'badge badge-sm bg-base-200/50 border-w-line/30 text-w-muted gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M15 12H9m12 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
    },
    caching: {
        classes: 'badge badge-sm bg-w-cyan/10 border-w-cyan/30 text-w-cyan gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3 animate-pulse"><path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3" /></svg>',
    },
    cached: {
        classes: 'badge badge-sm bg-green-500/10 border-green-500/30 text-green-400 gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
    },
    vaulting: {
        classes: 'badge badge-sm bg-w-purple/10 border-w-purple/30 text-w-purpleL gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3 animate-pulse"><path stroke-linecap="round" stroke-linejoin="round" d="M12 16.5V9.75m0 0 3 3m-3-3-3 3M6.75 19.5a4.5 4.5 0 0 1-1.41-8.775 5.25 5.25 0 0 1 10.233-2.33 3 3 0 0 1 3.758 3.848A3.752 3.752 0 0 1 18 19.5H6.75Z" /></svg>',
    },
    caching_checking: {
        classes: 'badge badge-sm bg-base-200/50 border-w-line/30 text-w-sub gap-1.5 px-3 py-2',
        icon: '<span class="loading loading-dots loading-xs"></span>',
    },
    caching_noseeders: {
        classes: 'badge badge-sm bg-error/10 border-error/30 text-error gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="m9.75 9.75 4.5 4.5m0-4.5-4.5 4.5M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
    },
    caching_paused: {
        classes: 'badge badge-sm bg-warning/10 border-warning/30 text-warning gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M15.75 5.25v13.5m-7.5-13.5v13.5" /></svg>',
    },
    vault_waiting: {
        classes: 'badge badge-sm bg-w-purple/10 border-w-purple/30 text-w-purpleL gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3 animate-pulse"><path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
    },
    vault_failed: {
        classes: 'badge badge-sm bg-warning/10 border-warning/30 text-warning gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z" /></svg>',
    },
    unknown: {
        classes: 'badge badge-sm bg-base-200/50 border-w-line/30 text-w-muted gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 5.25h.008v.008H12v-.008Z" /></svg>',
    },
    vaulted: {
        classes: 'badge badge-sm bg-green-500/10 border-green-500/30 text-green-400 gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285Z" /></svg>',
    },
};

// Piece bar. Cells come base64-packed from the server (0..255 fill per cell,
// plus a bitset of cells being fetched); the colour is the badge's, and the
// whole thing is one grid of spans — cheap enough at 256 cells to rebuild on
// every status message.
const BAR_COLOR = {
    cached: 'text-green-400',
    vaulted: 'text-green-400',
    vaulting: 'text-w-purpleL',
    vault_waiting: 'text-w-purpleL',
    vault_failed: 'text-w-purpleL',
    caching: 'text-w-cyan',
    idle: 'text-w-cyan',
    unknown: 'text-w-cyan',
};

function decodeBytes(b64) {
    try {
        const bin = atob(b64);
        const out = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
        return out;
    } catch (e) {
        return null;
    }
}

const BAR_DIVIDER = '<div class="piece-bar-divider" aria-hidden="true"></div>';

function renderBar(status) {
    // No bar to draw (idle, unknown, waiting): keep the slot's height with a
    // hairline so the header does not jump when a transfer starts or ends.
    if (!status.pieces) return BAR_DIVIDER;
    const fill = decodeBytes(status.pieces);
    if (!fill || !fill.length) return BAR_DIVIDER;
    const active = status.active ? decodeBytes(status.active) : null;
    const color = BAR_COLOR[status.state] || 'text-w-cyan';
    const title = status.pieces_label || '';
    let cells = '';
    for (let i = 0; i < fill.length; i++) {
        const isActive = active && (active[i >> 3] & (1 << (i & 7)));
        cells += `<span style="--fill:${(fill[i] / 255).toFixed(2)}"${isActive ? ' class="is-active"' : ''}></span>`;
    }
    return `<div class="piece-bar ${color}" role="img" aria-label="${title}" title="${title}">${cells}</div>`;
}

function paintBars(resourceId, status) {
    const html = renderBar(status);
    document.querySelectorAll(`[data-piece-bar-for="${resourceId}"]`).forEach((host) => {
        host.innerHTML = html;
    });
}

function renderLoading() {
    return '<div class="badge badge-sm bg-base-200/50 border-w-line/30 text-w-muted gap-1.5 px-3 py-2"><span class="loading loading-dots loading-xs"></span></div>';
}

function renderBadge(status) {
    // Paused caching is the same state with a different face: amber, a pause
    // glyph, no throughput (there is none), and the server's "paused" label.
    const checking = status.state === 'caching' && status.checking;
    const noSeeders = status.state === 'caching' && status.no_seeders && !checking;
    const paused = status.state === 'caching' && status.paused && !noSeeders && !checking;
    const config = BADGE_CONFIG[checking ? 'caching_checking' : noSeeders ? 'caching_noseeders' : paused ? 'caching_paused' : status.state];
    if (!config) return '';

    let label = status.label || '';
    let peers = '';
    if (noSeeders) {
        label = `${label} · ${Math.round(status.progress)}%`;
    } else if (checking) {
        label = `${label} ${Math.round(status.progress)}%`;
    } else if (status.state === 'caching' || status.state === 'vaulting' || (status.state === 'vault_failed' && status.progress > 0)) {
        label = `${label} ${Math.round(status.progress)}%`;
        // Swarm throughput, server-formatted ("2.3 MB/s"); absent when nothing moves.
        if (status.rate_label && !paused) label = `${label} · ${status.rate_label}`;
    }
    // Swarm suffix ("12 seeders · 3 leechers") arrives translated from the
    // server; empty for terminal states and when nothing is known.
    if (status.swarm) {
        peers = ` <span class="opacity-70">(${status.swarm})</span>`;
    }

    // Built as a node, not a string: `detail` is the Vault API's error text
    // (arbitrary upstream content) and goes into the title attribute through
    // the DOM property, which the serializer escapes properly. Icon is our
    // constant markup; label and swarm are server-translated strings.
    const el = document.createElement('div');
    el.className = config.classes;
    el.innerHTML = `${config.icon} ${label}${peers}`;
    if (status.state === 'vault_failed' && status.detail) {
        el.title = String(status.detail);
    }
    if (paused && status.paused_hint) {
        el.title = String(status.paused_hint);
    }
    if (noSeeders && status.no_seeders_hint) {
        el.title = String(status.no_seeders_hint);
    }
    return el.outerHTML;
}

av(async function() {
    const container = this;
    const resourceId = container.dataset.resourceId;
    if (!resourceId) return;

    const badge = container.querySelector('#torrent-status-badge');
    if (!badge) return;

    // Show loading state until first SSE message arrives
    badge.innerHTML = renderLoading();

    const csrfToken = container.dataset.csrf;
    if (!csrfToken) return;

    const lang = document.documentElement.lang;
    const langPrefix = lang && lang !== 'en' ? `/${lang}` : '';
    // Dev-only badge override (handlers/resource/status.go debugStatus): the
    // page URL's debug_status/seeders/leechers/peers/progress ride along to
    // the SSE endpoint; the server ignores them in release mode.
    const dbg = new URLSearchParams(window.location.search);
    let extra = '';
    for (const k of ['debug_status', 'seeders', 'leechers', 'peers', 'progress', 'debug_pieces', 'rate', 'paused', 'noseeders', 'checking']) {
        if (dbg.has(k)) extra += `&${k}=${encodeURIComponent(dbg.get(k))}`;
    }
    const source = new EventSource(`${langPrefix}/${resourceId}/status?_csrf=${encodeURIComponent(csrfToken)}${extra}`);
    container._statusSource = source;

    source.onmessage = (e) => {
        try {
            const status = JSON.parse(e.data);
            badge.innerHTML = renderBadge(status);
            paintBars(resourceId, status);
            if (status.state === 'vaulted') {
                source.close();
                container._statusSource = null;
            }
        } catch (err) {
            // Ignore parse errors
        }
    };

    source.onerror = () => {
        if (source.readyState === EventSource.CLOSED) {
            container._statusSource = null;
        }
    };

}, function() {
    const container = this;
    if (container._statusSource) {
        container._statusSource.close();
        container._statusSource = null;
    }
});

export {}
