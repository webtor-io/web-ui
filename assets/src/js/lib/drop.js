import { trackImpressions } from './impression';

// The drop zone of the home page and the tool pages, and the "Add files"
// button of an upload's page. A drop of .torrent files only opens them, as it
// always did; anything else is the user's own files and goes to the uploader
// (lib/upload.js), which is loaded only then and shows its progress in the
// page's upload log (partials/upload/log.html). The state shared with the
// uploader lives on window and in the DOM: this module is bundled twice.

// collect reads a drop's items while the event lasts: the browser empties
// them as soon as it is over.
export function collect(dt) {
    const items = [...(dt.items || [])].filter((i) => i.kind === 'file');
    if (items.length) return items.map((i) => ({ entry: i.webkitGetAsEntry ? i.webkitGetAsEntry() : null, file: i.getAsFile() }));
    return [...(dt.files || [])].map((file) => ({ entry: null, file }));
}

// torrentsOnly: every item is a .torrent file, not a folder.
export function torrentsOnly(items) {
    return items.every(({ entry, file }) => !entry?.isDirectory && /\.torrent$/i.test(file?.name ?? entry?.name ?? ''));
}

const picked = (input) => [...(input.files || [])].map((file) => ({ entry: null, file }));

// showSignIn puts the guest's sign-in prompt in its slot once, where a
// signed-in user's upload shows its progress, and brings it into view: on the
// home page that is above the search field, out of sight of a drop on a phone.
function showSignIn(doc, tpl) {
    const slot = doc.querySelector('.upload-sign-in-slot');
    if (!slot) return;
    if (!slot.firstElementChild) {
        slot.append(tpl.content.cloneNode(true));
        trackImpressions(slot);
    }
    slot.firstElementChild.scrollIntoView?.({ block: 'nearest' });
}

export function initDrop(dropzone, load = () => import('./upload')) {
    // First: the upload page's handler must see a drop before this page's
    // listeners, which outlive an async navigation, cancel it.
    bindPageDrop(document, load);
    const dropzoneInput = dropzone.querySelector('.dropzone-input');
    const host = dropzone.parentElement;
    const uploads = dropzone.hasAttribute('data-uploads');

    dropzone.addEventListener('click', function() {
        dropzoneInput.click();
    });

    ['drag', 'dragstart', 'dragend', 'dragover', 'dragenter', 'dragleave', 'drop'].forEach(function(event) {
        document.addEventListener(event, function(e) {
            e.preventDefault();
            e.stopPropagation();
        });
    });

    const signIn = () => {
        const tpl = document.getElementById('upload-sign-in');
        if (tpl) showSignIn(dropzone.ownerDocument, tpl);
        return !!tpl;
    };
    const busy = () => {
        if (window.__uploadBusy) window.toast?.error(dropzone.dataset.uploadBusy);
        return !!window.__uploadBusy;
    };
    const own = (items) => {
        if (signIn() || busy()) return;
        load().then((m) => m.startUpload(dropzone.ownerDocument.querySelector('[data-upload-log]'), items, {
            upsell: dropzone.hasAttribute('data-upload-upsell'),
            vault: dropzone.hasAttribute('data-upload-vault'),
        }));
    };
    const submit = (files) => {
        dropzoneInput.files = files;
        dropzone.requestSubmit();
    };

    // This listener stays on the document after an async navigation: a zone
    // that has left the page takes no drop.
    document.addEventListener('drop', function(e) {
        if (!dropzone.isConnected) return;
        const dt = e.dataTransfer;
        if (!uploads) return submit(dt.files);
        const items = collect(dt);
        if (torrentsOnly(items)) return submit(dt.files);
        own(items);
    });

    // A picker of own files is emptied once read, so that picking the same
    // files again after an error is a change.
    dropzoneInput.addEventListener('change', function() {
        if (uploads) {
            const items = picked(dropzoneInput);
            if (!torrentsOnly(items)) {
                dropzoneInput.value = '';
                return own(items);
            }
        }
        dropzone.requestSubmit();
    });

    if (!uploads) return;
    const dirInput = host.querySelector('.dropzone-dir');
    host.querySelector('.dropzone-folder')?.addEventListener('click', function() {
        if (signIn() || busy()) return;
        dirInput.click();
    });
    dirInput?.addEventListener('change', function() {
        const items = picked(dirInput);
        if (torrentsOnly(items)) return submit(dirInput.files);
        dirInput.value = '';
        own(items);
    });
}

// destination is the folder of the upload that files added on its page go
// to: the open folder, given as "/<root>/<sub>…", when it is inside root;
// root otherwise. None when the page does not know root: the uploader
// names it then.
function destination(raw, root) {
    if (!root) return undefined;
    const segs = raw.split('/').filter(Boolean);
    if (!segs.length || segs[0] !== root || segs.some((s) => s === '.' || s === '..')) return [root];
    return segs;
}

