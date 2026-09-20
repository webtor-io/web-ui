// Moving to the next file without leaving the player. The decisions are in
// next-item.js; this is the machinery: the start form of the next file, the
// background render, the swap on the stage, the address bar, the page around.
//
// One path, whatever the state: the next player is mounted INTO THE SAME
// STAGE (Player.jsx initPlayer) from a render fetched off the page
// (background-render.js), so a viewer in fullscreen stays there. The page
// around it -- the file card, the list, the log the player sits in -- belongs
// to the previous file at that moment; it is brought up to date at once when
// the player is windowed, and when fullscreen ends otherwise. Anything that
// cannot be done quietly (an error card, a cap modal, a Turnstile checkbox)
// falls back to opening the next file the ordinary, visible way.

import { backgroundToken, fetchStreamRender } from './background-render.js';
import { markAutoResume } from './preferred-lang.js';
import { readCarry } from './next-item.js';

// A prepared render older than this is not used: its transcoder session and
// the job's cached render both live about ten minutes.
export const PREPARED_MAX_AGE_MS = 8 * 60 * 1000;

const START_FORM = 'form[action$="/stream-video"], form[action$="/stream-audio"]';

// nextStartForm: the form that started THIS file, re-addressed to the next
// one and carrying the viewer's current track choice. Detached -- it is only
// ever read (FormData, action, data-async-target), never submitted.
export function nextStartForm(next, carry, root = document) {
    const cur = root.querySelector(START_FORM);
    if (!cur || !next) return null;
    const form = cur.cloneNode(true);
    const item = form.querySelector('input[name="item-id"]');
    if (!item) return null;
    item.value = next.itemId;
    // FormData reads the live value; the attribute is kept in step for
    // anything that serialises the clone.
    item.setAttribute('value', next.itemId);
    for (const [name, value] of Object.entries(carry || {})) {
        const input = root.createElement ? root.createElement('input') : document.createElement('input');
        input.type = 'hidden';
        input.name = name;
        input.value = value;
        form.appendChild(input);
    }
    return form;
}

// nextURL: this page, pointed at the next file. `file` is how a file is
// addressed; `file-idx` is the other way in and would contradict it.
export function nextURL(href, path) {
    const u = new URL(href);
    u.searchParams.set('file', path);
    u.searchParams.delete('file-idx');
    u.hash = '';
    return u.pathname + u.search;
}

export function createNextItemGo({ next, resourceID, root, getStage, initPlayer, destroyPlayer, onEvent = () => {},
    fetchRender = fetchStreamRender, getToken = backgroundToken, now = () => Date.now(),
    navigate = (u) => window.location.assign(u) }) {
    let prepared = null;   // { doc, at }
    let preparing = null;  // Promise
    let going = false;

    const fetchNext = async () => {
        const modal = document.getElementById('subtitles');
        const form = nextStartForm(next, readCarry(modal || document));
        if (!form) return null;
        const token = await getToken();
        if (token === null) return null; // needs a visible checkbox: not quietly
        return fetchRender(form, { token });
    };

    const prepare = () => {
        if (preparing || prepared) return preparing;
        preparing = fetchNext().then((doc) => {
            preparing = null;
            if (doc) {
                prepared = { doc, at: now() };
                kickTranslation(doc);
            }
            onEvent('prepared', { ok: !!doc });
            return doc;
        }).catch(() => { preparing = null; return null; });
        return preparing;
    };

    const freshPrepared = () => (prepared && now() - prepared.at <= PREPARED_MAX_AGE_MS ? prepared.doc : null);

    // The ordinary way in, for everything that cannot be done quietly.
    const visibleFallback = () => {
        navigate(nextURL(window.location.href, next.path) + '#action=stream');
    };

    const go = async (how) => {
        if (going) return;
        going = true;
        const startedAt = now();
        const prewarmed = !!freshPrepared();
        let doc = freshPrepared();
        if (!doc) {
            prepared = null;
            doc = await (preparing || prepare());
        }
        if (!doc || !doc.querySelector('.player')) {
            onEvent('go', { how, prewarmed, fallback: true, wait_ms: now() - startedAt });
            visibleFallback();
            return;
        }
        const fullscreen = !!(document.fullscreenElement || document.webkitFullscreenElement);
        mountOnStage(doc);
        const url = nextURL(window.location.href, next.path);
        const title = doc.querySelector('.player').getAttribute('data-resource-title');
        if (title) document.title = `${title} | Webtor.io`;
        const main = document.querySelector('main[data-async-layout]');
        window.history.pushState({
            context: 'links', url, fetchParams: undefined, targetSelector: 'main',
            layout: main ? main.getAttribute('data-async-layout') : 'main',
        }, '', url);
        onEvent('go', { how, prewarmed, fallback: false, fullscreen, wait_ms: now() - startedAt });
        if (!fullscreen) {
            syncPage(url);
        } else {
            const onChange = () => {
                if (document.fullscreenElement || document.webkitFullscreenElement) return;
                document.removeEventListener('fullscreenchange', onChange);
                document.removeEventListener('webkitfullscreenchange', onChange);
                syncPage(url);
            };
            document.addEventListener('fullscreenchange', onChange);
            document.addEventListener('webkitfullscreenchange', onChange);
        }
    };

    // mountOnStage: the old player goes, its stage stays; everything else the
    // old render brought (the dialogs, the grace card, the logo) goes with it,
    // or the new render's #subtitles would be the second one in the document.
    function mountOnStage(doc) {
        const stage = getStage();
        destroyPlayer({ keepStage: true });
        const host = stage ? stage.parentNode : root;
        for (const child of [...host.children]) {
            if (child !== stage) child.remove();
        }
        // Scripts in the render are inert when adopted this way, which is
        // wanted: the player is initialised here, by hand, on the stage.
        for (const node of [...doc.body.childNodes]) {
            if (node.nodeType === 1 && node.tagName === 'SCRIPT') continue;
            host.appendChild(document.importNode(node, true));
        }
        // The next player does not stop to ask "continue from ...?" -- the
        // same one-shot note a settings restart leaves (preferred-lang.js).
        markAutoResume(resourceID, next.path);
        return Promise.resolve(initPlayer(host, { stage })).then(() => {
            window.dispatchEvent(new CustomEvent('player_replaced', { detail: { target: host } }));
        });
    }

    return { prepare, go, isPrepared: () => !!freshPrepared() };
}

