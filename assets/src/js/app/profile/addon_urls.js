import av from '../../lib/av';
import DragDrop from '../../lib/dragAndDrop';
import { init as initI18n, t, tf } from '../../lib/profile/i18n';

av(async function(){
    await initI18n();
    // Initialize drag and drop functionality for addon URLs
    new DragDrop({
        listSelector: '#addon-list',
        itemSelector: '.addon-item',
        orderInputSelector: '#addon_order',
        dataAttribute: 'data-addon-id'
    });

    // Handle delete button clicks
    const deletedAddons = new Set();
    const deletedAddonsInput = document.getElementById('deleted_addons');
    
    function handleDeleteAddon(event) {
        const button = event.target.closest('.delete-addon');
        if (!button) return;

        const addonId = button.getAttribute('data-addon-id');
        const addonItem = button.closest('.addon-item');
        // The addon row may render either a "name + url" pair (when the
        // server captured a manifest snapshot) or a bare URL fallback.
        // The URL string is always present, so we match its container
        // and read its textContent regardless of which layout is active.
        const labelEl = addonItem.querySelector('.font-semibold')
            || addonItem.querySelector('.font-medium');
        const addonUrl = labelEl ? labelEl.textContent.trim() : '';
        
        if (confirm(tf('profile.addons.deleteConfirm', addonUrl))) {
            // Track delete event with Umami
            if (window.umami) {
                window.umami.track('addon-url-delete');
            }
            
            // Add to deleted addons set
            deletedAddons.add(addonId);
            
            // Update hidden input
            deletedAddonsInput.value = Array.from(deletedAddons).join(',');
            
            // Remove the element from DOM
            addonItem.remove();

            if (window.toast) window.toast.success(t('profile.addons.deleted'));
            
            // Update the addon order input to remove deleted addon
            const orderInput = document.getElementById('addon_order');
            const currentOrder = orderInput.value.split(',').filter(id => !deletedAddons.has(id));
            orderInput.value = currentOrder.join(',');
            
            // Show empty state if no addons left
            const addonList = document.getElementById('addon-list');
            const remainingAddons = addonList.querySelectorAll('.addon-item[data-addon-id]');
            if (remainingAddons.length === 0) {
                const emptyState = document.getElementById('addon-empty-state');
                if (emptyState) {
                    emptyState.classList.remove('hidden');
                }
            }
        }
    }
    
    // Per-addon "Refresh snapshot" button — re-fetches the manifest
    // server-side and rerenders just the addon name + capabilities row.
    // Lets users force-update an addon whose snapshot has gone stale
    // without removing and re-adding it.
    async function handleRefreshAddon(event) {
        const button = event.target.closest('.refresh-addon');
        if (!button) return;
        event.preventDefault();
        const addonId = button.getAttribute('data-addon-id');
        if (!addonId) return;

        const addonItem = button.closest('.addon-item');
        button.disabled = true;
        button.classList.add('animate-spin');
        try {
            const res = await fetch(`/stremio/addon-url/${addonId}/refresh-snapshot`, {
                method: 'POST',
                headers: {
                    'Accept': 'application/json',
                    'X-Requested-With': 'XMLHttpRequest',
                    'X-CSRF-TOKEN': window._CSRF,
                },
            });
            if (!res.ok) {
                const data = await res.json().catch(() => ({}));
                if (window.toast) window.toast.error(data.error || t('profile.addons.refreshFailed'));
                return;
            }
            const data = await res.json();
            // Rebuild the label area + logo in place. We deliberately
            // keep the markup tiny — the row also has drag handle, the
            // refresh/delete buttons and the toggle, which we leave
            // untouched.
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
            // Refresh the logo: drop the previous <img>, set the new
            // initial, and add a fresh <img> when the new snapshot has
            // a logo URL. The initial span is always present and shows
            // through if the image fails to load.
            const logoEl = addonItem.querySelector('.addon-logo');
            if (logoEl) {
                const initialSpan = logoEl.querySelector('.logo-initial');
                const newInitial = (data.name || '?').slice(0, 1).toUpperCase();
                if (initialSpan) initialSpan.textContent = newInitial;
                logoEl.dataset.initial = newInitial;
                const oldImg = logoEl.querySelector('img');
                if (oldImg) oldImg.remove();
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
            if (window.toast) window.toast.success(t('profile.addons.refreshed'));
        } catch (e) {
            if (window.toast) window.toast.error(t('profile.addons.refreshFailed'));
        } finally {
            button.disabled = false;
            button.classList.remove('animate-spin');
        }
    }

    // Add event listener to the addon list using event delegation
    const addonList = document.getElementById('addon-list');
    if (addonList) {
        addonList.addEventListener('click', handleDeleteAddon);
        addonList.addEventListener('click', handleRefreshAddon);
    }
});

export {}