// addFiles hands items to the uploader as files added to el's upload. The
// open folder is read now: an async navigation of the listing changes it
// under a button that stays.
function addFiles(el, items, load) {
    const card = el.ownerDocument.querySelector('.js-list-card[data-resource-id="' + el.dataset.uploadId + '"]');
    const raw = card ? card.dataset.dir ?? '' : el.dataset.uploadDir ?? '';
    load().then((m) => m.startUpload(el.ownerDocument.querySelector('[data-upload-log]'), items, {
        id: el.dataset.uploadId,
        name: el.dataset.uploadName,
        upsell: el.hasAttribute('data-upload-upsell'),
        dir: destination(raw, el.dataset.uploadName),
    }));
}

// initAddFiles wires "Add files" ([data-upload-add], with its file input
// next to it) on an upload's page. A drop anywhere on the page does the same
// (bindPageDrop).
export function initAddFiles(el, load = () => import('./upload')) {
    holdVaultButton(el.ownerDocument);
    bindPageDrop(el.ownerDocument, load);
    const input = el.nextElementSibling;
    // A second run in the tab would clear the busy flag of the first one
    // when it ends, and a delete could cut the first one's files.
    const busy = () => {
        if (window.__uploadBusy) window.toast?.error(el.dataset.busy);
        return !!window.__uploadBusy;
    };
    el.addEventListener('click', function() {
        if (busy()) return;
        input.click();
    });
    input.addEventListener('change', function() {
        const items = picked(input);
        input.value = '';
        if (busy()) return;
        addFiles(el, items, load);
    });
}

const hasFiles = (e) => [...(e.dataTransfer?.types || [])].includes('Files');

// bindPageDrop makes the whole page of an upload take a drop of files, once
// per window: its listeners outlive async navigations, so they look for the
// add button at the time of the event, and use the latest loader given.
export function bindPageDrop(doc = document, load = () => import('./upload')) {
    const first = !window.__uploadPageDrop;
    window.__uploadPageDrop = load;
    if (!first) return;
    // Without a cancelled dragover the browser sends no drop.
    doc.addEventListener('dragover', function(e) {
        if (hasFiles(e) && doc.querySelector('[data-upload-add]')) e.preventDefault();
    });
    doc.addEventListener('drop', function(e) {
        // Text or a link; or taken deeper, as the subtitles label does.
        if (!hasFiles(e) || e.defaultPrevented) return;
        const el = doc.querySelector('[data-upload-add]');
        if (!el) return;
        // Else the browser opens the file in place of the page.
        e.preventDefault();
        if (window.__uploadBusy) {
            window.toast?.error(el.dataset.busy);
            return;
        }
        addFiles(el, collect(e.dataTransfer), window.__uploadPageDrop);
    });
}

// The mark another tab of this browser keeps fresh while it uploads into
// id (lib/upload.js). A tab that died leaves a mark that goes stale.
export const BUSY_FRESH_MS = 180000;

export function markBusy(id) {
    try { window.localStorage.setItem('upload-busy:' + id, String(Date.now())); } catch { /* no storage */ }
}

export function clearBusy(id) {
    try { window.localStorage.removeItem('upload-busy:' + id); } catch { /* no storage */ }
}

// busyMarked: a fresh mark of id. No mark reads as 0 and garbage as NaN:
// neither is fresh; a mark from the future (a clock set back) is fresh only
// within the same bound.
export function busyMarked(id) {
    try {
        return Math.abs(Date.now() - Number(window.localStorage.getItem('upload-busy:' + id))) < BUSY_FRESH_MS;
    } catch {
        return false;
    }
}

// The button that opens the pledge modal and the one that stores from it.
const HOLD = '#pledge-button button[type="submit"], #pledge-modal form[action$="/vault/add"] button[type="submit"]';

// holdVaultButton keeps "Save to Vault" from sealing an upload that is still
// being written, in this tab or in another one of this browser: a pledge
// closes the upload for writing. Once per window, on the document, so the
// re-rendered pledge button stays covered. A click is cancelled rather than
// the submit: the async form handler does not look at defaultPrevented.
export function holdVaultButton(doc = document) {
    if (window.__uploadVaultGuard) return;
    window.__uploadVaultGuard = true;
    doc.addEventListener('click', function(e) {
        const add = doc.querySelector('[data-upload-add]');
        // closest: the button holds an <svg><path>, a click lands on those.
        if (!add || !e.target.closest?.(HOLD)) return;
        if (window.__uploadBusy || busyMarked(add.dataset.uploadId)) {
            e.preventDefault();
            window.toast?.error(add.dataset.busy);
        }
    }, true);
}
