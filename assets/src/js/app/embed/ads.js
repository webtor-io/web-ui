import av from '../../lib/av';

av(async function() {
    if (window._ads === undefined) return;
    const renderAd = (await import('../../lib/ads')).default;
    for (const ad of window._ads) {
        renderAd(this, ad);
    }
});
