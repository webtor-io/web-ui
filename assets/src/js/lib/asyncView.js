import {makeDebug} from './debug';
const debug = await makeDebug('webtor:embed:message');
export default function init() {
    if (window.av) {
        for (const data  of window.av) {
            initAsyncView(...data);
        }
    }
    window.av = {
        push(data) {
            initAsyncView(...data);
        }
    }
}
function initAsyncView(target, init, destroy) {
    const scripts = target.getElementsByTagName('script');
    const src = scripts[scripts.length-1].src;
    const url = new URL(src);
    const name = url.pathname.replace(/\.js$/, '');
    target.setAttribute('data-async-view', name);
    const onLoad = function(e) {
        debug(`webtor:async view script loaded name=%o`, name);
        const target = e.detail.target;
        target.setAttribute('data-async-view', name);
        if (!target.reload) {
            target.reload = function() {
                return new Promise(function(resolve, _) {
                    target.reloadResolve = resolve;
                })
            }
        }
        init.call(target);
    }
    const onDestroy = async (e) => {
        debug(`webtor:async view script destroyed name=%o`, name);
        const event = new CustomEvent(`async:${name}_destroyed`);
        if (destroy) {
            let target = document;
            if (e && e.detail && e.detail.target) {
                target = e.detail.target;
            }
            await destroy.call(target);
        }
        window.dispatchEvent(event);
    }
    let key = `__async${name}_loaded`;
    if (!window[key]) {
        window.addEventListener(`async:${name}`, onLoad);
        window.addEventListener(`async:${name}_destroy`, onDestroy);
        window[key] = true;
        onLoad({
            detail: {
                target,
            },
        });
    }

}
