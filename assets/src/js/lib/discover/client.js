// Stremio addon API client with caching, abort support, and timeouts.
//
// Manifest fetch is special: failures don't drop the addon from the list,
// they get reported with a status so the UI can show "addon unavailable"
// while still rendering the addon's last-known catalogs (from
// manifestCache) as disabled options. See manifestCache.js.

import { getLang } from '../i18n';
import * as manifestCache from './manifestCache';
import { refreshSnapshot, isSnapshotStale } from './addonsApi';

const FETCH_TIMEOUT = 10000;
const CACHE_MAX = 100;

function fetchWithTimeout(url, signal, timeout = FETCH_TIMEOUT) {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeout);

    // If an external signal is provided, forward its abort
    if (signal) {
        signal.addEventListener('abort', () => controller.abort(), { once: true });
    }

    const headers = { 'Accept-Language': getLang() };

    return fetch(url, { signal: controller.signal, headers }).finally(() => clearTimeout(timeoutId));
}

class LRUCache {
    constructor(max = CACHE_MAX) {
        this.max = max;
        this.map = new Map();
    }

    get(key) {
        if (!this.map.has(key)) return undefined;
        const value = this.map.get(key);
        // Move to end (most recently used)
        this.map.delete(key);
        this.map.set(key, value);
        return value;
    }

    set(key, value) {
        if (this.map.has(key)) this.map.delete(key);
        this.map.set(key, value);
        if (this.map.size > this.max) {
            // Delete oldest entry
            const first = this.map.keys().next().value;
            this.map.delete(first);
        }
    }
}

export const CINEMETA_BASE = 'https://v3-cinemeta.strem.io';

// Map an HTTP status / fetch error into one of the two failure buckets we
// surface to the user. 4xx (except 408/429) means the addon is responding
// but rejecting our request — likely a stale URL or revoked auth — so the
// fix is in the user's profile. Everything else (network, timeout, 5xx,
// malformed JSON) is treated as a temporary outage.
function classifyError(httpStatus, raw) {
    if (httpStatus === 401 || httpStatus === 403 || httpStatus === 404) {
        return 'misconfigured';
    }
    return 'unreachable';
}

export class StremioClient {
    // addonSeeds is the per-addon snapshot the server bootstraps into
    // window._addons. Each seed: { id, url, name, manifestId, version,
    // resources, types, fetchedAt }. Used as a fallback for the addon's
    // human-readable name + capabilities when its manifest is currently
    // unreachable AND we have no localStorage cache (e.g. new browser /
    // private window). The lazy refresh below pings the server when a
    // fresh manifest arrives and the seed is missing or stale.
    constructor(addonUrls, addonSeeds = []) {
        this.addonUrls = addonUrls.map(u => u.replace(/\/manifest\.json$/, ''));
        this.cache = new LRUCache();
        // addonStatuses is the source of truth for per-addon health, see
        // fetchAllManifests below. Each entry:
        //   { baseUrl, manifest, status: 'ok'|'unreachable'|'misconfigured',
        //     source: 'fresh'|'cache'|'seed'|null, error?: string }
        this.addonStatuses = null;
        this.seedsByUrl = new Map();
        for (const s of addonSeeds || []) {
            const baseUrl = (s.url || '').replace(/\/manifest\.json$/, '');
            if (baseUrl) this.seedsByUrl.set(baseUrl, s);
        }
        manifestCache.prune(this.addonUrls);
    }

    // Build a synthetic manifest from a server seed so the UI can render
    // a name + capabilities for an addon whose live manifest fetch just
    // failed. Catalogs are intentionally absent — we never persist them
    // server-side because addons mutate their catalog list far more
    // often than name/version/resources.
    seedToManifest(seed) {
        if (!seed) return null;
        return {
            id: seed.manifestId || '',
            name: seed.name || '',
            version: seed.version || '',
            resources: seed.resources || [],
            types: seed.types || [],
            catalogs: [],
        };
    }

