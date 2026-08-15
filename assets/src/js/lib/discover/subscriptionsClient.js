// Wire-format wrappers for /discover/subscriptions/*. Same shape and same
// conventions as watchlistClient: CSRF through csrfHeaders, every message
// comes from the server so toasts stay in the current locale, and a network
// failure is a value rather than a throw.
//
// A subscription is addressed by content, not by row id — {kind, video_id,
// season} — because that is what the UI knows about the thing under the
// cursor. Row ids only exist in the profile list.

// Explicit extensions: these two modules are imported by the node --test
// suite as well as by webpack, and Node's ESM resolver does not guess them.
import { langPath } from './i18n.js';
import { csrfHeaders } from './http.js';

// subscriptionKey is the identity the UI compares against. Same string is
// produced from a server item and from a local {kind, videoId, season}, so
// "am I subscribed to this" is a Set lookup rather than a scan.
export function subscriptionKey(kind, videoId, season) {
    if (kind === 'season') return `season:${videoId}:${Number(season) || 0}`;
    return `movie:${videoId}`;
}

export function itemKey(item) {
    return subscriptionKey(item.kind, item.video_id, item.season);
}

// fetchSubscriptionIds is the prefetch that decides which bells render as
// already-subscribed. Failures return an empty set: a missing highlight is
// a smaller wrong than a modal that refuses to open.
export async function fetchSubscriptionIds() {
    try {
        const res = await fetch(langPath('/discover/subscriptions/ids'), {
            method: 'GET',
            headers: { 'Accept': 'application/json' },
        });
        if (!res.ok) return { keys: [], count: 0, limit: -1 };
        const data = await res.json();
        const items = data.items || [];
        return {
            keys: items.map(itemKey),
            count: data.count ?? items.length,
            limit: data.limit ?? -1,
        };
    } catch (e) {
        return { keys: [], count: 0, limit: -1 };
    }
}

// subscribe returns:
//   { ok: true, added, message }          — server accepted
//   { ok: false, code, message, limit? }  — code is one of limit_exceeded,
//                                           not_eligible, bad_request,
//                                           internal, network
export async function subscribe({ kind, videoId, season, source }) {
    try {
        const res = await fetch(langPath('/discover/subscriptions'), {
            method: 'POST',
            headers: csrfHeaders(),
            body: JSON.stringify({
                kind,
                video_id: videoId,
                season: kind === 'season' ? Number(season) || 0 : 0,
                source: source || 'other',
            }),
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

export async function unsubscribe({ kind, videoId, season }) {
    const query = kind === 'season' ? `?season=${Number(season) || 0}` : '';
    try {
        const res = await fetch(
            langPath(`/discover/subscriptions/${kind}/${encodeURIComponent(videoId)}${query}`),
            { method: 'DELETE', headers: csrfHeaders() },
        );
        let body = {};
        try { body = await res.json(); } catch (_) { /* ignore */ }
        if (res.ok) return { ok: true, message: body.message || '' };
        return { ok: false, code: body.code || `http_${res.status}`, message: body.message || '' };
    } catch (e) {
        return { ok: false, code: 'network', message: '' };
    }
}
