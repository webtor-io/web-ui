import executeScriptElements from "./executeScriptElements";
// options (optional): { noScroll?: boolean } — lets callers suppress the
// automatic scroll-to-top that `data-async-scroll-top` targets normally
// trigger. Used by small in-place toggles (e.g. watched mark/unmark buttons)
// that reload a big target but shouldn't jump the user back to the top.
// destroyViews / activateViews are the two halves of a swap, for a caller that
// cannot hand its HTML to loadAsyncView because part of the target is live and
// must survive (the playing player, lib/player/next-item-go.js syncPage).
// `skip` is that part: its views are neither destroyed nor re-initialised.
export function destroyViews(target, skip = null) {
    const els = target.querySelectorAll('[data-async-view]');
    for (const el of els) {
        if (skip && skip.contains(el)) continue;
        const view = el.getAttribute('data-async-view');
        const detail = {
            target: el,
        };
        const event = new CustomEvent(`async:${view}_destroy`, { detail });
        window.dispatchEvent(event);
    }
}

export function activateViews(target, skip = null) {
    executeScriptElements(target, skip);
    // Update async elements
    window.dispatchEvent(new CustomEvent('async', { detail: { target } }));
    for (const script of target.getElementsByTagName('script')) {
        if (script.src === "" || (skip && skip.contains(script))) continue;
        const url = new URL(script.src);
        const name = url.pathname.replace(/\.js$/, '');
        window.dispatchEvent(new CustomEvent('async:' + name, { detail: { target: script.parentElement } }));
    }
}

function loadAsyncView(target, body, options) {
    destroyViews(target);
    renderBody(target, body, options);
}
function renderBody(target, body, options) {
    // SAFETY: `body` is always same-origin server-rendered HTML from our own Go/Gin
    // templates — extracted from <template data-async-fragment> blocks in AJAX
    // responses (async.js, then dispatched to layout.js for nav/footer), or from
    // same-origin SSE messages (progressLog.js). No external or user-supplied
    // HTML reaches here.
    target.innerHTML = body;
    activateViews(target);

    if (target.hasAttribute('data-async-scroll-top') && !(options && options.noScroll)) {
        window.scrollTo({ top: 0 });
    }
}

export default loadAsyncView;