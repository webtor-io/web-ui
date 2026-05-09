import { render } from 'preact';
import av from '../lib/av';
import { CINEMETA_BASE } from '../lib/discover/client';
import { DiscoverApp } from '../lib/discover/components/DiscoverApp';
import { init as initI18n } from '../lib/discover/i18n';

av(async function () {
    await initI18n();
    const container = this;
    const serverAddons = window._addons || [];

    // Build the list of base URLs (Cinemeta first when not user-managed)
    // and the matching seed entries for the StremioClient. Cinemeta has
    // no DB row — we synthesize a seed inline so the AddonHealthChip and
    // selector know it serves catalog/meta even before the manifest fetch
    // round-trip completes.
    const cinemetaSeed = {
        id: '',
        url: CINEMETA_BASE,
        name: 'Cinemeta',
        resources: ['catalog', 'meta'],
        types: ['movie', 'series'],
        fetchedAt: null,
    };

    const seeds = [...serverAddons];
    const hasCinemeta = seeds.some(a => (a.url || '').replace(/\/manifest\.json$/, '') === CINEMETA_BASE);
    if (!hasCinemeta) seeds.unshift(cinemetaSeed);

    const addonUrls = seeds.map(a => (a.url || '').replace(/\/manifest\.json$/, '')).filter(Boolean);
    const hasCustomAddons = serverAddons.length > 0;
    const mountEl = container.querySelector('#discover-mount') || container;
    render(<DiscoverApp addonUrls={addonUrls} addonSeeds={seeds} hasCustomAddons={hasCustomAddons} />, mountEl);
}, function () {
    // Destroy callback: unmount Preact on async navigation away
    const container = this;
    const mountEl = container.querySelector('#discover-mount');
    if (mountEl) render(null, mountEl);
});

export {};
