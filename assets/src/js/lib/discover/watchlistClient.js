// Wire-format wrappers for /discover/watchlist/*. The handler is JSON-only;
// these helpers handle CSRF, error normalisation, and shape conversion so
// the reducer + DiscoverApp don't have to know about the HTTP layer.
//
// Item shape returned by GET /discover/watchlist comes back as
//   { video_id, type, title, year, poster_url, rating, source, created_at }
// which we adapt into the Cinemeta-flavoured shape ItemGrid expects:
//   { id, type, name, year, poster, imdbRating }
// so the same grid component renders both modes (catalogs / watchlist) with
// no source-of-truth branching.

import { langPath } from './i18n';

function csrfHeaders() {
    return {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
        'X-Requested-With': 'XMLHttpRequest',
        'X-CSRF-TOKEN': window._CSRF,
    };
}

function adaptItem(row) {
    return {
        id: row.video_id,
        type: row.type,
        name: row.title,
        year: row.year || undefined,
        poster: row.poster_url || `/lib/${row.type}/poster/${row.video_id}/500.jpg`,
        imdbRating: row.rating != null ? row.rating : undefined,
    };
}

// fetchWatchlistIds is a cheap prefetch for the bookmark-badge highlight in
// catalog/search/AI grids. It returns just the ids — we don't need metadata
// to draw the filled icon.
export async function fetchWatchlistIds() {
    try {
        const res = await fetch(langPath('/discover/watchlist/ids'), {
            method: 'GET',
            headers: { 'Accept': 'application/json' },
        });
        if (!res.ok) return { ids: [], limit: -1 };
        const data = await res.json();
        return { ids: data.video_ids || [], limit: data.limit ?? -1 };
    } catch (e) {
        return { ids: [], limit: -1 };
    }
}

// fetchWatchlist returns the full grid: items in Cinemeta shape, plus the
// id list (for the highlight Set in non-filter mode) and the soft-cap limit.
export async function fetchWatchlist() {
    try {
        const res = await fetch(langPath('/discover/watchlist'), {
            method: 'GET',
            headers: { 'Accept': 'application/json' },
        });
        if (!res.ok) return { items: [], ids: [], limit: -1 };
        const data = await res.json();
        const items = (data.items || []).map(adaptItem);
        return { items, ids: data.video_ids || [], limit: data.limit ?? -1 };
    } catch (e) {
        return { items: [], ids: [], limit: -1 };
    }
}

// addToWatchlist returns:
//   { ok: true, added: bool, message } — server accepted; message is the
//                                        server-localised toast string
//                                        (empty when added=false on conflict)
//   { ok: false, code, message, limit? } — server-localised error
//
// Error message ALWAYS comes from the server so the toast stays in the
// current i18n locale — same convention as toggleWatched / rateVideo (see
// apiPost in discoverUtils.js).
export async function addToWatchlist(videoId, type, source) {
    try {
        const res = await fetch(langPath('/discover/watchlist'), {
            method: 'POST',
            headers: csrfHeaders(),
            body: JSON.stringify({ video_id: videoId, type, source: source || 'other' }),
        });
        let body = {};
        try { body = await res.json(); } catch (_) { /* ignore */ }
        if (res.ok) {
            return { ok: true, added: !!body.added, message: body.message || '' };
        }
        return {
            ok: false,
            code: body.code || `http_${res.status}`,
            message: body.message || '',
            limit: body.limit,
        };
    } catch (e) {
        return { ok: false, code: 'network', message: '' };
    }
}

export async function removeFromWatchlist(videoId, type) {
    try {
        const res = await fetch(langPath(`/discover/watchlist/${type}/${encodeURIComponent(videoId)}`), {
            method: 'DELETE',
            headers: csrfHeaders(),
        });
        let body = {};
        try { body = await res.json(); } catch (_) { /* ignore */ }
        if (res.ok) return { ok: true, message: body.message || '' };
        return { ok: false, code: body.code || `http_${res.status}`, message: body.message || '' };
    } catch (e) {
        return { ok: false, code: 'network', message: '' };
    }
}
