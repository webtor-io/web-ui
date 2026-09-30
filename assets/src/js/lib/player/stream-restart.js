// The stream restart after its transcoder session is gone (docs/player.md,
// "Network errors and the stream restart").
//
// A session lives in the transcoder pod's memory: 10 min without a request
// (a pause, a hidden or sleeping tab) or a rollout, and it is gone; a day
// later the token in its URLs is too. Nothing the player can load again
// brings it back -- POST /session/<id>/seek answers 404 as well. A new
// session is a new start of the stream job, which is what this does, at the
// viewer's place in the film:
//
//   1. the note (sessionStorage, this tab): where the film stood, whether
//      it played, and the viewer's answer to the grace popup if they gave
//      one. The next player of the same file takes it on mounting and
//      continues there without the resume prompt, and without asking the
//      popup's question again (Player.jsx). Not
//      /watch/position: it answers 204 to a viewer without an account, and
//      it saves nothing under 30 s (useWatchHistory MIN_SAVED_POSITION).
//      Written only for a place the viewer is at (Player.jsx decides): past
//      the resume question and past 0. A session found dead at mounting (a
//      replayed job) has no such place, and a note there would stand in for
//      the saved position the next player must still offer;
//   2. the restart, `purge=true` on every path: a finished job is replayed
//      to whoever asks for its id, and the id changes only every 10 min on
//      the site, every hour in the embed (jobs/scripts Action / Embed) -- a
//      replay would hand back the very session that is gone. And
//      `force-slow=true` where this run was the slow-download modal's
//      "watch as is" (the element's data-offer-answered, StatusAnswered):
//      the viewer has answered that question, and a start without it asks
//      it again before any player (the modal's own "watch as is" then also
//      carries the purge -- slow_download.html, SlowDownloadData.Purge):
//        - in an embed: its POST again (passthrough.js postEmbedStart), with
//          the declaration the start sent;
//        - on the resource page whose start form is this file's: that form
//          (form.requestSubmit, as the preferred-language change restarts;
//          Turnstile and the declaration hook as for the button);
//        - otherwise (the form is another file's after a quiet move to the
//          next one, or there is none): this page for this file, started by
//          its deep link (#action=stream&purge=true, app/resource/get.js),
//          loaded (passthrough.js loadDocument).
//
// At most one AUTOMATIC restart per file every AUTO_RESTART_EVERY_MS, per
// tab (the budget, sessionStorage): a session that dies again within five
// minutes is not a pause the viewer took, and a restart would only go round
// again -- the card instead (createRecoveryPolicy). Where the budget cannot
// be written (storage blocked, e.g. an embed with third-party storage off)
// no automatic restart happens at all: an unrecorded restart is no limit.

import { loadDocument, postEmbedStart } from './passthrough.js';
import { applyDeclaration } from './decode-declaration.js';

export const AUTO_RESTART_EVERY_MS = 5 * 60 * 1000;
export const BUDGET_KEY = 'wt-stream-restarts';
export const NOTE_KEY = 'wt-stream-restart';
// The note waits this long for the next player: a stream job takes a
// median 58 s from the click to the first frame, its slow third minutes.
export const NOTE_TTL_MS = 10 * 60 * 1000;
// Marks the purge field this restart put on the page's start form; the next
// player takes it off (Player.jsx), so the viewer's own next press of the
// button is an ordinary start again.
export const PURGE_MARK = 'data-stream-restart';

export function fileKey(resourceID, path) {
    return `${resourceID || ''}:${path || ''}`;
}

export function safeSessionStorage(win = typeof window !== 'undefined' ? window : null) {
    try {
        return win ? win.sessionStorage : null;
    } catch (e) {
        return null;
    }
}

function readBudget(storage) {
    const m = JSON.parse(storage.getItem(BUDGET_KEY) || '{}');
    return m && typeof m === 'object' ? m : {};
}

