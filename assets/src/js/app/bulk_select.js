import av from '../lib/av';

// "Remove selected" on the library and Vault pages (partials/bulk_select.html).
// Select mode is the data-bulk-on attribute on this view's root; the markup
// shows and hides itself from it. The boxes carry no name: the selection goes
// as one comma-joined field (a multipart part per box is cut off at 1000
// parts without a word, web.FormBulkIDs). A list re-rendered inside the root
// (the Vault table renewing its status tokens; the library's filters swap the
// whole main, and they are hidden in select mode anyway)
// comes back unchecked, so the chosen ids are kept by this root's view (a
// closure per init, not module scope -- CLAUDE.md) and put back on the boxes
// still there.

// web.MaxBulkIDs: the server refuses more. Select all stops here, a library
// larger than this goes in rounds.
const MAX = 1000;

av(function () {
    const root = this;
    const form = root.querySelector('#bulk-remove');
    const toggle = root.querySelector('.js-bulk-toggle');
    if (!form || !toggle) return;
    const submit = form.querySelector('.js-bulk-submit');
    const count = form.querySelector('.js-bulk-count');
    const ids = form.querySelector('.js-bulk-ids');
    const sel = new Set();
    const boxes = () => root.querySelectorAll('input.js-bulk-box:not(:disabled)');

    function sync() {
        const present = new Set();
        for (const b of boxes()) {
            present.add(b.value);
            b.checked = sel.has(b.value);
        }
        for (const id of sel) if (!present.has(id)) sel.delete(id);
        ids.value = [...sel].join(',');
        submit.disabled = sel.size === 0;
        count.textContent = sel.size ? ` (${sel.size})` : '';
    }
    function setMode(on) {
        root.toggleAttribute('data-bulk-on', on);
        if (!on) sel.clear();
        sync();
    }

    toggle.classList.remove('hidden');
    toggle.addEventListener('click', () => setMode(true));
    form.querySelector('.js-bulk-cancel').addEventListener('click', () => setMode(false));
    form.querySelector('.js-bulk-all').addEventListener('click', () => {
        for (const b of boxes()) {
            if (sel.size >= MAX) break;
            sel.add(b.value);
        }
        sync();
    });
    root.addEventListener('change', (e) => {
        const b = e.target;
        if (!b.matches('input.js-bulk-box')) return;
        if (b.checked && sel.size < MAX) sel.add(b.value);
        else sel.delete(b.value);
        sync();
    });
    // A click on the button, not the form's submit: lib/async.js takes the
    // submit event and does not look at defaultPrevented, so a "no" there
    // would still send the form. The event is sent here, after the "yes":
    // a data-umami-event would count every press, the declined ones too.
    submit.addEventListener('click', (e) => {
        if (!sel.size || !confirm(root.dataset.bulkConfirm)) {
            e.preventDefault();
            return;
        }
        if (window.umami) window.umami.track('bulk-remove', { path: submit.dataset.path, count: sel.size });
    });
    const onSwap = (e) => {
        const t = e.detail && e.detail.target;
        if (t && t !== root && root.contains(t)) sync();
    };
    window.addEventListener('async', onSwap);
    root._bulkOff = () => window.removeEventListener('async', onSwap);
    sync();
}, function () {
    if (this._bulkOff) this._bulkOff();
});