    // Backward-compat alias: callers that previously expected the live
    // manifest list now read addonStatuses (only entries that have a
    // manifest, live or cached). Used by buildCatalogs in the reducer.
    get manifests() {
        if (!this.addonStatuses) return null;
        return this.addonStatuses.filter(a => a.manifest);
    }

    set manifests(_) {
        // Mutating the manifest list directly is a holdover from earlier
        // code paths; the new flow always goes through fetchAllManifests.
        // We accept the assignment to keep the existing reset-then-refetch
        // helpers (retry, wizard) working without a wider refactor.
        if (_ === null) this.addonStatuses = null;
    }

    async fetchManifest(baseUrl) {
        const url = `${baseUrl}/manifest.json`;
        let res;
        try {
            res = await fetchWithTimeout(url);
        } catch (e) {
            const err = new Error(`Failed to reach ${url}`);
            err.kind = 'unreachable';
            throw err;
        }
        if (!res.ok) {
            const err = new Error(`Manifest fetch returned ${res.status} for ${url}`);
            err.kind = classifyError(res.status);
            throw err;
        }
        try {
            return await res.json();
        } catch (e) {
            const err = new Error(`Malformed manifest at ${url}`);
            err.kind = 'unreachable';
            throw err;
        }
    }

    // Returns full per-addon status, never silently filters failures.
    // Fallback chain on fetch failure: localStorage cache → server seed
    // (whatever the server captured at add-time) → null. On success,
    // also fires off a lazy backfill request so the server-side snapshot
    // catches up if it's missing or older than 7 days.
    async fetchAllManifests() {
        const tasks = this.addonUrls.map(async (url) => {
            const seed = this.seedsByUrl.get(url);
            try {
                const manifest = await this.fetchManifest(url);
                manifestCache.write(url, manifest);
                // Lazy backfill — server captures snapshots at add-time,
                // but old rows from before migration #52 carry NULLs and
                // long-lived addons may have evolved their manifest. We
                // ping the server to update on any successful client-side
                // fetch when its copy looks stale. Fire-and-forget; errors
                // are silently dropped by addonsApi.refreshSnapshot.
                if (seed?.id && isSnapshotStale(seed.fetchedAt)) {
                    refreshSnapshot(seed.id);
                }
                return { baseUrl: url, manifest, status: 'ok', source: 'fresh' };
            } catch (e) {
                const cached = manifestCache.read(url);
                const status = e?.kind || 'unreachable';
                if (cached) {
                    return { baseUrl: url, manifest: cached.manifest, status, source: 'cache', error: e?.message, lastSuccessAt: cached.lastSuccessAt };
                }
                if (seed) {
                    const seedManifest = this.seedToManifest(seed);
                    if (seedManifest) {
                        const lastSuccessAt = seed.fetchedAt ? Date.parse(seed.fetchedAt) : null;
                        return { baseUrl: url, manifest: seedManifest, status, source: 'seed', error: e?.message, lastSuccessAt };
                    }
                }
                return { baseUrl: url, manifest: null, status, source: null, error: e?.message };
            }
        });
        const results = await Promise.all(tasks);
        this.addonStatuses = results;
        return results;
    }

    async fetchCatalog(baseUrl, type, catalogId, skip = 0, { signal } = {}) {
        const cacheKey = `${baseUrl}/${type}/${catalogId}/${skip}`;
        const cached = this.cache.get(cacheKey);
        if (cached) return cached;

        const url = `${baseUrl}/catalog/${type}/${catalogId}${skip > 0 ? `/skip=${skip}` : ''}.json`;
        const res = await fetchWithTimeout(url, signal);
        if (!res.ok) throw new Error('Failed to fetch catalog');
        const data = await res.json();
        this.cache.set(cacheKey, data);
        return data;
    }