// canAutoRestart: no restart of this file in this tab for the last
// AUTO_RESTART_EVERY_MS -- and the budget can be read at all.
export function canAutoRestart(storage, key, now = Date.now()) {
    if (!storage) return false;
    try {
        const at = readBudget(storage)[key];
        return !(typeof at === 'number' && now - at < AUTO_RESTART_EVERY_MS && now >= at);
    } catch (e) {
        return false;
    }
}

// recordRestart writes this file's restart; false where it could not be
// written. Entries past the window are dropped on the way.
export function recordRestart(storage, key, now = Date.now()) {
    if (!storage) return false;
    try {
        let m = {};
        try { m = readBudget(storage); } catch (e) { m = {}; }
        const kept = {};
        for (const [k, at] of Object.entries(m)) {
            if (typeof at === 'number' && now - at < AUTO_RESTART_EVERY_MS && now >= at) kept[k] = at;
        }
        kept[key] = now;
        storage.setItem(BUDGET_KEY, JSON.stringify(kept));
        return true;
    } catch (e) {
        return false;
    }
}

// The grace popup's answers (Player.jsx hide: data-grace-cta-answered).
const GRACE_ANSWERS = new Set(['continue', 'dismiss']);

// writeNote: grace is the viewer's answer to this player's grace popup
// ('continue' | 'dismiss'), '' where there was none.
export function writeNote(storage, { resourceID, path, at, play, grace = '' }, now = Date.now()) {
    if (!storage || !resourceID) return false;
    try {
        storage.setItem(NOTE_KEY, JSON.stringify({
            resourceID, path: path || '', at: Math.max(0, Number(at) || 0), play: !!play,
            grace: GRACE_ANSWERS.has(grace) ? grace : '', until: now + NOTE_TTL_MS,
        }));
        return true;
    } catch (e) {
        return false;
    }
}

// takeNote reads and removes the note: { at, play, grace } when it is for
// this file and still fresh, else null.
export function takeNote(storage, resourceID, path, now = Date.now()) {
    if (!storage) return null;
    let note = null;
    try {
        note = JSON.parse(storage.getItem(NOTE_KEY) || 'null');
        if (!note || note.resourceID !== resourceID || (note.path || '') !== (path || '')) return null;
        storage.removeItem(NOTE_KEY);
    } catch (e) {
        return null;
    }
    if (!(now <= note.until)) return null;
    const at = Number(note.at);
    return { at: Number.isFinite(at) && at > 0 ? at : 0, play: !!note.play, grace: GRACE_ANSWERS.has(note.grace) ? note.grace : '' };
}

// restartURL: this page pointed at the file, starting it at once, past the
// job cache (app/resource/get.js reads action, purge and force-slow from the
// hash); forceSlow for a run that was the slow-download modal's "watch as is".
export function restartURL(href, path, { forceSlow = false } = {}) {
    const u = new URL(href);
    if (path) {
        u.searchParams.set('file', path);
        u.searchParams.delete('file-idx');
    }
    u.hash = 'action=stream&purge=true' + (forceSlow ? '&force-slow=true' : '');
    return u.pathname + u.search + u.hash;
}

const START_FORMS = 'form[action$="/stream-video"], form[action$="/stream-audio"]';

function field(form, name) {
    const el = form.querySelector(`input[name="${name}"]`);
    return el ? el.value : '';
}

// itemOf: the file's item id -- on the element where the job put it for the
// player's own restarts (data-item-id), else on its #subtitles dialog, which
// every video render carries. '' for an audio render: the deep link then.
function itemOf(video, root, doc) {
    const d = (video && video.dataset) || {};
    if (d.itemId) return d.itemId;
    const modal = (root && root.querySelector && root.querySelector('#subtitles')) || doc.querySelector('#subtitles');
    return (modal && modal.getAttribute('data-item-id')) || '';
}

