import executeScriptElements, { inSkip } from "./executeScriptElements";
import { carryBusy, hasJob } from "./actionBusy";
// options (optional): { noScroll?: boolean } — lets callers suppress the
// automatic scroll-to-top that `data-async-scroll-top` targets normally
// trigger. Used by small in-place toggles (e.g. watched mark/unmark buttons)
// that reload a big target but shouldn't jump the user back to the top.
// destroyViews / activateViews are the two halves of a swap, for a caller that
// cannot hand its HTML to loadAsyncView because part of the target is live and
// must survive (the playing player, lib/player/next-item-go.js syncPage).
// `skip` is that part (an element, or several): its views are neither
// destroyed nor re-initialised.
export function destroyViews(target, skip = null) {
    const els = target.querySelectorAll('[data-async-view]');
    for (const el of els) {
        if (inSkip(skip, el)) continue;
        const view = el.getAttribute('data-async-view');
        const detail = {
            target: el,
        };
        const event = new CustomEvent(`async:${view}_destroy`, { detail });
        window.dispatchEvent(event);
    }
}

export function activateViews(target, skip = null) {
    executeScriptElements(target, skip);
    // Update async elements
    window.dispatchEvent(new CustomEvent('async', { detail: { target } }));
    for (const script of target.getElementsByTagName('script')) {
        if (script.src === "" || inSkip(skip, script)) continue;
        const url = new URL(script.src);
        const name = url.pathname.replace(/\.js$/, '');
        window.dispatchEvent(new CustomEvent('async:' + name, { detail: { target: script.parentElement } }));
    }
}

// options.keepJobs: an in-place toggle -- the watched mark, a rating -- that
// answers with the whole of main and the same page in it. A job's log there
// (partials/file.html #log-<item>, list.html's archive log) with a job in it
// (actionBusy.js hasJob) keeps its job: it came back empty, the job and a
// player buffering or already playing thrown away (2026-10-14) -- one slip of
// the thumb on a phone, where the toggle sits next to Download. The log is
// moved into its new place, as syncPage moves the playing player: its views
// are neither told they are going nor started again, and the start form
// rendered anew is busy from the same press (carryBusy) -- the job still
// releases it, a second press is still swallowed. Only for such a toggle: a
// log id names a file, not a job -- the home page's #log-load comes back
// with a new job in it, a language switch re-renders the page in other words,
// and another torrent can have a file at the same path.
function loadAsyncView(target, body, options) {
    const live = options && options.keepJobs
        ? Array.from(target.querySelectorAll('[id^="log-"]')).filter(hasJob) : [];
    if (!live.length) {
        destroyViews(target);
        renderBody(target, body, options);
        return;
    }
    // Parsed aside, in the target's own context, to find the log's new place.
    // SAFETY: as in renderBody.
    const fresh = target.cloneNode(false);
    fresh.innerHTML = body;
    const slots = new Map(Array.from(fresh.querySelectorAll('[id^="log-"]'), (s) => [s.id, s]));
    const kept = live.filter((l) => slots.has(l.id));
    // A busy start form's twin is the one at its place among the forms with
    // its target and action: the same template renders them in the same
    // order, and two can share both (the archive dialog's TAR and ZIP).
    const sig = (f) => `${f.getAttribute('data-async-target')} ${f.getAttribute('action')}`;
    const peers = (f) => Array.from(target.querySelectorAll('form[data-async-target]'))
        .filter((p) => !inSkip(kept, p) && sig(p) === sig(f));
    const busy = Array.from(target.querySelectorAll('form[data-busy="true"]'))
        .filter((f) => !inSkip(kept, f) && kept.some((l) => f.getAttribute('data-async-target') === `#${l.id}`))
        .map((f) => [f, peers(f).indexOf(f)]);
    destroyViews(target, kept);
    for (const log of kept) slots.get(log.id).replaceWith(log);
    // Moved within one task: a media element pauses only when it is still out
    // of the document at the next stable state, so the player plays on.
    target.replaceChildren(...fresh.childNodes);
    for (const [old, i] of busy) {
        const twin = peers(old)[i];
        if (twin) carryBusy(old, twin);
    }
    activateViews(target, kept);
    scrollTop(target, options);
}
function renderBody(target, body, options) {
    // SAFETY: `body` is always same-origin server-rendered HTML from our own Go/Gin
    // templates — extracted from <template data-async-fragment> blocks in AJAX
    // responses (async.js, then dispatched to layout.js for nav/footer), or from
    // same-origin SSE messages (progressLog.js). No external or user-supplied
    // HTML reaches here.
    target.innerHTML = body;
    activateViews(target);
    scrollTop(target, options);
}
function scrollTop(target, options) {
    if (target.hasAttribute('data-async-scroll-top') && !(options && options.noScroll)) {
        window.scrollTo({ top: 0 });
    }
}

export default loadAsyncView;
