// waitForElement resolves with the first element `find()` returns, now or
// once a DOM mutation makes it appear, or with null after `timeoutMs`.
//
// Written for the resource page's `#action=stream` auto-start: the av()
// queue is drained while the file card that carries the stream form is
// still on its way into the DOM, so a one-shot querySelector at that moment
// finds nothing and the deep link silently does nothing (measured on prod
// 2026-09-16: the hook ran, no form, no POST). Watching for the element
// instead of assuming it is there is the whole fix.
export function waitForElement(find, { root = document.body, timeoutMs = 15000, observe } = {}) {
    const now = find();
    if (now) return Promise.resolve(now);
    return new Promise((resolve) => {
        let done = false;
        const finish = (el) => {
            if (done) return;
            done = true;
            clearTimeout(timer);
            if (obs) obs.disconnect();
            resolve(el);
        };
        const check = () => {
            const el = find();
            if (el) finish(el);
        };
        const Observer = observe || (typeof MutationObserver !== 'undefined' ? MutationObserver : null);
        const obs = Observer ? new Observer(check) : null;
        if (obs) obs.observe(root, { childList: true, subtree: true });
        const timer = setTimeout(() => finish(null), timeoutMs);
        // A mutation may have landed between find() above and observe().
        check();
    });
}
