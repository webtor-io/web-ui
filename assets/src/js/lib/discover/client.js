// Stremio addon API client with caching, abort support, and timeouts

const FETCH_TIMEOUT = 10000;
const CACHE_MAX = 100;

function fetchWithTimeout(url, signal, timeout = FETCH_TIMEOUT) {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeout);

    // If an external signal is provided, forward its abort
    if (signal) {
        signal.addEventListener('abort', () => controller.abort(), { once: true });
    }

    return fetch(url, { signal: controller.signal }).finally(() => clearTimeout(timeoutId));
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

export class StremioClient {
    constructor(addonUrls) {
        this.addonUrls = addonUrls.map(u => u.replace(/\/manifest\.json$/, ''));
        this.cache = new LRUCache();
        this.manifests = null;
    }

    async fetchManifest(baseUrl) {
        const url = `${baseUrl}/manifest.json`;
        const res = await fetchWithTimeout(url);
        if (!res.ok) throw new Error(`Failed to fetch manifest from ${url}`);
        return res.json();
    }

    async fetchAllManifests() {
        const results = await Promise.allSettled(
            this.addonUrls.map(async (url) => {
                const manifest = await this.fetchManifest(url);
                return { baseUrl: url, manifest };
            })
        );
        return results
            .filter(r => r.status === 'fulfilled')
            .map(r => r.value);
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
        // Always try Cinemeta first
        try {
            const url = `${CINEMETA_BASE}/meta/${type}/${id}.json`;
            const res = await fetchWithTimeout(url, signal);
            if (res.ok) {
                const data = await res.json();
                if (data.meta?.videos?.length > 0) {
                    return data.meta;
                }
            }
        } catch (e) { /* fall through to user addons */ }

        // Fall back to user's meta-capable addons
        const metaAddons = this.manifests
            ? this.manifests.filter(m => {
                const resources = m.manifest.resources || [];
                return resources.some(r =>
                    (typeof r === 'string' && r === 'meta') ||
                    (r && r.name === 'meta')
                );
            })
            : [];

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
                return r.value;
            }
        }
        for (const r of results) {
            if (r.status === 'fulfilled' && r.value) return r.value;
        }
        return null;
    }

    getSearchCatalogs() {
        if (!this.manifests) return [];
        const catalogs = [];
        for (const m of this.manifests) {
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

    async fetchStreams(type, id, { signal } = {}) {
        const streamAddons = this.manifests
            ? this.manifests.filter(m => {
                const resources = m.manifest.resources || [];
                return resources.some(r =>
                    (typeof r === 'string' && r === 'stream') ||
                    (r && r.name === 'stream')
                );
            })
            : [];

        const results = await Promise.allSettled(
            streamAddons.map(async (addon) => {
                const url = `${addon.baseUrl}/stream/${type}/${id}.json`;
                const res = await fetchWithTimeout(url, signal);
                if (!res.ok) throw new Error('Failed to fetch streams');
                const data = await res.json();
                return (data.streams || []).map(s => ({
                    ...s,
                    addonName: addon.manifest.name || 'Unknown',
                }));
            })
        );

        const streams = [];
        for (const r of results) {
            if (r.status === 'fulfilled') streams.push(...r.value);
        }
        return streams;
    }
}
