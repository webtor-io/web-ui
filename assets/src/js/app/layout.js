function showProgress() {
    const progress = document.getElementById('progress');
    progress.classList.remove('hidden');
}
function hideProgress() {
    const progress = document.getElementById('progress');
    progress.classList.add('hidden');
}
function updateDescription(val) {
    const existingDesc = document.querySelector('meta[name="description"]');
    
    if (!val || val.trim() === '') {
        if (existingDesc) {
            existingDesc.remove();
        }
        return;
    }
    
    const tempDiv = document.createElement('div');
    tempDiv.innerHTML = val;
    const newMeta = tempDiv.querySelector('meta[name="description"]');
    
    if (existingDesc && newMeta) {
        existingDesc.setAttribute('content', newMeta.getAttribute('content'));
    } else if (newMeta) {
        const titleTag = document.querySelector('title');
        if (titleTag) {
            titleTag.insertAdjacentElement('afterend', newMeta);
        }
    }
}

if (window._umami) {
    const umami = (await import('../lib/umami')).init(window, window._umami);
    window.umami = umami;

}

window.progress = {
    show: showProgress,
    hide: hideProgress,
};

import {bindAsync} from '../lib/async';
import initAsyncView from '../lib/asyncView';

const initTheme = (await import('../lib/themeSelector')).initTheme;

initTheme(document.querySelector('[data-toggle-theme]'));
document.body.style.display = 'flex';
hideProgress();
bindAsync({
    async fetch(f, url, fetchParams) {
        showProgress();
        fetchParams.headers['X-CSRF-TOKEN'] = window._CSRF;
        fetchParams.headers['X-SESSION-ID'] = window._sessionID;
        const res = await fetch(url, fetchParams);
        hideProgress();
        return res;
    },
    update(key, val) {
        if (key === 'title') document.querySelector('title').innerText = val;
        if (key === 'description') updateDescription(val);
    },
    fallback: {
        selector: 'main',
        layout: '{{ template "main" . }}',
    },
});
initAsyncView();