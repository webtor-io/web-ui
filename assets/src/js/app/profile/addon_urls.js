import av from '../../lib/av';
import { init as initI18n } from '../../lib/profile/i18n';
import { initListEditor } from '../../lib/profile/listEditor';

av(async function(){
    await initI18n();

    initListEditor({
        kind: 'addon',
        i18nPrefix: 'profile.addons',
        umamiDeleteEvent: 'addon-url-delete',
        // Re-fetches the manifest server-side, so a snapshot that went stale
        // can be updated without removing and re-adding the addon.
        refreshUrl: id => `/stremio/addon-url/${id}/refresh-snapshot`,
        onRefreshed: renderAddonSnapshot,
    });
});

// renderAddonSnapshot rebuilds the label area and the logo in place. The
// markup is deliberately tiny: the row also carries the drag handle, the
// refresh/delete buttons and the toggle, which stay untouched.
function renderAddonSnapshot(addonItem, data) {
    const labelEl = addonItem.querySelector('.flex-1.min-w-0');
    const url = addonItem.querySelector('.text-w-muted')?.textContent
        || addonItem.querySelector('.font-medium')?.textContent
        || '';
    if (labelEl && data.name) {
        labelEl.innerHTML = `
            <div class="font-semibold text-sm truncate"></div>
            <div class="text-xs text-w-muted truncate"></div>
            <div class="flex flex-wrap gap-1 mt-1.5"></div>
        `;
        labelEl.querySelector('.font-semibold').textContent = data.name;
        labelEl.querySelector('.text-w-muted').textContent = url;
        const resBox = labelEl.querySelector('.flex-wrap');
        for (const r of (data.resources || [])) {
            const span = document.createElement('span');
            span.className = 'text-[10px] px-1.5 py-0.5 rounded bg-w-cyan/10 text-w-cyan font-medium';
            span.textContent = r;
            resBox.appendChild(span);
        }
    }
    // Refresh the logo: drop the previous <img>, set the new initial, and add
    // a fresh <img> when the new snapshot has a logo URL. The initial span is
    // always present and shows through if the image fails to load.
    const logoEl = addonItem.querySelector('.addon-logo');
    if (!logoEl) return;
    const initialSpan = logoEl.querySelector('.logo-initial');
    const newInitial = (data.name || '?').slice(0, 1).toUpperCase();
    if (initialSpan) initialSpan.textContent = newInitial;
    logoEl.dataset.initial = newInitial;
    logoEl.querySelector('img')?.remove();
    if (data.logo) {
        const img = document.createElement('img');
        img.src = data.logo;
        img.alt = '';
        img.loading = 'lazy';
        img.referrerPolicy = 'no-referrer';
        img.className = 'relative w-full h-full object-contain bg-base-200';
        img.onerror = () => img.remove();
        logoEl.appendChild(img);
    }
}

export {}
