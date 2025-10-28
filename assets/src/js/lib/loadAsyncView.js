import executeScriptElements from "./executeScriptElements";
function loadAsyncView(target, body) {
    const els = target.querySelectorAll('[data-async-view]');
    for (const el of els) {
        const view = el.getAttribute('data-async-view');
        const detail = {
            target: el,
        };
        const event = new CustomEvent(`async:${view}_destroy`, { detail });
        window.dispatchEvent(event);
    }
    renderBody(target, body);
}
function renderBody(target, body) {
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
        const event = new CustomEvent('async:' + name, { detail });
        window.dispatchEvent(event);
    }

    // Process async views
    // const yOffset = -100;
    // const y = target.getBoundingClientRect().top + window.scrollY + yOffset;
    //
    // window.scrollTo({ top: y });
}

export default loadAsyncView;