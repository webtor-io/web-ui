import { render } from 'preact';
import av from '../lib/av';
import { CINEMETA_BASE } from '../lib/discover/client';
import { DiscoverApp } from '../lib/discover/components/DiscoverApp';

av(function () {
    const container = this;
    const isDiscoverPage = container.id === 'discover-page' ||
        container.querySelector?.('#discover-page');

    if (!isDiscoverPage) {
        // Fallback: old ribbon behavior
        const modal = container.querySelector('#discover-modal');
        if (modal) {
            container.querySelectorAll('.discover-open').forEach(btn => {
                btn.addEventListener('click', () => {
                    modal.showModal();
                    window.umami?.track('discover-modal-shown');
                });
            });
            modal.querySelectorAll('.discover-close').forEach(btn => {
                btn.addEventListener('click', () => modal.close());
            });
        }
        return;
    }

    const addonUrls = [...(window._addonUrls || [])];
    if (!addonUrls.some(u => u.replace(/\/manifest\.json$/, '') === CINEMETA_BASE)) {
        addonUrls.unshift(CINEMETA_BASE);
    }

    const mountEl = container.querySelector('#discover-mount') || container;
    render(<DiscoverApp addonUrls={addonUrls} />, mountEl);
}, function () {
    // Destroy callback: unmount Preact on async navigation away
    const container = this;
    const mountEl = container.querySelector('#discover-mount');
    if (mountEl) render(null, mountEl);
});

export {};
