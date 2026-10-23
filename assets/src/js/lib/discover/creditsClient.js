// Wire wrapper for POST /discover/credits — directors / creators / cast
// from TMDB for the title open in the stream modal. Same caching rules as
// reviewsClient: one verdict per IMDB id for the page lifetime, failures
// left uncached so the next modal open retries.

import { langPath } from './i18n';
import { csrfHeaders } from './http';
import { bareImdbTitleID } from './ids';

const cache = new Map();
const pending = new Map();

// peekCredits is the cached verdict for id, or null, synchronously -- so a
// header that remounts on every modal view draws known credits at once.
export function peekCredits(id) {
    return cache.get(id) || null;
}

// fetchCredits resolves to {directors?, creators?: string[], people?:
// {name, photo?}[]}, or null for non-resolvable ids and on failure.
export async function fetchCredits(id, type) {
    if (!bareImdbTitleID(id)) return null;
    if (cache.has(id)) return cache.get(id);
    if (pending.has(id)) return pending.get(id);

    const p = (async () => {
        try {
            const res = await fetch(langPath('/discover/credits'), {
                method: 'POST',
                headers: csrfHeaders(),
                body: JSON.stringify({ id, type: type === 'series' ? 'series' : 'movie' }),
            });
            if (!res.ok) return null; // transient — leave uncached for retry
            const data = await res.json();
            cache.set(id, data);
            return data;
        } catch {
            return null;
        } finally {
            pending.delete(id);
        }
    })();
    pending.set(id, p);
    return p;
}
