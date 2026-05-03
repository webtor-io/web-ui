import loadAsyncView from "./loadAsyncView";

if (!window.__popstateFilters) window.__popstateFilters = [];
export function addPopstateFilter(fn) {
    window.__popstateFilters.push(fn);
    return () => {
        const i = window.__popstateFilters.indexOf(fn);
        if (i >= 0) window.__popstateFilters.splice(i, 1);
    };
}

async function asyncFetch(url, targetSelector, fetchParams, params, options) {
    let target;
    if (typeof targetSelector === 'string' || targetSelector instanceof String) {
        target = document.querySelector(targetSelector);
    } else if (targetSelector instanceof HTMLElement) {
        target = targetSelector;
    } else {
        throw `Wrong type of target ${targetSelector}`;
    }
    let layout;
    if (!target) {
        target = document.querySelector(params.fallback.selector);
        layout = params.fallback.layout;
    } else {
        layout = target.getAttribute('data-async-layout');
    }
    const updateHeaders = {};
    const updateFields = [];
    for (const an of target.getAttributeNames()) {
        if (an.startsWith('data-async-update-')) {
            const f = an.replace('data-async-update-', '');
            const key = 'X-Update-' + f;
            updateFields.push(f);
            updateHeaders[key] = target.getAttribute(an);
        }
    }
    if (!fetchParams) fetchParams = {};
    if (!fetchParams.headers) fetchParams.headers = {};
    fetchParams.headers = Object.assign(fetchParams.headers, {
        'X-Requested-With': 'XMLHttpRequest',
        'X-Layout': layout,
        'X-Return-Url': window.location.pathname + window.location.search,
    }, updateHeaders);
    let fetchFunc = fetch;
    if (params.fetch) {
        const oldFetch = fetch;
        fetchFunc = function(url, fetchParams) {
            return params.fetch(oldFetch, url, fetchParams);
        }
    }
    const res = await fetchFunc(url, fetchParams);
    const text = await res.text();
    const fragments = parseFragments(text);
    loadAsyncView(target, fragments.main ?? text, options);
    for (const f of updateFields) {
        params.update(f, f in fragments ? fragments[f] : null);
    }
    return res;
}

// Parse the AJAX response body into a {name: html} map.
// Wire format: a series of <template data-async-fragment="NAME">…</template>
// blocks, one per layout slot (main, title, description, nav, footer, lang…).
// See services/template/template.go HTML() for the producer side.
function parseFragments(text) {
    const out = {};
    const doc = new DOMParser().parseFromString(text, 'text/html');
    for (const tpl of doc.querySelectorAll('template[data-async-fragment]')) {
        out[tpl.getAttribute('data-async-fragment')] = tpl.innerHTML;
    }
    return out;
}

async function async(selector, params = {}, scope = null) {
    if (!scope) {
        scope = document;
        window.addEventListener('popstate', async function(e) {
            if (window.__popstateFilters.some(fn => fn(e))) return;
            if (e.state && e.state.targetSelector && e.state.url && e.state.layout && e.state.context && params.history && e.state.context === params.history.context) {
                await asyncFetch(
                    e.state.url,
                    e.state.targetSelector,
                    e.state.fetchParams,
                    params,
                );
            }
        });
        window.addEventListener('async', function(e) {
            async(selector, params, e.detail.target);
        });
    }
    const els = scope.querySelectorAll(selector);
    for (const el of els) {
        el.reload = function() {
            let {url, fetchParams} = params.fetchParams.call(el);
            return asyncFetch.call(el, url, el, fetchParams, params);
        }
        // In case if reload was already invoked
        if (el.reloadResolve) {
            const res = await el.reload();
            el.reloadResolve(res);
        }
        if (!el.getAttribute('data-async-target')) continue;
        // Skip already-bound elements (e.g. on rebind after Preact render)
        if (el._asyncBound) continue;
        el._asyncBound = true;
        el.addEventListener(params.event, async function(e) {
            e.preventDefault();
            e.stopPropagation();
            let history = true;
            if (el.getAttribute('data-async-push-state') && el.getAttribute('data-async-push-state') === 'false') {
                history = false;
            }
            // Per-element opt-out for the scroll-to-top that `data-async-scroll-top`
            // targets normally trigger. Useful for small in-place toggles that
            // reload the whole main content but shouldn't jump the viewport.
            const noScroll = el.getAttribute('data-async-no-scroll') === 'true';
            const options = { noScroll };
            const targetSelector = this.getAttribute('data-async-target');
            const target = document.querySelector(targetSelector);
            const layout = target.getAttribute('data-async-layout');
            let {url, fetchParams} = params.fetchParams.call(this);
            const push = function(url, fetchParams) {
                if (!history) return;
                window.history.pushState({
                    context: params.history.context,
                    url,
                    fetchParams,
                    targetSelector,
                    layout,
                }, '', url);
            }
            const self = this;
            const fetch = function() {
                return asyncFetch.call(self, url, targetSelector, fetchParams, params, options);
            }
            params.history.wrap(fetch, push, url, fetchParams);
            return false;
        });
    }
}

function asyncForms(p = {}) {
    const params = Object.assign({
        event: 'submit',
        history: {
            context: 'forms',
            async wrap(fetch, push, url, fetchParams) {
                const res = await fetch();
                if (res.status === 200) {
                    const u = new URL(res.url);
                    push(u.pathname + u.search, {
                        headers: fetchParams.headers,
                    });
                }
            }
        },
        fetchParams() {
            let method = 'get';
            if (this.getAttribute('method')) {
                method = this.getAttribute('method').toLowerCase();
            }
            const formData = new FormData(this);
            switch (method) {
                case 'post':
                    return {url: this.action, fetchParams: {
                            method,
                            body: new FormData(this),
                        }
                    };
                case 'get':
                    const u = new URL(this.action);
                    for (const pair of formData.entries()) {
                        u.searchParams.set(pair[0], pair[1]);
                    }
                    return {url: u.toString(), fetchParams: {
                            method,
                        }
                    };
                default:
                    throw new Exception(`method ${method} not supported`);
            }
        },
    }, p);
    async('form', params);
}
function asyncLinks(p = {}) {
    const params = Object.assign({
        event: 'click',
        history: {
            context: 'links',
            async wrap(fetch, push, url, fetchParams) {
                push(url, fetchParams);
                return fetch();
            }
        },
        fetchParams() {
            const url = this.getAttribute('href');
            return {url};
        },
    }, p)
    async('a', params);
}

function asyncLayout(p = {}) {
    const params = Object.assign({
        fetchParams() {
            const url = window.location.href;
            return {url};
        },
    }, p)
    async('*[data-async-layout]', params);
}

export function rebindAsync(target) {
    window.dispatchEvent(new CustomEvent('async', { detail: { target } }));
}

export function bindAsync(params = {}) {
    asyncLinks(params);
    asyncForms(params);
    asyncLayout(params);
}
