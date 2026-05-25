// Poster URL rewriter for users who've opted into showing adult
// content unblurred (UserSettings.ShowAdult=true). The server emits
// the default `/lib/poster/<rid>/<file>` URLs in every template; this
// hook swaps them to the auth-gated `/lib/poster/raw/<rid>/<file>`
// variant in-place so the unblurred variant gets fetched on first
// render. Brief flash of blurred-then-clear is acceptable for an
// opt-in minority — the alternative (per-template server-side
// branching on UserSettings) would touch every poster-emitting
// template across library, continue-watching, and the resource page.
//
// Idempotent: skips URLs that already have `/poster/raw/` in them,
// so re-running on async-loaded fragments doesn't double-rewrite.
function rewriteAdultPosters(root) {
    if (!window._showAdult) return;
    const target = root || document;
    target.querySelectorAll('img[src*="/lib/poster/"]').forEach((img) => {
        // Skip the OG share canvas — defence-in-depth in case any
        // template ever embeds it as an <img>. OG images go to
        // third parties (Telegram, Twitter, Stremio addon clients)
        // which haven't opted in and must always see blurred adult.
        // The endpoint also auth-gates /raw/ so an unauth share-render
        // would 403, but we'd rather not even attempt the rewrite.
        if (img.src.indexOf('/og.jpg') !== -1) return;
        if (img.src.indexOf('/lib/poster/raw/') !== -1) return;
        img.src = img.src.replace('/lib/poster/', '/lib/poster/raw/');
    });
    // The 18+ badge is paired with server-side blur — when the user
    // has opted in to see unblurred, the badge would just clutter
    // every card. Templates mark the overlay-wrapper with
    // .w-adult-badge so we can remove it in one sweep.
    target.querySelectorAll('.w-adult-badge').forEach((el) => el.remove());
}
document.addEventListener('DOMContentLoaded', () => rewriteAdultPosters());
// data-async fragments mount via custom events; rewrite there too so
// continue-watching reloads / library re-renders pick up the swap.
document.addEventListener('async-loaded', (e) => rewriteAdultPosters(e.target));

// Per-card click-to-reveal for adult posters. Persists per-resource
// reveals in localStorage so a user who taps "18+" on a card sees
// the unblurred image on every subsequent page-load without flipping
// the global show_adult preference. Disabled for:
//   - users who've already opted in globally (handled by rewriteAdultPosters)
//   - anonymous users (the /raw/ endpoint requires auth; tapping
//     would just 401 with no useful UX, so we suppress the click target)
const ADULT_REVEAL_STORAGE_KEY = 'w-adult-revealed';
const ADULT_REVEAL_MAX = 500;

function loadAdultReveals() {
    try {
        const arr = JSON.parse(localStorage.getItem(ADULT_REVEAL_STORAGE_KEY) || '[]');
        return new Set(Array.isArray(arr) ? arr : []);
    } catch (e) {
        return new Set();
    }
}

function saveAdultReveals(set) {
    // Bound the storage so a power-user tapping reveals across years
    // of activity doesn't grow the entry past the localStorage 5MB
    // quota. Drop oldest first.
    const arr = [...set];
    if (arr.length > ADULT_REVEAL_MAX) {
        arr.splice(0, arr.length - ADULT_REVEAL_MAX);
    }
    try {
        localStorage.setItem(ADULT_REVEAL_STORAGE_KEY, JSON.stringify(arr));
    } catch (e) {
        // Quota exceeded or storage disabled — fail silently. The
        // user keeps the reveal for the current session only.
    }
}

function revealAdultCard(badgeWrapper) {
    const card = badgeWrapper.parentElement;
    if (!card) return;
    const img = card.querySelector('img[src*="/lib/poster/"]');
    if (img && img.src.indexOf('/lib/poster/raw/') === -1 && img.src.indexOf('/og.jpg') === -1) {
        img.src = img.src.replace('/lib/poster/', '/lib/poster/raw/');
    }
    badgeWrapper.remove();
}

function applyAdultReveals(root) {
    if (window._showAdult) return;
    const target = root || document;
    const reveals = loadAdultReveals();
    target.querySelectorAll('.w-adult-badge[data-resource-id]').forEach((badge) => {
        if (reveals.has(badge.dataset.resourceId)) {
            revealAdultCard(badge);
        }
    });
}

// Delegated click handler. Caught at capture so the wrapping <a>
// doesn't navigate before we get to the badge. preventDefault +
// stopPropagation keep the click local to the reveal action.
document.addEventListener('click', (e) => {
    if (window._showAdult) return;
    const span = e.target.closest('.w-adult-badge > span');
    if (!span) return;
    const wrapper = span.parentElement;
    const rid = wrapper && wrapper.dataset.resourceId;
    if (!rid) return;
    e.preventDefault();
    e.stopPropagation();
    const reveals = loadAdultReveals();
    reveals.add(rid);
    saveAdultReveals(reveals);
    revealAdultCard(wrapper);
}, { capture: true });

document.addEventListener('DOMContentLoaded', () => applyAdultReveals());
document.addEventListener('async-loaded', (e) => applyAdultReveals(e.target));

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