// answeredSlow: this run is the slow-download modal's "watch as is" -- the
// stream job renders data-offer-answered on the element for a force-slow
// start (StreamContent.StatusAnswered).
export function answeredSlow(video) {
    return !!(video && video.dataset && 'offerAnswered' in video.dataset);
}

// markedField puts a hidden field on the start form, marked as the
// restart's (PURGE_MARK), unless the form has that field already.
function markedField(form, doc, name) {
    if (form.querySelector(`input[name="${name}"]`)) return;
    const i = doc.createElement('input');
    i.setAttribute('type', 'hidden');
    i.setAttribute('name', name);
    i.setAttribute('value', 'true');
    i.setAttribute(PURGE_MARK, '');
    form.append(i);
}

// startFormOf: the page's start form of this file -- its resource and item
// -- or null (none on the page, another file's after a quiet move to the next
// one, an audio render without an item id).
function startFormOf(video, root, doc) {
    const resourceId = ((video && video.dataset) || {}).resourceId || '';
    const itemId = itemOf(video, root, doc);
    if (!itemId) return null;
    return Array.from(doc.querySelectorAll(START_FORMS))
        .find((f) => field(f, 'resource-id') === resourceId && field(f, 'item-id') === itemId) || null;
}

// markStart puts the restart's fields on this file's start form, marked
// (PURGE_MARK): purge, and force-slow where this run was the slow-download
// modal's "watch as is" (answeredSlow). They ride on every start of the form
// until a player mounts (it takes them off, clearPurgeMarks) -- so a restart
// that ends before any player (the no-peers or the slow-download modal, an
// error) leaves the viewer's next press of the button purged too.
function markStart(form, doc, video) {
    markedField(form, doc, 'purge');
    if (answeredSlow(video)) markedField(form, doc, 'force-slow');
}

// restartStream starts this file's stream again, past the job cache -- and,
// where the viewer answered the slow-download modal "watch as is" for this
// run, with that answer (force-slow): without it the job runs its gate again
// and the modal asks the same question before any player. Returns which path
// it took: 'embed', 'form' or 'navigate'.
export function restartStream({ win = window, doc = document, video, root = null,
    navigate = (u) => loadDocument(win.location, u) }) {
    const d = (video && video.dataset) || {};
    if (win._embedSettings) {
        // Nothing to carry: an embed's start never answers the modal (its
        // job is started with forceSlow false, jobs/scripts/embed.go, and
        // its POST reads no force-slow).
        postEmbedStart(win, doc, [['decode', d.decode || null], ['purge', 'true']]);
        return 'embed';
    }
    const form = startFormOf(video, root, doc);
    if (form && typeof form.requestSubmit === 'function') {
        markStart(form, doc, video);
        // As a start from the button: the layout's hook writes the same on
        // each of Turnstile's passes (decode-declaration.js).
        try { applyDeclaration(form, win); } catch (e) { /* the hook, or none */ }
        form.requestSubmit();
        return 'form';
    }
    navigate(restartURL(win.location.href, d.path || '', { forceSlow: answeredSlow(video) }));
    return 'navigate';
}

// markNextStart: the recovery gave up on this player -- the card is up
// (createRecoveryPolicy showCard): its session is dead, or loading has
// failed for good. The job id of this file's start stays the same for 10
// minutes, and the job that id replays is this player's, with the dead
// session in it: the viewer's own press of the page's button next ("Смотреть"
// instead of the card's) replayed it -- the same dead session, the card
// again (Chrome, 2026-09-30, scenario 9c). So this file's start form gets
// the restart's marked fields (markStart) now, without starting anything:
// the next start from it is past the job cache. Once: the next player takes
// them off; so does the card coming down (clearPurgeMarks) -- the stream is
// back. Nothing in an embed (no page form: its card's button is its start).
// Returns the form it marked, or null.
export function markNextStart({ win = window, doc = document, video, root = null }) {
    if (win._embedSettings) return null;
    const form = startFormOf(video, root, doc);
    if (!form) return null;
    markStart(form, doc, video);
    return form;
}

