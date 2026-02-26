import { buildCatalogs, getCatalogsForType, getTypes } from './discoverReducer';

// Shared button chip class for Tabs (btn-sm) and FilterChips (btn-xs)
// Size classes must be written as full literals for Tailwind static analysis
export function chipClass(active, size = 'sm') {
    const sizeClass = size === 'xs' ? 'btn-xs' : 'btn-sm';
    return active
        ? `btn ${sizeClass} bg-w-cyan/15 border border-w-cyan/30 text-w-cyan`
        : `btn ${sizeClass} btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan`;
}

// Dedup: modal restore logic used in two effects
export function restoreModalFromUrl(id, url, openModalById, modalEpisodeRef) {
    if (modalEpisodeRef.current) {
        const isRestoredEntry = window.history.state?.context === 'links';
        if (isRestoredEntry) {
            url.withPopstate(() => openModalById(id));
        } else {
            url.replace({ id, season: null, episode: null });
            openModalById(id);
        }
    } else {
        url.withPopstate(() => openModalById(id));
    }
}

// Dedup: manifest loading shared between init and retry
export async function loadManifests(client) {
    const manifests = await client.fetchAllManifests();
    client.manifests = manifests;
    const catalogs = buildCatalogs(manifests);
    const types = getTypes(catalogs);
    return { manifests, catalogs, types };
}