// A carried AI translation is the default of the prepared render, but a
// translation only starts when something asks for it. One request for the
// track is that ask: by the time the episode begins the first lines are in.
function kickTranslation(doc) {
    const chip = doc.querySelector('.subtitle[data-default="true"][data-provider="Translated"]');
    const src = chip && chip.getAttribute('data-src');
    if (!src) return;
    try { fetch(src, { method: 'HEAD' }).catch(() => {}); } catch (e) { /* best effort */ }
}

// syncPage brings the page around the player up to date with the file that is
// now playing: the file card, the list, the address the buttons post to. The
// player itself must not be re-rendered, so #content is not swapped the usual
// way -- the fresh content is fetched aside, the live player is moved into
// ITS log container, and only then does it replace the old one.
export async function syncPage(url, { fetchImpl = fetch } = {}) {
    const content = document.getElementById('content');
    const stageHost = document.querySelector('.wt-player-stage');
    if (!content || !stageHost) return false;
    let text = '';
    try {
        const res = await fetchImpl(url, {
            headers: {
                'X-Requested-With': 'XMLHttpRequest',
                'X-Layout': content.getAttribute('data-async-layout') || '',
                'X-CSRF-TOKEN': window._CSRF,
                'X-SESSION-ID': window._sessionID,
            },
        });
        if (!res || res.ok === false) return false;
        text = await res.text();
    } catch (e) {
        return false;
    }
    const parsed = new DOMParser().parseFromString(text, 'text/html');
    const tpl = parsed.querySelector('template[data-async-fragment="main"]');
    const fresh = document.createElement('div');
    // SAFETY: same-origin server-rendered HTML, the same fragment the async
    // library puts into this very container (lib/loadAsyncView.js).
    fresh.innerHTML = tpl ? tpl.innerHTML : text;
    const log = fresh.querySelector('#file [id^="log-"]');
    const live = stageHost.parentNode; // the action view's root: stage + dialogs
    if (!log || !live) return false;
    const video = stageHost.querySelector('video, audio');
    const wasPlaying = !!video && !video.paused;
    log.appendChild(live);
    content.replaceChildren(...fresh.childNodes);
    // A media element pauses when it leaves the document, even for a moment.
    if (wasPlaying && video.paused) video.play().catch(() => {});
    window.dispatchEvent(new CustomEvent('async', { detail: { target: content } }));
    return true;
}
