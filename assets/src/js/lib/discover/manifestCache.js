// localStorage cache for Stremio addon manifests.
//
// Why: when a user's addon is unreachable, we still want to show its
// catalog list (disabled) and addon name in the UI. The only place we
// know that information is the manifest — so we keep a copy from the
// last successful fetch.
//
// Cache is overwrite-on-success: a failing fetch never invalidates the
// stored copy. A 30-day sanity cap drops genuinely abandoned entries.

const PREFIX = 'stremio.manifest.';
const MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;
const MAX_BYTES = 200 * 1024;

function key(baseUrl) {
    return PREFIX + baseUrl;
}

function safeLS() {
    try { return window.localStorage; } catch { return null; }
}

export function read(baseUrl) {
    const ls = safeLS();
    if (!ls) return null;
    try {
        const raw = ls.getItem(key(baseUrl));
        if (!raw) return null;
        const obj = JSON.parse(raw);
        if (!obj?.manifest || !obj?.lastSuccessAt) return null;
        if (Date.now() - obj.lastSuccessAt > MAX_AGE_MS) {
            ls.removeItem(key(baseUrl));
            return null;
        }
        return { manifest: obj.manifest, lastSuccessAt: obj.lastSuccessAt };
    } catch {
        return null;
    }
}

export function write(baseUrl, manifest) {
    const ls = safeLS();
    if (!ls) return;
    const payload = JSON.stringify({ manifest, lastSuccessAt: Date.now() });
    if (payload.length > MAX_BYTES) return;
    try {
        ls.setItem(key(baseUrl), payload);
    } catch {
        // Quota exceeded or storage disabled — best-effort
    }
}

// Drop entries no longer referenced by the user's addon list, or stale
// past MAX_AGE_MS. Call once per session with the active addon set.
export function prune(activeBaseUrls) {
    const ls = safeLS();
    if (!ls) return;
    const active = new Set((activeBaseUrls || []).map(u => u.replace(/\/manifest\.json$/, '')));
    const now = Date.now();
    const toRemove = [];
    try {
        for (let i = 0; i < ls.length; i++) {
            const k = ls.key(i);
            if (!k || !k.startsWith(PREFIX)) continue;
            const baseUrl = k.slice(PREFIX.length);
            if (!active.has(baseUrl)) { toRemove.push(k); continue; }
            try {
                const obj = JSON.parse(ls.getItem(k));
                if (!obj?.lastSuccessAt || now - obj.lastSuccessAt > MAX_AGE_MS) {
                    toRemove.push(k);
                }
            } catch {
                toRemove.push(k);
            }
        }
        for (const k of toRemove) ls.removeItem(k);
    } catch {
        // ignore
    }
}
