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
import { readCarry } from './next-item.js';
import { persistTrackChoice } from './track-dialog.js';
import { destroyViews, activateViews } from '../loadAsyncView';

// A prepared render older than this is not used: its transcoder session and
// the job's cached render both live about ten minutes.
export const PREPARED_MAX_AGE_MS = 8 * 60 * 1000;

// How long the next file's start may take before it is given up on. NOT the
// 30 s a settings restart allows (background-render.js): that one re-renders a
// file that is already playing, this one is a cold stream start -- a warm-up
// alone may run for minutes on a thin swarm. With 30 s every slow start "timed
// out" into the visible fallback, which is a full page load: the viewer waited
// half a minute and then watched the page restart (owner, 2026-09-20). The
// job's own deadlines decide when a start has failed; this only has to be
// longer than they are. The card shows the log meanwhile.
export const NEXT_RENDER_TIMEOUT_MS = 10 * 60 * 1000;

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

const FALLBACK_NOTE = 'wt-next-fallback';

// takeFallbackNote: the reason the PREVIOUS page gave up on a quiet move and
// reloaded, once, if it is recent. Reported by the player that comes up after.
export function takeFallbackNote(storage, nowMs = Date.now()) {
    try {
        const raw = storage.getItem(FALLBACK_NOTE);
        if (!raw) return null;
        storage.removeItem(FALLBACK_NOTE);
        const note = JSON.parse(raw);
        return note && nowMs - note.at < 5 * 60 * 1000 ? note : null;
    } catch (e) {
        return null;
    }
}