    async fetchMeta(type, id, { signal } = {}) {
        // Cache only confirmed hits (meta with videos[]). A meta-without-
        // videos response can be transient — caching it would freeze a
        // series out of the calendar until the page reloads, even after
        // Cinemeta recovers.
        const cacheKey = `meta:${type}:${id}`;
        const cached = this.cache.get(cacheKey);
        if (cached !== undefined) return cached;

        let result = null;

        try {
            const url = `${CINEMETA_BASE}/meta/${type}/${id}.json`;
            const res = await fetchWithTimeout(url, signal);
            if (res.ok) {
                const data = await res.json();
                if (data.meta?.videos?.length > 0) {
                    this.cache.set(cacheKey, data.meta);
                    return data.meta;
                }
                if (data.meta) result = data.meta;
            }
        } catch (e) { /* fall through to user addons */ }

        // Fall back to user's meta-capable addons (status='ok' only — no
        // point hammering an addon we already know is unreachable).
        const okAddons = (this.addonStatuses || []).filter(a => a.status === 'ok' && a.manifest);
        const metaAddons = okAddons.filter(m => {
            const resources = m.manifest.resources || [];
            return resources.some(r =>
                (typeof r === 'string' && r === 'meta') ||
                (r && r.name === 'meta')
            );
        });

        const results = await Promise.allSettled(
            metaAddons.map(async (addon) => {
                const url = `${addon.baseUrl}/meta/${type}/${id}.json`;
                const res = await fetchWithTimeout(url, signal);
                if (!res.ok) throw new Error('Failed to fetch meta');
                const data = await res.json();
                return data.meta || null;
            })
        );

        for (const r of results) {
            if (r.status === 'fulfilled' && r.value?.videos?.length > 0) {
                this.cache.set(cacheKey, r.value);
                return r.value;
            }
        }
        for (const r of results) {
            if (r.status === 'fulfilled' && r.value) return r.value;
        }
        return result;
    }

    getSearchCatalogs() {
        const okAddons = (this.addonStatuses || []).filter(a => a.status === 'ok' && a.manifest);
        const catalogs = [];
        for (const m of okAddons) {
            for (const cat of (m.manifest.catalogs || [])) {
                const hasSearch =
                    cat.extraSupported?.includes('search') ||
                    cat.extra?.some(e => e.name === 'search');
                if (hasSearch) {
                    catalogs.push({
                        id: cat.id,
                        type: cat.type,
                        name: cat.name || cat.id,
                        addonName: m.manifest.name || 'Unknown',
                        baseUrl: m.baseUrl,
                    });
                }
            }
        }
        return catalogs;
    }

    async searchCatalog(baseUrl, type, catalogId, query, { signal } = {}) {
        const url = `${baseUrl}/catalog/${type}/${catalogId}/search=${encodeURIComponent(query)}.json`;
        const res = await fetchWithTimeout(url, signal, 8000);
        if (!res.ok) throw new Error('Search failed');
        const data = await res.json();
        return data.metas || [];
    }

    getStreamAddons() {
        const okAddons = (this.addonStatuses || []).filter(a => a.status === 'ok' && a.manifest);
        return okAddons.filter(m => {
            const resources = m.manifest.resources || [];
            return resources.some(r =>
                (typeof r === 'string' && r === 'stream') ||
                (r && r.name === 'stream')
            );
        });
    }

    async fetchStreamFromAddon(addon, type, id, { signal } = {}) {
        const url = `${addon.baseUrl}/stream/${type}/${id}.json`;
        const res = await fetchWithTimeout(url, signal);
        if (!res.ok) throw new Error('Failed to fetch streams');
        const data = await res.json();
        return (data.streams || []).map(s => ({
            ...s,
            addonName: addon.manifest.name || 'Unknown',
        }));
    }

    async fetchStreams(type, id, { signal } = {}) {
        const streamAddons = this.getStreamAddons();

        const results = await Promise.allSettled(
            streamAddons.map(addon => this.fetchStreamFromAddon(addon, type, id, { signal }))
        );

        const streams = [];
        for (const r of results) {
            if (r.status === 'fulfilled') streams.push(...r.value);
        }
        return streams;
    }
}