// clearPurgeMarks takes off the fields a restart left on the page's start
// forms (restartStream: purge, force-slow).
export function clearPurgeMarks(doc = document) {
    try {
        for (const el of doc.querySelectorAll(`input[${PURGE_MARK}]`)) el.remove();
    } catch (e) { /* nothing to clear */ }
}

// createRecoveryPolicy decides what a player does when hls.js cannot go on
// (network-recovery.js) or a session seek finds the session gone
// (session-seek.js onSessionGone):
//   - sessionGone(reason '404'|'403', status, pos?, via?, loader?): the grace
//     popup up or on its way (graceBlocks) -> nothing until it is answered
//     (graceAnswered), loading stopped; the next file loading (leaving) ->
//     nothing; within the budget (canAutoRestart) -> the restart at the
//     viewer's place (below), Umami player-recover-restart {reason, status,
//     via: load|error-nonfatal|seek, loader?}; beyond it -> the card, reason
//     'limit';
//   - giveUp(status): the card, reason 'network' (another 4xx, or the
//     backoff ran out);
//   - click(): the card's button -- the restart at the viewer's place,
//     playing, counted in the budget too;
//   - seeking(): a seek of the viewer's has begun (Player.jsx handleSeek);
//   - resumed(): a fragment of the film arrived (network-recovery.js
//     onFilmLoaded). Under the card that means loading went on after all --
//     a stall's startLoad, Play from the keyboard or the media session, a
//     session seek on a live session -- and the film plays: the card comes
//     down (hideCard), and the next failure is judged afresh. A card still
//     waiting (below) is dropped.
// The viewer's place is position() -- except after a session seek that
// found the session gone: sessionGone's pos, its target. Such a seek moves
// neither the element nor the run's offset (session-seek.js throws before
// either), so position() is still where the viewer was before it -- and on
// a restarted player whose resume seek found its session gone, the run's 0.
// The target stays the place for whatever comes next -- the restart, one
// that waited for the grace popup, the card's click, whether the seek opened
// the card, came while it waited or while it was up -- until the viewer
// seeks again (a local seek moves the element: its place is theirs) or the
// film loads again. Review F2, 2026-09-30: past the budget the card dropped
// it, and the click restarted at the old place.
// The card never stops a film that still plays from its buffer
// (playingFromBuffer: playing, and the element has data ahead): it waits
// until the element starves or the viewer pauses (whenStarving), and only
// then stops loading, is counted and shows. A 429 storm or a dead session
// with a minute buffered would otherwise freeze a picture that had a minute
// to go -- and the buffer may outlast the trouble.
// One owner of what the viewer sees: while blocked() -- a guard of
// passthrough.js has begun its own restart, or player-dead (dead-player.js)
// has declared the player dead -- this does nothing at all: no restart, no
// card. And `engaged` says this has taken the player over (a restart gone
// out, a card up or waiting, a restart waiting for the grace popup), which
// the others read the same way.
// The restart itself is `restart({ at, play })` (the player: the note, then
// restartStream). One restart per player: once it goes, the rest is the
// next page's.
export function createRecoveryPolicy({
    storage, key, now = () => Date.now(),
    graceBlocks = () => false, leaving = () => false,
    blocked = () => false,
    playingFromBuffer = () => false,
    whenStarving = () => () => {},
    position = () => ({ at: 0, play: false }),
    restart, stopLoad = () => {}, showCard = () => {}, hideCard = () => {},
    track = () => {},
}) {
    let restarting = false;
    let card = null;
    // The card, waiting for the film to starve: { reason, status, cause, via }.
    let waiting = null;
    let unwatch = null;
    let pending = null;
    let disposed = false;
    // The target of the last session seek that found the session gone,
    // { at, play }: the viewer's place until they seek again (above).
    let asked = null;
    const place = () => asked || position();

    const isBlocked = () => {
        try { return !!blocked(); } catch (e) { return false; }
    };
    const stopWaiting = () => {
        waiting = null;
        if (unwatch) {
            const u = unwatch;
            unwatch = null;
            try { u(); } catch (e) { /* gone already */ }
        }
    };
    const go = (pos) => {
        restarting = true;
        pending = null;
        stopWaiting();
        try { restart(pos); } catch (e) { /* the page goes on */ }
    };
    const show = ({ reason, status, cause, via }) => {
        stopWaiting();
        if (disposed || card || restarting || isBlocked()) return;
        card = { reason, status };
        try { stopLoad(); } catch (e) { /* already stopped */ }
        const data = { reason, status: status || 0 };
        if (cause) data.cause = cause;
        if (via) data.via = via;
        track('player-recover-card-shown', data);
        showCard(card);
    };
    const playingNow = () => {
        try { return !!playingFromBuffer(); } catch (e) { return false; }
    };
    const openCard = (reason, status, cause, via) => {
        if (card || restarting || isBlocked()) return;
        if (waiting) {
            // Another report while the card waits: the first one's card, but
            // the film is asked again. A session seek's report comes after
            // the seek paused the old run itself (while it was out: no
            // starving then) and never plays it again -- no event of the
            // element's follows it, and the card would never come.
            if (!playingNow()) show(waiting);
            return;
        }
        const c = { reason, status, cause, via };
        if (!playingNow()) {
            show(c);
            return;
        }
        waiting = c;
        try {
            unwatch = whenStarving(() => {
                if (waiting === c) show(c);
            }) || null;
        } catch (e) {
            show(c);
        }
    };

    // A second report after the restart has gone out (the page is still
    // loading the next one) finds the budget spent, and no card opens while
    // restarting (openCard): one restart per player.
    function sessionGone(reason, status, pos = null, via = 'load', loader = '') {
        if (disposed) return;
        // A seek's target is the viewer's place from now on -- also under a
        // card that is up already, which swallows the report itself.
        if (pos) asked = pos;
        if (card) return;
        if (isBlocked() || leaving()) return;
        const at = place();
        if (graceBlocks(at.at)) {
            // The place is not kept here: `asked` has it, and a seek of the
            // viewer's meanwhile makes it theirs again.
            pending = { reason, status, via, loader };
            try { stopLoad(); } catch (e) { /* already stopped */ }
            return;
        }
        if (canAutoRestart(storage, key, now()) && recordRestart(storage, key, now())) {
            const data = { reason, status: status || 0, via };
            if (loader) data.loader = loader;
            track('player-recover-restart', data);
            go(at);
            return;
        }
        openCard('limit', status, reason, via);
    }

    return {
        sessionGone,
        giveUp(status) {
            if (disposed || restarting) return;
            if (isBlocked() || leaving()) return;
            openCard('network', status);
        },
        graceAnswered() {
            if (disposed || !pending) return;
            const p = pending;
            pending = null;
            sessionGone(p.reason, p.status, null, p.via, p.loader);
        },
        click() {
            if (disposed || restarting || !card || isBlocked()) return;
            track('player-recover-card-click', { reason: card.reason, status: card.status || 0 });
            recordRestart(storage, key, now());
            // "Continue watching" is a Play: the card paused the dead film.
            go({ ...place(), play: true });
        },
        seeking() {
            if (disposed || restarting) return;
            asked = null;
        },
        resumed() {
            if (disposed || restarting) return;
            asked = null;
            stopWaiting();
            if (!card) return;
            card = null;
            try { hideCard(); } catch (e) { /* the page goes on */ }
        },
        dispose() { disposed = true; pending = null; stopWaiting(); },
        get card() { return card; },
        get waiting() { return waiting; },
        get pending() { return pending; },
        get restarting() { return restarting; },
        // This has taken the player over: a restart gone out, a card up or
        // waiting for the buffer, a restart waiting for the grace popup.
        get engaged() { return restarting || !!card || !!waiting || !!pending; },
    };
}
