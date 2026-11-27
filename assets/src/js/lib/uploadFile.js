// Deleting a file or a folder of the owner's upload from its page
// (partials/list.html): a write token from web-ui, then a DELETE straight to
// the seeder, the way the files went up.

// deleteUploadFile deletes path from the upload id: a file, or a folder when
// path ends in "/". It answers the DELETE's status, or the token request's
// when that one failed. want is 1, not 0: 0 renews at what the upload holds,
// and an upload holding nothing past its reserve gets no token at all.
export async function deleteUploadFile(tokenURL, id, path, doFetch = fetch) {
    const res = await doFetch(tokenURL, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-TOKEN': window._CSRF,
            'X-SESSION-ID': window._sessionID,
        },
        body: JSON.stringify({ id, want: 1 }),
    });
    if (!res.ok) return res.status;
    const g = await res.json();
    const rel = path.split('/').filter(Boolean).map(encodeURIComponent).join('/') + (path.endsWith('/') ? '/' : '');
    const q = new URLSearchParams({ token: g.token, 'api-key': g.apiKey });
    const del = await doFetch(`${g.url}/${rel}?${q}`, { method: 'DELETE' });
    return del.status;
}

async function onClick(e) {
    const btn = e.target.closest && e.target.closest('[data-upload-delete]');
    if (!btn) return;
    // A file going up in this tab would be cut by the page's reload.
    if (window.__uploadBusy) {
        window.toast?.error(btn.dataset.busy);
        return;
    }
    if (btn.dataset.confirm && !confirm(btn.dataset.confirm)) return;
    let status = 0;
    try {
        status = await deleteUploadFile(btn.dataset.uploadToken, btn.dataset.uploadId, btn.dataset.uploadDelete);
    } catch (err) {
        console.error(err);
    }
    // 410: already gone. Anything else, the seeder's 413 for a deleted list
    // that would not fit included, leaves the file: say so.
    if (status === 204 || status === 410) {
        location.reload();
    } else {
        window.toast?.error(btn.dataset.error);
    }
}

// bindUploadFileDelete wires the delete buttons of the upload's listing once
// per page: a listener on the document outlives the #list swaps.
export function bindUploadFileDelete(doc) {
    if (window.__uploadFileDelete) return;
    window.__uploadFileDelete = true;
    doc.addEventListener('click', onClick);
}
