import { buildCatalogs, getCatalogsForType, getTypes } from './discoverReducer';

// Shared button chip class for Tabs (btn-sm) and FilterChips (btn-xs)
// Size classes must be written as full literals for Tailwind static analysis
export function chipClass(active, size = 'sm') {
    const sizeClass = size === 'xs' ? 'btn-xs' : 'btn-sm';
    return active
        ? `btn ${sizeClass} bg-w-cyan/15 border border-w-cyan/30 text-w-cyan`
        : `btn ${sizeClass} btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan`;
}

// Dedup: modal restore logic used in two effects
export function restoreModalFromUrl(id, url, openModalById, modalEpisodeRef) {
    if (modalEpisodeRef.current) {
        const isRestoredEntry = window.history.state?.context === 'links';
        if (isRestoredEntry) {
            url.withPopstate(() => openModalById(id));
        } else {
            url.replace({ id, season: null, episode: null });
            openModalById(id);
        }
    } else {
        url.withPopstate(() => openModalById(id));
    }
}

// Dedup: manifest loading shared between init and retry
export async function loadManifests(client) {
    const manifests = await client.fetchAllManifests();
    client.manifests = manifests;
    const catalogs = buildCatalogs(manifests);
    const types = getTypes(catalogs);
    return { manifests, catalogs, types };
}

// Queries the server for the subset of the given IMDB ids that this user has
// marked as watched. Returns [] on any failure (auth error, network, etc.) —
// a failed marker query must never block discover rendering. Filters out
// non-IMDB ids (Stremio can return addon-specific ids like "tt1234567:1:2"
// for episodes — we only query top-level titles).
// Fetches user statuses (watched + rating) for IMDB ids in one request.
// Returns { statuses: { "tt123": { watched: true, rating: 7 }, ... } }.
// On failure returns empty object — must never block discover rendering.
// Generic API POST wrapper. Server returns JSON when Accept: application/json:
//   { status: "success"|"error", message?: string, ... }
// Toast is shown automatically from the server-provided message.
async function apiPost(url, body) {
    try {
        const opts = {
            method: 'POST',
            headers: {
                'Accept': 'application/json',
                'X-Requested-With': 'XMLHttpRequest',
                'X-CSRF-TOKEN': window._CSRF,
                'X-Return-Url': window.location.pathname + window.location.search,
            },
        };
        if (body) opts.body = body;
        const res = await fetch(url, opts);
        if (!res.ok) return null;
        const data = await res.json();
        if (data.status === 'success' && data.message && window.toast) {
            window.toast.success(data.message);
        } else if (data.status === 'error') {
            if (window.toast) window.toast.error(data.message || 'Something went wrong');
            return null;
        }
        return data;
    } catch (e) {
        if (window.toast) window.toast.error('Network error');
        return null;
    }
}

export async function toggleWatched(videoID, type, currentlyWatched) {
    const action = currentlyWatched ? 'unmark' : 'mark';
    const data = await apiPost(`/library/${type}/${videoID}/${action}`);
    if (!data) return null;
    return { watched: !currentlyWatched, rateForm: !!data['rate-form'] };
}

export async function rateVideo(videoID, type, rating) {
    const body = new URLSearchParams();
    body.set('rating', String(rating));
    const data = await apiPost(`/library/${type}/${videoID}/rate`, body);
    return !!data;
}

export async function unrateVideo(videoID, type) {
    const data = await apiPost(`/library/${type}/${videoID}/unrate`);
    return !!data;
}

export async function fetchUserStatuses(ids) {
    const titleIds = (ids || []).filter(id => typeof id === 'string' && id.startsWith('tt') && !id.includes(':'));
    if (titleIds.length === 0) return {};
    try {
        const res = await fetch('/library/status', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Requested-With': 'XMLHttpRequest',
                'X-CSRF-TOKEN': window._CSRF,
            },
            body: JSON.stringify({ ids: titleIds }),
        });
        if (!res.ok) return {};
        const data = await res.json();
        return data.statuses || {};
    } catch (e) {
        return {};
    }
}
