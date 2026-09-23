// The tool-intent line on the resource page (partials/resource/tool_intent):
// "Or download the files directly — no torrent client needed", shown to a
// visitor who submitted the torrent on /magnet-to-torrent.
//
// Its button is a link to #content, so without JS the browser just scrolls to
// the file card and the list. With JS it presses the page's own download
// button instead of posting a form of its own: the Turnstile hook, the busy
// state, the archive's progress modal and the Umami event of that button all
// come with it, and the line keeps no copy of those forms that could drift.
// The button is looked up at click time — picking a file or a directory
// swaps #content/#list and replaces the buttons that were there on load.
//
// The directory archive comes first: someone who wanted the .torrent wanted
// the whole torrent. A single-file torrent has no list, only its file card.

// Which of the page's buttons the line presses: { button, kind } with kind
// "archive" or "file", or null when the page has neither.
export function directDownload(doc) {
    const archive = doc.querySelector('#list form.download-dir button[type=submit]');
    if (archive) return { button: archive, kind: 'archive' };
    const file = doc.querySelector('#file form.download button[type=submit]');
    if (file) return { button: file, kind: 'file' };
    return null;
}

export function initToolIntent(root, doc = document) {
    const link = root.querySelector('[data-tool-intent-direct]');
    if (!link) return;
    link.addEventListener('click', (e) => {
        const target = directDownload(doc);
        // Nothing to press: let the #content anchor scroll.
        if (!target) return;
        e.preventDefault();
        target.button.click();
        // The archive reports into its modal. A file's job log opens at the
        // bottom of its card, which on a phone is below the header this line
        // sits in: bring the card into view (#file carries the scroll margin
        // for the navbar), and leave it alone when it is already on screen.
        // Decided by the kind, not by btn.closest('#file'): partials/file.html
        // ends with a `<div … />`, which HTML does not self-close, so #file is
        // left open and #list ends up inside it.
        if (target.kind !== 'file') return;
        const card = doc.getElementById('file');
        if (card && typeof card.scrollIntoView === 'function') {
            card.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        }
    });
}
