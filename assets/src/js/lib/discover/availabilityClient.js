// Client for POST /discover/availability: which of these streams Webtor
// already holds, so the list can say so and put them first.
//
// Discover asks the user's addons from the browser, so its streams never pass
// the server code that marks the Stremio addon's answers. Without the mark a
// viewer picks among forty releases of one film by resolution and size, and
// each pick of a cold one costs them a ~58 s median wait (measured 2026-09-19)
// and the platform one more torrent to fetch. Most of the time a copy that
// starts at once is already in the list -- it just was not labelled.

import { langPath } from '../i18n';
import { csrfHeaders } from './http';
import { extractInfoHash, extractFileIdx } from './stream';

// The answer decides the ORDER of the list, so the list waits for it instead
// of being re-sorted under the viewer's finger a moment later. That makes this
// the price of every stream modal: two indexed reads on the server, and no
// longer than this before the list is shown as it is.
const FETCH_TIMEOUT = 1500;

// Mirrors maxAvailabilityItems on the server. Only the top of a longer list is
// asked about -- the rest are far below the fold in any order.
const MAX_ITEMS = 500;

// applyCached returns the streams with `cached: true` on the given positions
// and those streams moved to the top. Stable on both sides: each source's own
// ranking and the round-robin of interleaveBySource survive within the cached
// group and within the rest.
export function applyCached(streams, positions) {
    const list = streams || [];
    const hit = new Set((positions || []).filter(i => Number.isInteger(i) && i >= 0 && i < list.length));
    if (hit.size === 0) return list;
    const first = [];
    const rest = [];
    list.forEach((s, i) => {
        if (hit.has(i)) first.push({ ...s, cached: true });
        else rest.push(s);
    });
    return [...first, ...rest];
}

// availabilityItems is the request body for a list: one item per stream, in
// the list's order, because the answer is positions. A stream without a hash
// still takes its position (as an item the server ignores).
export function availabilityItems(streams) {
    return (streams || []).slice(0, MAX_ITEMS).map((s) => {
        const item = { infoHash: extractInfoHash(s) || '' };
        const idx = extractFileIdx(s);
        if (Number.isInteger(idx) && idx >= 0) item.fileIdx = idx;
        return item;
    });
}

// withCached never throws and never returns less than it was given: the mark
// is a hint, and a list without it is still the list.
export async function withCached(streams, { signal, fetchImpl } = {}) {
    const list = streams || [];
    const items = availabilityItems(list);
    if (!items.some(it => it.infoHash)) return list;
    const doFetch = fetchImpl || fetch;
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), FETCH_TIMEOUT);
    if (signal) signal.addEventListener('abort', () => controller.abort(), { once: true });
    try {
        const res = await doFetch(langPath('/discover/availability'), {
            method: 'POST',
            headers: csrfHeaders(),
            body: JSON.stringify({ items }),
            signal: controller.signal,
        });
        if (!res.ok) return list;
        const data = await res.json();
        return applyCached(list, data && data.cached);
    } catch (e) {
        return list;
    } finally {
        clearTimeout(timeoutId);
    }
}
