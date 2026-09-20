// The first history entry has to be restorable like every later one.
//
// An async navigation pushes an entry WITH a state (url, target, layout), and
// Back re-fetches from it. The entry the visit started on had none: the page
// came from the server, nobody called pushState for it. So Back from the
// first async navigation changed the URL and nothing else -- the address bar
// said "/" and the screen still showed the page the viewer had left.
//
// Found 2026-09-20 from the other end: 500s on "/" and on tool pages, ~40 a
// day. A resource page stranded under "/" keeps running, and the moment one
// of its components reloads itself (the status badge renewing its token, the
// library button) it asks for window.location with ITS layout -- "/" rendered
// through `resource/status_inner`, a template the index view does not have.
//
// Seeded once per document, only if nothing else has claimed the entry.
export function seedInitialEntry(win = window, doc = document, context = 'links') {
    try {
        if (win.history.state) return false;
        const main = doc.querySelector('main[data-async-layout]');
        if (!main) return false; // embed, error pages: nothing to restore into
        win.history.replaceState({
            context,
            url: win.location.pathname + win.location.search,
            fetchParams: undefined,
            targetSelector: 'main',
            layout: main.getAttribute('data-async-layout'),
        }, '', win.location.href);
        return true;
    } catch (e) {
        return false; // sandboxed iframe: history is not ours to write
    }
}
