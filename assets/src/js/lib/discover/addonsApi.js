// Lightweight client wrapper around /stremio/addon-url/:id/refresh-snapshot.
// Used as fire-and-forget from the StremioClient when a fresh manifest
// arrives for an addon whose server-side snapshot is missing or stale.

import { langPath } from '../i18n';

// 7 days. Below this we consider the server snapshot fresh enough not
// to ping for a refresh on every Discover open. Keeps server load low
// while still picking up addons that change capabilities.
const STALE_AFTER_MS = 7 * 24 * 60 * 60 * 1000;

export function isSnapshotStale(fetchedAt) {
    if (!fetchedAt) return true;
    const ts = typeof fetchedAt === 'string' ? Date.parse(fetchedAt) : Number(fetchedAt);
    if (!ts || Number.isNaN(ts)) return true;
    return (Date.now() - ts) > STALE_AFTER_MS;
}

// Asks the server to re-fetch the manifest and persist the snapshot.
// Returns the parsed JSON on success, or null on any failure — callers
// treat this as best-effort.
export async function refreshSnapshot(addonId) {
    if (!addonId) return null;
    try {
        const res = await fetch(langPath(`/stremio/addon-url/${addonId}/refresh-snapshot`), {
            method: 'POST',
            headers: {
                'Accept': 'application/json',
                'X-Requested-With': 'XMLHttpRequest',
                'X-CSRF-TOKEN': window._CSRF,
            },
        });
        if (!res.ok) return null;
        return await res.json();
    } catch {
        return null;
    }
}
