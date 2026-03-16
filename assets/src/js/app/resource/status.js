import av from '../../lib/av';

const BADGE_CONFIG = {
    idle: {
        classes: 'badge badge-sm bg-base-200/50 border-w-line/30 text-w-muted gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M15 12H9m12 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
        label: 'Idle',
    },
    caching: {
        classes: 'badge badge-sm bg-w-cyan/10 border-w-cyan/30 text-w-cyan gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3 animate-pulse"><path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3" /></svg>',
    },
    cached: {
        classes: 'badge badge-sm bg-green-500/10 border-green-500/30 text-green-400 gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
        label: 'Cached',
    },
    vaulting: {
        classes: 'badge badge-sm bg-w-purple/10 border-w-purple/30 text-w-purpleL gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3 animate-pulse"><path stroke-linecap="round" stroke-linejoin="round" d="M12 16.5V9.75m0 0 3 3m-3-3-3 3M6.75 19.5a4.5 4.5 0 0 1-1.41-8.775 5.25 5.25 0 0 1 10.233-2.33 3 3 0 0 1 3.758 3.848A3.752 3.752 0 0 1 18 19.5H6.75Z" /></svg>',
    },
    vaulted: {
        classes: 'badge badge-sm bg-green-500/10 border-green-500/30 text-green-400 gap-1.5 px-3 py-2',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285Z" /></svg>',
        label: 'Vaulted',
    },
};

function renderLoading() {
    return '<div class="badge badge-sm bg-base-200/50 border-w-line/30 text-w-muted gap-1.5 px-3 py-2"><span class="loading loading-dots loading-xs"></span></div>';
}

function renderBadge(status) {
    const config = BADGE_CONFIG[status.state];
    if (!config) return '';

    let label = config.label || '';
    let peers = '';
    if (status.state === 'caching') {
        label = `Caching ${Math.round(status.progress)}%`;
    } else if (status.state === 'vaulting') {
        label = `Vaulting ${Math.round(status.progress)}%`;
    }
    // Show peers for non-terminal states
    if (status.state !== 'cached' && status.state !== 'vaulted' && status.seeders > 0) {
        peers = ` <span class="opacity-70">(${status.seeders} peers)</span>`;
    }

    return `<div class="${config.classes}">${config.icon} ${label}${peers}</div>`;
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

    const source = new EventSource(`/${resourceId}/status?_csrf=${encodeURIComponent(csrfToken)}`);
    container._statusSource = source;

    source.onmessage = (e) => {
        try {
            const status = JSON.parse(e.data);
            badge.innerHTML = renderBadge(status);
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
