// Shared share-resource handler. Used by both the header share button
// (inline onclick in resource/get.html) and the in-player share button
// (Player.jsx via direct import).
//
// Same UTM contract (utm_source=webtor, utm_medium=share,
// utm_campaign=resource_share) — keeps the existing arrival-attribution
// queries valid. Fires `share-resource` Umami event with `{location}`
// tag so we can A/B the two placements.

export function shareResource(opts = {}) {
    const location = opts.location || 'header';
    const u = new URL(opts.url || window.location.href);
    u.searchParams.set('utm_source', 'webtor');
    u.searchParams.set('utm_medium', 'share');
    u.searchParams.set('utm_campaign', 'resource_share');
    const url = u.toString();
    const title = opts.title || document.title;

    if (window.umami) window.umami.track('share-resource', { location });

    const tryNativeShare = () => {
        if (!navigator.share) return false;
        navigator.share({ title, url }).catch(() => {});
        return true;
    };

    const openDialog = () => {
        const d = document.getElementById('share-dialog');
        const input = document.getElementById('share-url');
        if (d && input && typeof d.showModal === 'function') {
            input.value = url;
            d.showModal();
            return true;
        }
        return false;
    };

    const copyToClipboard = () => {
        if (!navigator.clipboard) return false;
        navigator.clipboard.writeText(url).then(() => {
            const dlg = document.getElementById('share-dialog');
            const msg = dlg?.dataset.linkCopiedText || 'Link copied';
            if (window.toast) window.toast.success(msg);
        }).catch(() => {});
        return true;
    };

    // iOS Safari: navigator.share() from inside fullscreen <video> either
    // silently fails or unexpectedly kicks out of fullscreen mid-share.
    // Exit fullscreen first so the share sheet lands on a clean DOM —
    // user can re-enter via the player's own controls after sharing.
    if (document.fullscreenElement && navigator.share) {
        document.exitFullscreen().then(() => {
            if (!tryNativeShare()) {
                if (!openDialog()) copyToClipboard();
            }
        }).catch(() => {
            if (!tryNativeShare()) {
                if (!openDialog()) copyToClipboard();
            }
        });
        return;
    }

    if (!tryNativeShare()) {
        if (!openDialog()) copyToClipboard();
    }
}

export function copyShareUrl() {
    const input = document.getElementById('share-url');
    if (!input) return;
    if (!navigator.clipboard) return;
    navigator.clipboard.writeText(input.value).then(() => {
        const dlg = document.getElementById('share-dialog');
        const msg = dlg?.dataset.linkCopiedText || 'Link copied';
        if (window.toast) window.toast.success(msg);
        if (window.umami) window.umami.track('share-copy-url');
        if (dlg) dlg.close();
    });
}

// Expose for inline onclick="shareResource()" / onclick="copyShareUrl()"
// in templates rendered before this module loads.
if (typeof window !== 'undefined') {
    window.shareResource = shareResource;
    window.copyShareUrl = copyShareUrl;
}
