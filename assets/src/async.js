const invokedScripts = {};
const loadedViews = {};
function getScriptName(script) {
    const src = script.getAttribute('src');
    if (src) {
        return src;
    }
    return;
}
// https://stackoverflow.com/a/69190644
function executeScriptElements(containerElement) {
    const scriptElements = containerElement.querySelectorAll('script');
  
    Array.from(scriptElements).forEach((scriptElement) => {
        const name = getScriptName(scriptElement);
        if (name) {
            if (invokedScripts[name]) {
                return;
            } else {
                invokedScripts[name] = true;
            }
        }
    
        const clonedElement = document.createElement('script');
  
        Array.from(scriptElement.attributes).forEach((attribute) => {
            clonedElement.setAttribute(attribute.name, attribute.value);
        });
      
        clonedElement.text = scriptElement.text;
  
        scriptElement.parentNode.replaceChild(clonedElement, scriptElement);
    });
}

window.addEventListener('DOMContentLoaded', (event) => {
    const scripts = document.querySelectorAll('script');
    for (const s of scripts) {
        const name = getScriptName(s);
        if (!name) continue;
        invokedScripts[name] = true;
    }
});

async function asyncFetch(url, selector, targetSelector, layout, fetchParams, params) {
    let target = document.querySelector(targetSelector);
    if (!target) {
        target = document.querySelector(params.fallback.selector);
        layout = params.fallback.layout;
    }
    if (!fetchParams) fetchParams = {};
    if (!fetchParams.headers) fetchParams.headers = {};
    fetchParams.headers = Object.assign(fetchParams.headers, {
        'X-Requested-With': 'XMLHttpRequest',
        'X-Layout': layout,
    });
    if (params.before) params.before();
    const res = await fetch(url, fetchParams);
    const text = await res.text();
    target.innerHTML = text;
    executeScriptElements(target);
    if (params.after) params.after();
    const template = res.headers.get('X-Template');
    const detail = {
        target,
        layout,
    };
    const event = new CustomEvent('async', { detail });
    window.dispatchEvent(event);
    if (template) {
        const event = new CustomEvent('async:'+template, { detail });
        window.dispatchEvent(event);
        if (!loadedViews[template]) {
            window.addEventListener(`async:${template}_loaded`, async function(e) {
                loadedViews[template] = true;
                window.dispatchEvent(event);
            });
        }
    }
    return res;
}
function async(selector, params = {}, scope = null) {
    if (!scope) {
        scope = document;
        window.addEventListener('popstate', async function(e) {
            if (e.state && e.state.targetSelector && e.state.url && e.state.layout && e.state.context && e.state.context == params.history.context) {
                await asyncFetch(
                    e.state.url,
                    selector,
                    e.state.targetSelector,
                    e.state.layout,
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
        if (!el.getAttribute('async-target')) continue;
        el.addEventListener(params.event, async function(e) {
            e.preventDefault();
            e.stopPropagation();
            let history = true;
            if (el.getAttribute('async-push-state') && el.getAttribute('async-push-state') == 'false') {
                history = false;
            }
            const targetSelector = this.getAttribute('async-target');
            let layout = this.getAttribute('async-layout');
            if (!layout) layout = 'async';
            let {url, fetchParams} = params.fetchParams.call(this);
            const push = function(url, fetchParams) {
                if (!history) return;
                window.history.pushState({
                    context: params.history.context,
                    url,
                    fetchParams,
                    targetSelector: targetSelector,
                    layout,
                }, '', '/#' + url);
            }
            const self = this;
            const fetch = function() {
                return asyncFetch.call(self, url, selector, targetSelector, layout, fetchParams, params, scope);
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
                if (res.status == 200) {
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
                method = this.getAttribute('method'); 
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


export default function(tags = [], params = {}) {
    if (tags.includes('a')) asyncLinks(params);
    if (tags.includes('form')) asyncForms(params);
}
