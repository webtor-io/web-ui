// Wire wrapper for POST /discover/localize — the bridge between catalog
// items fetched client-side from Stremio addons and the server-side
// enrichment localization pipeline (Enricher.LocalizeByID → tmdb.localized
// cache). English sessions never call the network: getLang() === 'en'
// short-circuits everything.
//
// Module-level cache holds one verdict per IMDB id for the page lifetime:
// an object {title?, plot?} when the pipeline localized it, null when the
// server explicitly answered "checked, no translation" (an empty object in
// the response). Ids OMITTED from a 200 response failed server-side
// (TMDB timeout / rate-limit) — those are NOT cached, so the next batch
// retries them instead of pinning English for the session.

import { getLang } from '../i18n';
import { langPath } from './i18n';
import { csrfHeaders } from './http';
import { bareImdbTitleID } from './ids';

// Server caps a batch at 100 ids; chunk larger merges (multi-source search
// can exceed a single catalog page) instead of silently dropping the tail.
const BATCH_LIMIT = 100;

const cache = new Map();
const pending = new Set();

// Bare IMDB title ids only — shared guard, see ids.js.
const localizableID = bareImdbTitleID;

// Synchronous cache read for call sites that need the translation at
// dispatch time (modal title assembly in DiscoverApp).
export function getCachedLocalized(id) {
    return cache.get(id) || null;
}

// localizedTitle picks the cached localized title for id, falling back to
// the given candidates in order. Every modal dispatch site that mixes
// Cinemeta's English meta.name with grid item names must build its title
// through this helper so the precedence stays in one place.
export function localizedTitle(id, ...fallbacks) {
    return getCachedLocalized(id)?.title || fallbacks.find(Boolean);
}

// withLocalized overlays the cached translation onto a grid/deep-link item
// (same shape ItemGrid uses). Returns the item untouched when nothing is
// cached for it.
export function withLocalized(item) {
    const loc = item && getCachedLocalized(item.id);
    if (!loc) return item;
    return {
        ...item,
        name: loc.title || item.name,
        description: loc.plot || item.description,
    };
}

async function postBatch(items) {
    const res = await fetch(langPath('/discover/localize'), {
        method: 'POST',
        headers: csrfHeaders(),
        body: JSON.stringify({ items }),
    });
    if (!res.ok) return null;
    const data = await res.json();
    return data.items || {};
}

// fetchLocalized takes [{id, type}] and resolves to {id: {title, plot}} for
// every id the pipeline could localize — including ids already cached from
// earlier batches, so callers can merge the result without tracking what
// was requested when.
export async function fetchLocalized(items) {
    const out = {};
    if (getLang() === 'en') return out;

    const seen = new Set();
    const targets = [];
    for (const it of items || []) {
        if (!it || !localizableID(it.id) || seen.has(it.id)) continue;
        seen.add(it.id);
        const hit = cache.get(it.id);
        if (hit) out[it.id] = hit;
        if (!cache.has(it.id) && !pending.has(it.id)) {
            targets.push({ id: it.id, type: it.type === 'series' ? 'series' : 'movie' });
        }
    }
    if (!targets.length) return out;

    targets.forEach(it => pending.add(it.id));
    try {
        const chunks = [];
        for (let i = 0; i < targets.length; i += BATCH_LIMIT) {
            chunks.push(targets.slice(i, i + BATCH_LIMIT));
        }
        const results = await Promise.all(chunks.map(c => postBatch(c).catch(() => null)));
        for (let i = 0; i < chunks.length; i++) {
            if (!results[i]) continue; // network/HTTP failure — leave uncached for retry
            for (const it of chunks[i]) {
                if (!(it.id in results[i])) continue; // pipeline error — leave uncached for retry
                const loc = results[i][it.id];
                const hasText = !!(loc && (loc.title || loc.plot));
                cache.set(it.id, hasText ? loc : null); // null = definitive "no translation"
                if (hasText) out[it.id] = loc;
            }
        }
    } finally {
        targets.forEach(it => pending.delete(it.id));
    }
    return out;
}