// canMoveOn: is this a page the move can happen on? It needs the start form of
// the current file (to re-address) and the #content section (to bring up to
// date). Only the resource page has them; a player anywhere else simply has
// no "next", rather than a button that ends in a reload of the wrong page.
export function canMoveOn(root = document) {
    return !!(root.querySelector(START_FORM) && root.getElementById && root.getElementById('content'));
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

export function createNextItemGo({ next, resourceID, root, getStage, getAspectRatio = () => '', initPlayer, destroyPlayer, onEvent = () => {},
    fetchRender = fetchStreamRender, getToken = backgroundToken, now = () => Date.now(),
    navigate = (u) => window.location.assign(u) }) {
    let prepared = null;   // { doc, at }
    let preparing = null;  // Promise
    let going = false;
    let whyNot = ''; // why the last quiet attempt produced no player

    const fetchNext = async () => {
        const modal = document.getElementById('subtitles');
        const form = nextStartForm(next, readCarry(modal || document));
        if (!form) { whyNot = 'no-start-form'; return null; }
        const token = await getToken();
        if (token === null) { whyNot = 'turnstile-needs-a-click'; return null; } // not quietly
        // The log of the next file's start, for the viewer who is waiting on
        // it (Player.jsx shows the latest line on the card).
        return fetchRender(form, { token, timeoutMs: NEXT_RENDER_TIMEOUT_MS, onProgress: (text) => onEvent('progress', { text }) });
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
    //
    // A fallback is a page load, and a page load wipes the console: the reason
    // is left in sessionStorage for the next page to report (takeFallbackNote),
    // so "it reloads instead of moving on" comes with a why.
    const visibleFallback = (reason) => {
        try {
            window.sessionStorage.setItem(FALLBACK_NOTE, JSON.stringify({ reason, path: next.path, at: now() }));
        } catch (e) { /* no storage: the reload still happens */ }
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
            // Not ready: this is a stream start like any other and can take
            // its minute. The player shows it (Player.jsx nextLoading).
            onEvent('loading', { on: true });
            doc = await (preparing || prepare());
            onEvent('loading', { on: false });
        }
        if (!doc || !doc.querySelector('.player')) {
            const reason = doc ? 'render-is-not-a-player' : (whyNot || 'no-render');
            onEvent('go', { how, prewarmed, fallback: true, reason, wait_ms: now() - startedAt });
            onEvent('loading', { on: true }); // until the navigation takes over
            visibleFallback(reason);
            return;
        }
        const fullscreen = !!(document.fullscreenElement || document.webkitFullscreenElement);
        const url = nextURL(window.location.href, next.path);
        try {
            await mountOnStage(doc);
        } catch (e) {
            // The old player is already gone at this point: a half-built
            // page is the one outcome worse than a reload.
            const reason = `mount-failed: ${e && e.message ? e.message : e}`;
            console.error('next item:', reason, e);
            onEvent('go', { how, prewarmed, fallback: true, reason, wait_ms: now() - startedAt });
            visibleFallback(reason);
            return;
        }
        const title = doc.querySelector('.player').getAttribute('data-resource-title');
        if (title) document.title = `${title} | Webtor.io`;
        const main = document.querySelector('main[data-async-layout]');
        window.history.pushState({
            context: 'links', url, fetchParams: undefined, targetSelector: 'main',
            layout: main ? main.getAttribute('data-async-layout') : 'main',
        }, '', url);
        onEvent('go', { how, prewarmed, fallback: false, fullscreen, wait_ms: now() - startedAt });
        scheduleSync(url);
    };

    // mountOnStage: the old player goes, its stage stays; everything else the
    // old render brought (the dialogs, the grace card, the logo) goes with it,
    // or the new render's #subtitles would be the second one in the document.
    function mountOnStage(doc) {
        const stage = getStage();
        const aspectRatio = getAspectRatio(); // before the old player is gone
        // The stage is empty between the two players, and an empty block has
        // no height: the page below jumped up and back (owner). Hold the
        // height it has now until the new player is ready, and say "loading"
        // inside it meanwhile.
        if (stage) {
            stage.style.minHeight = `${stage.offsetHeight}px`;
            // --switching holds the height; --empty is the spinner, and only
            // for as long as there is no player in the stage to show its own.
            stage.classList.add('wt-player-stage--switching', 'wt-player-stage--empty');
            const release = () => {
                window.removeEventListener('player_ready', release);
                clearTimeout(timer);
                stage.style.minHeight = '';
                stage.classList.remove('wt-player-stage--switching', 'wt-player-stage--empty');
            };
            const timer = setTimeout(release, 15000);
            window.addEventListener('player_ready', release);
        }
        destroyPlayer({ keepStage: true });
        const host = stage ? stage.parentNode : root;
        for (const child of [...host.children]) {
            if (child !== stage) child.remove();
        }
        // Scripts in the render are inert when adopted this way, which is
        // wanted: the player is initialised here, by hand, on the stage.
        //
        // What is adopted is the CONTENT of the render's own wrapper (the
        // <div class="relative"> around the video and its dialogs), into the
        // old wrapper that holds the stage: adopting the wrapper itself nested
        // one more level on every move. The stylesheet and script that follow
        // the wrapper in the render are already on the page.
        const player = doc.querySelector('.player');
        const from = player && player.parentNode ? player.parentNode : doc.body;
        for (const node of [...from.childNodes]) {
            if (node.nodeType === 1 && (node.tagName === 'SCRIPT' || node.tagName === 'LINK')) continue;
            host.appendChild(document.importNode(node, true));
        }
        // No auto-resume note here (there was one in the first version): a
        // next episode the viewer had already started asks "continue from
        // ... / start over" like any other file -- it is their question, and
        // answering it for them read as the position being ignored (owner,
        // 2026-09-20).
        // awaitStart: the new player is about to play by itself; until it does
        // it shows a spinner, not the big Play button of a paused film.
        return Promise.resolve(initPlayer(host, { stage, aspectRatio, awaitStart: true })).then(() => {
            // From here on the player is up. What follows is housekeeping, and
            // a throw in it must NOT read as "the mount failed": that sends
            // the viewer into a page reload with a working player on screen
            // (2026-09-20 -- the review's try/catch was drawn around all of
            // this, and automatic moves turned into reloads).
            try {
                if (stage) stage.classList.remove('wt-player-stage--empty');
                persistDefaults(host, resourceID, next.itemId);
                window.dispatchEvent(new CustomEvent('player_replaced', { detail: { target: host } }));
            } catch (e) {
                console.error('next item: after-mount step failed', e);
            }
        });
    }

    return { prepare, go, isPrepared: () => !!freshPrepared() };
}

// The carried choice arrives as this render's DEFAULT chips, and a default is
// not a saved choice: the next plain start of this file -- a settings restart,
// a reload -- would ask the ladder again and could flip the very thing the
// viewer carried over (subtitles they had switched off coming back on). So
// what the render chose is saved as theirs, the way a click on the chip is.
function persistDefaults(scope, resourceID, itemID) {
    for (const [type, sel] of [['audio', '.audio[data-default="true"]'], ['subtitle', '.subtitle[data-default="true"]']]) {
        const chip = scope.querySelector(sel);
        const id = chip && chip.getAttribute('data-id');
        if (id) persistTrackChoice(type, { id, resourceID, itemID });
    }
}

// The page is synced with the file that is playing NOW, once: several moves
// can happen in one fullscreen sitting, and each used to leave its own
// "sync when fullscreen ends" behind -- on exit they all ran, raced, and the
// slowest won, which could leave episode 2's card and start form under
// episode 3's picture. One pending URL, one listener, and a generation that
// lets a newer sync overtake an older one in flight.
let pendingSyncURL = null;
let syncListening = false;
let syncGeneration = 0;

function inFullscreen() {
    return !!(document.fullscreenElement || document.webkitFullscreenElement);
}

function scheduleSync(url) {
    pendingSyncURL = url;
    const run = () => {
        if (inFullscreen() || pendingSyncURL === null) return;
        const target = pendingSyncURL;
        pendingSyncURL = null;
        syncPage(target);
    };
    if (!syncListening) {
        syncListening = true;
        document.addEventListener('fullscreenchange', run);
        document.addEventListener('webkitfullscreenchange', run);
    }
    run();
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

// liveRoot: everything of the playing stream that must survive a page sync, as
// ONE element -- the top of the action view, the direct child of its log
// container. Not `stage.parentNode`: the stream template wraps the video in a
// <div class="relative">, so that was an INNER element, and the view's marker
// (data-async-view, whose destroy handler is destroyPlayer) sat on its
// ancestor. destroyViews(content, live) did not see the ancestor as part of
// the live subtree, told it that it was going -- and the player that had just
// mounted was destroyed a moment later, leaving the bare file list (owner,
// 2026-09-20). It also left the view's own wrapper behind on every sync.
export function liveRoot(stage) {
    let el = stage;
    while (el && el.parentElement && !/^log-/.test(el.parentElement.id || '')) el = el.parentElement;
    return el && el.parentElement ? el : (stage ? stage.parentNode : null);
}

// syncPage brings the page around the player up to date with the file that is
// now playing: the file card, the list, the address the buttons post to. The
// player itself must not be re-rendered, so #content is not swapped the usual
// way -- the fresh content is fetched aside, the live player is moved into
// ITS log container, and only then does it replace the old one.
export async function syncPage(url, { fetchImpl = fetch } = {}) {
    const generation = ++syncGeneration;
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
    if (generation !== syncGeneration) return false; // a newer move has its own sync under way
    const parsed = new DOMParser().parseFromString(text, 'text/html');
    const tpl = parsed.querySelector('template[data-async-fragment="main"]');
    const fresh = document.createElement('div');
    // SAFETY: same-origin server-rendered HTML, the same fragment the async
    // library puts into this very container (lib/loadAsyncView.js).
    fresh.innerHTML = tpl ? tpl.innerHTML : text;
    const log = fresh.querySelector('#file [id^="log-"]');
    const live = liveRoot(stageHost);
    if (!log || !live) return false;
    const video = stageHost.querySelector('video, audio');
    const wasPlaying = !!video && !video.paused;
    // The same two halves loadAsyncView performs around a swap, minus the
    // live player: the views of the old card and list are told they are
    // going, and the new ones are started -- without the second half the file
    // list came back with its scripts never run (resource/select.js: no
    // multi-select, no archive) until the next navigation.
    destroyViews(content, live);
    log.appendChild(live);
    content.replaceChildren(...fresh.childNodes);
    // A media element pauses when it leaves the document, even for a moment.
    if (wasPlaying && video.paused) video.play().catch(() => {});
    activateViews(content, live);
    return true;
}
