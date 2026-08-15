// Shared behaviour of the profile's drag-and-drop settings lists — Stremio
// addon URLs and Torznab indexers.
//
// Both render the same shape: a sortable list of rows, a per-row delete that
// only stages the removal (the form submit is what applies it), a per-row
// refresh that re-probes the upstream server-side, and an empty state that
// appears once the last row is gone. Only the DOM update after a successful
// refresh differs, which is why that is the one callback.
//
// The element ids and class names are derived from `kind` rather than passed
// in, because the templates already follow that convention throughout:
//
//   #<kind>-list  .<kind>-item  #<kind>_order  #deleted_<kind>s
//   #<kind>-empty-state  .delete-<kind>  .refresh-<kind>  data-<kind>-id

import DragDrop from '../dragAndDrop';
import { t, tf } from './i18n';

export function initListEditor({ kind, plural, i18nPrefix, umamiDeleteEvent, refreshUrl, onRefreshed }) {
    const list = document.getElementById(`${kind}-list`);
    if (!list) return;

    const idAttr = `data-${kind}-id`;
    const orderInput = document.getElementById(`${kind}_order`);
    const deletedInput = document.getElementById(`deleted_${plural || kind + 's'}`);

    new DragDrop({
        listSelector: `#${kind}-list`,
        itemSelector: `.${kind}-item`,
        orderInputSelector: `#${kind}_order`,
        dataAttribute: idAttr,
    });

    const deleted = new Set();

    // rowLabel reads whichever label container the row rendered: rows show
    // "name + url" when the upstream probe succeeded and a bare URL when it
    // did not.
    function rowLabel(item) {
        const el = item.querySelector('.font-semibold') || item.querySelector('.font-medium');
        return el ? el.textContent.trim() : '';
    }

    function handleDelete(event) {
        const button = event.target.closest(`.delete-${kind}`);
        if (!button) return;

        const id = button.getAttribute(idAttr);
        const item = button.closest(`.${kind}-item`);
        if (!confirm(tf(`${i18nPrefix}.deleteConfirm`, rowLabel(item)))) return;

        if (window.umami && umamiDeleteEvent) window.umami.track(umamiDeleteEvent);

        // Staged, not applied: the row leaves the DOM and its id rides in a
        // hidden field until the form is submitted.
        deleted.add(id);
        if (deletedInput) deletedInput.value = Array.from(deleted).join(',');
        item.remove();
        if (window.toast) window.toast.success(t(`${i18nPrefix}.deleted`));

        if (orderInput) {
            orderInput.value = orderInput.value.split(',').filter(v => !deleted.has(v)).join(',');
        }
        if (list.querySelectorAll(`.${kind}-item[${idAttr}]`).length === 0) {
            document.getElementById(`${kind}-empty-state`)?.classList.remove('hidden');
        }
    }

    async function handleRefresh(event) {
        const button = event.target.closest(`.refresh-${kind}`);
        if (!button) return;
        event.preventDefault();
        const id = button.getAttribute(idAttr);
        if (!id || !refreshUrl) return;

        const item = button.closest(`.${kind}-item`);
        button.disabled = true;
        button.classList.add('animate-spin');
        try {
            const res = await fetch(refreshUrl(id), {
                method: 'POST',
                headers: {
                    'Accept': 'application/json',
                    'X-Requested-With': 'XMLHttpRequest',
                    'X-CSRF-TOKEN': window._CSRF,
                },
            });
            if (!res.ok) {
                const data = await res.json().catch(() => ({}));
                if (window.toast) window.toast.error(data.error || t(`${i18nPrefix}.refreshFailed`));
                return;
            }
            const data = await res.json();
            if (onRefreshed) onRefreshed(item, data);
            if (window.toast) window.toast.success(t(`${i18nPrefix}.refreshed`));
        } catch (e) {
            if (window.toast) window.toast.error(t(`${i18nPrefix}.refreshFailed`));
        } finally {
            button.disabled = false;
            button.classList.remove('animate-spin');
        }
    }

    list.addEventListener('click', handleDelete);
    list.addEventListener('click', handleRefresh);
}
