import loadAsyncView from "./loadAsyncView";
async function asyncFetch(url, targetSelector, fetchParams, params) {
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
    for (const an of target.getAttributeNames()) {
        if (an.startsWith('data-async-update-')) {
            const key = 'X-Update-' + an.replace('data-async-update-', '');
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
    console.log(res.url);
    const text = await res.text();
    loadAsyncView(target, text, params);
    for (const [h, val] of res.headers.entries()) {
        if (!h.startsWith('x-update-')) continue;
        const key = h.replace('x-update-', '');
        let decodedBytes = new Uint8Array([...atob(val)].map(c => c.charCodeAt(0)));
        let decodedHeader = new TextDecoder("utf-8").decode(decodedBytes);
        params.update(key, decodedHeader);
    }
    return res;
}
async function async(selector, params = {}, scope = null) {
    if (!scope) {
        scope = document;
        window.addEventListener('popstate', async function(e) {
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
        el.addEventListener(params.event, async function(e) {
            e.preventDefault();
            e.stopPropagation();
            let history = true;
            if (el.getAttribute('data-async-push-state') && el.getAttribute('data-async-push-state') === 'false') {
                history = false;
            }
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
                return asyncFetch.call(self, url, targetSelector, fetchParams, params);
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

function asyncGet(p = {}) {
    const params = Object.assign({
        fetchParams() {
            const url = this.getAttribute('data-async-get');
            return {url};
        },
    }, p)
    async('*[data-async-get]', params);
}

export function bindAsync(params = {}) {
    asyncLinks(params);
    asyncForms(params);
    asyncGet(params);
}
