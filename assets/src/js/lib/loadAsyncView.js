import executeScriptElements from "./executeScriptElements";
// options (optional): { noScroll?: boolean } — lets callers suppress the
// automatic scroll-to-top that `data-async-scroll-top` targets normally
// trigger. Used by small in-place toggles (e.g. watched mark/unmark buttons)
// that reload a big target but shouldn't jump the user back to the top.
function loadAsyncView(target, body, options) {
    const els = target.querySelectorAll('[data-async-view]');
    for (const el of els) {
        const view = el.getAttribute('data-async-view');
        const detail = {
            target: el,
        };
        const event = new CustomEvent(`async:${view}_destroy`, { detail });
        window.dispatchEvent(event);
    }
    renderBody(target, body, options);
}
function renderBody(target, body, options) {
    // SAFETY: `body` is always same-origin server-rendered HTML from our own Go/Gin
    // templates — fetched via XMLHttpRequest from the app's own endpoints (async.js),
    // from same-origin SSE messages (progressLog.js), or from same-origin response
    // headers (layout.js nav updates). No external or user-supplied HTML reaches here.
    target.innerHTML = body;
    executeScriptElements(target);
    const detail = {
        target,
    };
    // Update async elements
    const event = new CustomEvent('async', { detail });
    window.dispatchEvent(event);

    const scripts = target.getElementsByTagName('script');
    for (const script of scripts) {
        if (script.src === "") continue;
        const url = new URL(script.src);
        const name = url.pathname.replace(/\.js$/, '');
        const scriptDetail = {
            target: script.parentElement,
        };
        const event = new CustomEvent('async:' + name, { detail: scriptDetail });
        window.dispatchEvent(event);
    }

    if (target.hasAttribute('data-async-scroll-top') && !(options && options.noScroll)) {
        window.scrollTo({ top: 0 });
    }
}

export default loadAsyncView;