import { render } from 'preact';
import av from '../lib/av';
import { CINEMETA_BASE } from '../lib/discover/client';
import { DiscoverApp } from '../lib/discover/components/DiscoverApp';

av(function () {
    const container = this;
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
