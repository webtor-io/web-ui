import * as adultReveal from '../lib/adultReveal';
adultReveal.install();

if (!HTMLFormElement.prototype.requestSubmit) {
    HTMLFormElement.prototype.requestSubmit = function(submitter) {
        if (submitter) {
            if (!(submitter instanceof HTMLElement) || submitter.type !== 'submit' || submitter.form !== this) {
                throw new TypeError('The specified element is not a submit button');
            }
            submitter.click();
            return;
        }
        submitter = document.createElement('input');
        submitter.type = 'submit';
        submitter.hidden = true;
        this.appendChild(submitter);
        submitter.click();
        this.removeChild(submitter);
    };
}

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
    const { eventDefaults } = await import('../lib/trackContext');
    const umami = (await import('../lib/umami')).init(window, {
        ...window._umami,
        defaultData: eventDefaults,
    });
    window.umami = umami;
    // Identify the session here, once, before any view-specific script (nav,
    // discover, action/*, …) fires its own track events. Server-side gin
    // session cookie ID is rendered to window._sessionID — same value across
    // anon → auth → Patreon → return, so the resulting distinct_id ties the
    // whole funnel together. umami.identify hashes it to a UUID under the
    // hood (Umami v2 validation requirement) — see lib/umami.js.
    if (window._sessionID) {
        umami.identify(window._sessionID);
    }
}

window.progress = {
    show: showProgress,
    hide: hideProgress,
};

import {bindAsync} from '../lib/async';
import initAsyncView from '../lib/asyncView';
import loadAsyncView from '../lib/loadAsyncView';
import toast from '../lib/toast';

document.body.style.display = 'flex';
hideProgress();

// Lang switcher: flip <html lang> synchronously on click — before the
// async-target fetch starts — so any client-side code reading
// `document.documentElement.lang` between click and response (e.g. view
// scripts re-initializing their i18n bundle) sees the new value. Capture
// phase so we beat the bindAsync click listener.
document.addEventListener('click', (e) => {
    const link = e.target.closest && e.target.closest('a[data-lang]');
    if (!link) return;
    const lang = link.getAttribute('data-lang');
    if (lang && document.documentElement.lang !== lang) {
        document.documentElement.lang = lang;
    }
}, true);
bindAsync({
    async fetch(f, url, fetchParams) {
        showProgress();
        fetchParams.headers['X-CSRF-TOKEN'] = window._CSRF;
        fetchParams.headers['X-SESSION-ID'] = window._sessionID;
        const res = await fetch(url, fetchParams);
        hideProgress();
        try {
            const u = new URL(res.url);
            if (u.searchParams.get('status') === 'success') {
                const msg = u.searchParams.get('message');
                if (msg) toast.success(msg);
            }
        } catch(e) { /* ignore URL parse errors */ }
        return res;
    },
    update(key, val) {
        if (key === 'title') document.querySelector('title').innerText = val;
        if (key === 'description') updateDescription(val);
        if (key === 'nav') {
            const nav = document.querySelector('nav');
            if (nav) loadAsyncView(nav, val);
        }
        if (key === 'footer') {
            const footer = document.getElementById('footer');
            if (footer) loadAsyncView(footer, val);
        }
        if (key === 'lang') {
            if (val && document.documentElement.lang !== val) {
                // Language changed via async nav. Sync <html lang> so the
                // next call to getLang() (lib/i18n.js) reads the new value.
                // Per-view i18n modules (lib/discover/i18n.js,
                // lib/player/i18n.js) observe this on their next init() call
                // — which fires on every async nav via av/asyncView — and
                // reload the new locale's message bundle.
                document.documentElement.lang = val;
            }
        }
    },
    fallback: {
        selector: 'main',
        layout: '{{ template "main" . }}',
    },
});
initAsyncView();