import av from '../../lib/av';
import DragDrop from '../../lib/dragAndDrop';
import { init as initI18n, t, tf } from '../../lib/profile/i18n';

av(async function(){
    await initI18n();

    new DragDrop({
        listSelector: '#indexer-list',
        itemSelector: '.indexer-item',
        orderInputSelector: '#indexer_order',
        dataAttribute: 'data-indexer-id'
    });

    const deletedIndexers = new Set();
    const deletedIndexersInput = document.getElementById('deleted_indexers');

    function handleDeleteIndexer(event) {
        const button = event.target.closest('.delete-indexer');
        if (!button) return;

        const indexerId = button.getAttribute('data-indexer-id');
        const indexerItem = button.closest('.indexer-item');
        // The row renders either "name + url" (caps probe succeeded) or a
        // bare URL, so match whichever label container is present.
        const labelEl = indexerItem.querySelector('.font-semibold')
            || indexerItem.querySelector('.font-medium');
        const label = labelEl ? labelEl.textContent.trim() : '';

        if (confirm(tf('profile.indexers.deleteConfirm', label))) {
            if (window.umami) {
                window.umami.track('torznab-indexer-delete');
            }

            deletedIndexers.add(indexerId);
            deletedIndexersInput.value = Array.from(deletedIndexers).join(',');
            indexerItem.remove();

            if (window.toast) window.toast.success(t('profile.indexers.deleted'));

            const orderInput = document.getElementById('indexer_order');
            const currentOrder = orderInput.value.split(',').filter(id => !deletedIndexers.has(id));
            orderInput.value = currentOrder.join(',');

            const indexerList = document.getElementById('indexer-list');
            const remaining = indexerList.querySelectorAll('.indexer-item[data-indexer-id]');
            if (remaining.length === 0) {
                const emptyState = document.getElementById('indexer-empty-state');
                if (emptyState) {
                    emptyState.classList.remove('hidden');
                }
            }
        }
    }

    // Per-indexer "Refresh" — re-probes t=caps server-side. Capabilities
    // change whenever the user adds trackers to their Jackett/Prowlarr, and
    // the query shape we send depends on them.
    async function handleRefreshIndexer(event) {
        const button = event.target.closest('.refresh-indexer');
        if (!button) return;
        event.preventDefault();
        const indexerId = button.getAttribute('data-indexer-id');
        if (!indexerId) return;

        const indexerItem = button.closest('.indexer-item');
        button.disabled = true;
        button.classList.add('animate-spin');
        try {
            const res = await fetch(`/torznab/indexer/${indexerId}/refresh-caps`, {
                method: 'POST',
                headers: {
                    'Accept': 'application/json',
                    'X-Requested-With': 'XMLHttpRequest',
                    'X-CSRF-TOKEN': window._CSRF,
                },
            });
            if (!res.ok) {
                const data = await res.json().catch(() => ({}));
                if (window.toast) window.toast.error(data.error || t('profile.indexers.refreshFailed'));
                return;
            }
            const data = await res.json();
            const nameEl = indexerItem.querySelector('.font-semibold');
            if (nameEl && data.name) nameEl.textContent = data.name;
            if (window.toast) window.toast.success(t('profile.indexers.refreshed'));
        } catch (e) {
            if (window.toast) window.toast.error(t('profile.indexers.refreshFailed'));
        } finally {
            button.disabled = false;
            button.classList.remove('animate-spin');
        }
    }

    const indexerList = document.getElementById('indexer-list');
    if (indexerList) {
        indexerList.addEventListener('click', handleDeleteIndexer);
        indexerList.addEventListener('click', handleRefreshIndexer);
    }
});

export {}
