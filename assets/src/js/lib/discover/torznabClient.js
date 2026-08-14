// Client for POST /discover/torznab/streams.
//
// Torznab is the one source Discover cannot fetch itself: indexers answer
// without CORS headers, and a self-hosted one on plain http is unreachable
// from this https page as mixed content. So the server queries the user's
// indexers and we merge its answer into the addon streams fetched in the
// browser. Everything else on this page still talks to addons directly.

import { langPath } from '../i18n';
import { csrfHeaders } from './http';

// Indexer queries fan out to every tracker configured in the user's
// Jackett/Prowlarr, so they are slower than an addon call by design. The
// server caps its own search at TORZNAB_TIMEOUT; this is the outer bound.
const FETCH_TIMEOUT = 20000;

// hasIndexers reports whether the user has any enabled indexer, so the
// stream modal can skip the request entirely instead of paying a round-trip
// for an empty list.
export function hasIndexers() {
    return Array.isArray(window._indexers) && window._indexers.length > 0;
}

// indexerLabel names the single row the stream modal shows for all
// indexers. It is the first indexer's name plus a count, rather than one
// row per indexer: the server queries them together and reports one result
// list, so per-indexer progress would be fiction.
export function indexerLabel() {
    if (!hasIndexers()) return '';
    const names = window._indexers.map(i => i.name).filter(Boolean);
    const first = names[0] || 'Indexers';
    return names.length > 1 ? `${first} +${names.length - 1}` : first;
}

// fetchTorznabStreams returns stream objects in the same shape the addon
// client produces — {name, title, infoHash, fileIdx} — so StreamModal needs
// no special case to render them.
export async function fetchTorznabStreams(type, id, { signal } = {}) {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), FETCH_TIMEOUT);
    if (signal) {
        signal.addEventListener('abort', () => controller.abort(), { once: true });
    }
    try {
        const res = await fetch(langPath('/discover/torznab/streams'), {
            method: 'POST',
            headers: csrfHeaders(),
            body: JSON.stringify({ type, id }),
            signal: controller.signal,
        });
        if (!res.ok) throw new Error(`Indexer search failed with ${res.status}`);
        const data = await res.json();
        return (data.streams || []).map(s => ({
            ...s,
            // First line of name is the indexer label, same convention the
            // addon streams follow.
            addonName: (s.name || '').split('\n')[0],
        }));
    } finally {
        clearTimeout(timeoutId);
    }
}
