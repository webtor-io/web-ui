import { buildCatalogs, buildAddons, getCatalogsForType, getTypes } from './discoverReducer';
import { langPath, t } from '../i18n';

// Shared button chip class for Tabs (btn-sm) and FilterChips (btn-xs)
// Size classes must be written as full literals for Tailwind static analysis
export function chipClass(active, size = 'sm') {
    const sizeClass = size === 'xs' ? 'btn-xs' : 'btn-sm';
    return active
        ? `btn ${sizeClass} bg-w-cyan/15 border border-w-cyan/30 text-w-cyan`
        : `btn ${sizeClass} btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan`;
}

// Pink-themed chip used by the Watchlist side of the Catalog | Watchlist
// mode switcher and (xs variant) by the Watchlist join-button in the stream
// modal. Active = subtle pink fill + pink border + pinkL text. Inactive =
// ghost with line border, hover lights border + text in pink — keeps the
// palette aligned with btn-soft and other heart-flavoured surfaces in the
// product, no separate token needed.
//
// Sizes: 'sm' for the sticky-bar switcher, 'xs' to align with the modal's
// btn-xs join-group (Watched / Rated badges).
export function watchlistChipClass(active, size = 'sm') {
    const sizeClass = size === 'xs' ? 'btn-xs' : 'btn-sm';
    return active
        ? `btn ${sizeClass} join-item bg-w-pink/15 border border-w-pink/40 text-w-pinkL hover:bg-w-pink/20 hover:border-w-pink/50`
        : `btn ${sizeClass} join-item btn-ghost border border-w-line text-w-sub hover:border-w-pink/40 hover:text-w-pinkL`;
}

// Cyan-themed chip for the Catalog side of the mode switcher. Mirrors
// chipClass() but always emits join-item so it sits inside the same join
// group as watchlistChipClass.
export function catalogChipClass(active, size = 'sm') {
    const sizeClass = size === 'xs' ? 'btn-xs' : 'btn-sm';
    return active
        ? `btn ${sizeClass} join-item bg-w-cyan/15 border border-w-cyan/30 text-w-cyan hover:bg-w-cyan/20 hover:border-w-cyan/40`
        : `btn ${sizeClass} join-item btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan`;
}

// Neutral-themed icon-only chip used by the Grid/Calendar view switcher.
// Square + label-less by design so it reads as secondary to the cyan/pink
// mode toggle next to it. Active = filled surface, inactive = ghost.
export function viewModeChipClass(active) {
    return active
        ? 'btn btn-sm btn-square join-item bg-w-surface border border-w-line text-w-text hover:bg-w-surface'
        : 'btn btn-sm btn-square join-item btn-ghost border border-w-line text-w-sub hover:text-w-text';
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

// Dedup: manifest loading shared between init and retry. Returns the full
// per-addon status list so the UI can show health (chip + disabled
// catalogs) alongside the catalog list itself.
export async function loadManifests(client) {
    const addonStatuses = await client.fetchAllManifests();
    const catalogs = buildCatalogs(addonStatuses);
    const types = getTypes(catalogs);
    const addons = buildAddons(addonStatuses);
    // manifests kept for backward-compat with anything still reading the
    // old shape (filtered to entries with a manifest, ignoring health).
    const manifests = addonStatuses.filter(a => a.manifest).map(a => ({ baseUrl: a.baseUrl, manifest: a.manifest }));
    return { manifests, catalogs, types, addons };
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
            if (window.toast) window.toast.error(data.message || t('discover.somethingWrong'));
            return null;
        }
        return data;
    } catch (e) {
        if (window.toast) window.toast.error(t('discover.networkError'));
        return null;
    }
}

export async function toggleWatched(videoID, type, currentlyWatched) {
    const action = currentlyWatched ? 'unmark' : 'mark';
    const data = await apiPost(langPath(`/library/${type}/${videoID}/${action}`));
    if (!data) return null;
    return { watched: !currentlyWatched, rateForm: !!data['rate-form'] };
}

export async function rateVideo(videoID, type, rating) {
    const body = new URLSearchParams();
    body.set('rating', String(rating));
    const data = await apiPost(langPath(`/library/${type}/${videoID}/rate`), body);
    return !!data;
}

export async function unrateVideo(videoID, type) {
    const data = await apiPost(langPath(`/library/${type}/${videoID}/unrate`));
    return !!data;
}

export async function fetchUserStatuses(ids) {
    const titleIds = (ids || []).filter(id => typeof id === 'string' && id.startsWith('tt') && !id.includes(':'));
    if (titleIds.length === 0) return {};
    try {
        const res = await fetch(langPath('/library/status'), {
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
