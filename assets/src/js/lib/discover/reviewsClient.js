// Wire wrapper for POST /discover/reviews — fetches TMDB user reviews
// for the title open in the stream modal. Single-id endpoint (no grid
// batching: only the modal needs reviews).
//
// Module-level cache holds one verdict per IMDB id for the page
// lifetime: an array of reviews (possibly empty = server checked, none
// exist). Non-OK responses and network failures are NOT cached — the
// next modal open retries instead of pinning a transient TMDB failure
// as "no reviews" for the session.

import { langPath } from './i18n';
import { csrfHeaders } from './http';
import { bareImdbTitleID } from './ids';

const cache = new Map();
const pending = new Map();

// fetchReviews resolves to an array of {author, rating, content, url,
// createdAt}. Resolves to [] for non-reviewable ids and on failure (the
// section just doesn't render); concurrent calls for the same id share
// one request.
export async function fetchReviews(id, type) {
    if (!bareImdbTitleID(id)) return [];
    if (cache.has(id)) return cache.get(id);
    if (pending.has(id)) return pending.get(id);

    const p = (async () => {
        try {
            const res = await fetch(langPath('/discover/reviews'), {
                method: 'POST',
                headers: csrfHeaders(),
                body: JSON.stringify({ id, type: type === 'series' ? 'series' : 'movie' }),
            });
            if (!res.ok) return []; // transient — leave uncached for retry
            const data = await res.json();
            const reviews = Array.isArray(data.reviews) ? data.reviews : [];
            cache.set(id, reviews);
            return reviews;
        } catch {
            return [];
        } finally {
            pending.delete(id);
        }
    })();
    pending.set(id, p);
    return p;
}
