// The job-start buttons (Watch, Download, …) while their job is running.
//
// Measured 2026-09-19: from the first click to the first frame the median is
// 58 s and only 4.8% of viewers are playing within 10 s -- the wait is the
// torrent swarm, not our code (a cold file spends ~21 s warming up and
// ~6 s buffering, and the slow third of jobs takes over a minute). The button
// stayed live through all of it, so 59% of streaming sessions pressed it more
// than once; two thirds of the clicks that recorded nothing were simply the
// same button again, a median 17 s later. Each of those was another POST. The
// server attaches a repeat within the job id's 10-minute bucket to the job
// already running (jobs/scripts/action.go), so the cost is the page's: the
// log, or a player still buffering, thrown away and replayed from the start.
//
// So a form that started a job is BUSY: its submit button keeps its label,
// swaps its icon for a spinner and goes dim (`.btn-busy` -- not `disabled`: it
// is working, not refusing), and a second submit is swallowed (lib/async.js).
// The log below the button keeps its place -- it is the detail, the button is
// the state.
//
// Every busy state has to end, or a job that dies quietly leaves a dead
// button. The ways out: the job log reports an ending (`error` -- no peers,
// the cap modal --, `download`, `redirect`, `close` of a job that rendered
// nothing; lib/progressLog.js), the player it rendered is shown or its 30 s
// backstop passes (app/action.js), the request itself failed or was refused
// (lib/async.js), or the safety timeout below expires.

import { notePress } from './stickyStatus';

// A job may legitimately run for minutes (p99 of a stream job is ~8 min), so
// this is a backstop against a lost SSE, not a deadline.
export const BUSY_MAX_MS = 15 * 60 * 1000;

// Which forms this is for: the five action endpoints that start a job. Other
// async forms (the watched toggle, profile settings) answer instantly and
// would only flash.
const ACTION_RE = /\/(download-file|download-dir|preview-image|stream-audio|stream-video)$/;

function actionPath(form) {
    try {
        return new URL(form.getAttribute('action') || '', window.location.href).pathname;
    } catch (e) {
        return '';
    }
}

export function isActionForm(form) {
    if (!form || form.tagName !== 'FORM') return false;
    return ACTION_RE.test(actionPath(form));
}

export function isBusy(form) {
    return !!form && form.dataset.busy === 'true';
}

function submitButton(form) {
    return form.querySelector('button[type="submit"], button:not([type])');
}

export function markBusy(form, { now = Date.now, timeoutMs = BUSY_MAX_MS } = {}) {
    if (!isActionForm(form) || isBusy(form)) return false;
    const btn = submitButton(form);
    if (!btn) return false;
    // One target, one working button. A directory's TAR and ZIP share a log
    // container (list.html), and a new job's response replaces whatever log
    // was in it -- so a form still busy for that target has just lost the
    // only thing that could release it. Release it here.
    const target = form.getAttribute('data-async-target');
    if (target) {
        for (const other of document.querySelectorAll('form[data-busy="true"]')) {
            if (other !== form && other.getAttribute('data-async-target') === target) clearBusy(other);
        }
    }
    // What the job produces is brought into view (stickyStatus.js reveal)
    // unless the viewer scrolls away after this. Not for a restart the player
    // makes on its own (resubmit): nobody pressed, and a viewer reading the
    // list under a playing file must not be pulled back up to it.
    if (!form._quietPress) notePress(form.ownerDocument.defaultView);
    show(form, btn, now, timeoutMs);
    // A form inside the log it targets -- the no-peers card's "try again",
    // the slow card's "continue at slow speed": the response replaces the
    // log and the form with it, and nothing on the page looked busy for the
    // job it started; a press on Watch then threw that job away. The page's
    // own start form for the same log and endpoint is busy with it.
    const host = target ? document.querySelector(target) : null;
    if (host && host.contains(form)) {
        const path = actionPath(form);
        for (const page of document.querySelectorAll('form[data-async-target]')) {
            if (page === form || host.contains(page) || isBusy(page)) continue;
            if (page.getAttribute('data-async-target') !== target || actionPath(page) !== path) continue;
            const pageBtn = submitButton(page);
            if (pageBtn) show(page, pageBtn, now, timeoutMs, form.dataset.busyPress);
        }
    }
    return true;
}

// Which press a busy form is busy from: app/action.js releases the forms of
// the press its job answers, a copy a swap rendered anew among them
// (carryBusy). Not busySince: two presses can share a millisecond. Counted on
// window, not here -- each bundle has its own copy of this module
// (clearBusyFor).
function nextPress() {
    window.__busyPress = (window.__busyPress || 0) + 1;
    return String(window.__busyPress);
}

function show(form, btn, now, timeoutMs, press = nextPress()) {
    form.dataset.busy = 'true';
    form.dataset.busySince = String(now());
    form.dataset.busyPress = press;
    // The button keeps saying what it is (owner, 2026-09-19): the label and
    // its icon stay, and a spinner joins them. A button that renamed itself
    // to "preparing…" lost the one word that said which of the two buttons
    // the viewer had pressed.
    btn.dataset.busyLabel = btn.innerHTML;
    const spinner = document.createElement('span');
    spinner.className = 'loading loading-spinner loading-xs';
    spinner.setAttribute('aria-hidden', 'true');
    // In the icon's place, not beside it (owner): every action button leads
    // with one (partials/icons.html), and a spinner next to it made the
    // button wider and said the same thing twice. Restored wholesale by
    // clearBusy, which puts the saved innerHTML back.
    const icon = btn.querySelector('svg');
    if (icon) icon.replaceWith(spinner);
    else btn.insertBefore(spinner, btn.firstChild);
    // Dimmed, not disabled (owner): the button is not refusing, it is
    // working. `.btn-busy` takes the pointer off it; a press that gets
    // through anyway (keyboard) is swallowed by the guard in async.js.
    btn.classList.add('btn-busy');
    btn.setAttribute('aria-busy', 'true');
    btn.setAttribute('aria-disabled', 'true');
    // Cleared first: one timer per form, whatever the caller does. Two of
    // them would leave one running with nothing to cancel it -- found by the
    // negative control for the guard above, which hung the test runner for
    // the full fifteen minutes instead of failing.
    if (form._busyTimer) clearTimeout(form._busyTimer);
    form._busyTimer = timeoutMs > 0 ? setTimeout(() => clearBusy(form), timeoutMs) : null;
}

export function clearBusy(form) {
    if (!form || !isBusy(form)) return false;
    delete form.dataset.busy;
    delete form.dataset.busySince;
    delete form.dataset.busyPress;
    delete form.dataset.busyPressed;
    if (form._busyTimer) {
        clearTimeout(form._busyTimer);
        form._busyTimer = null;
    }
    const btn = submitButton(form);
    if (btn) {
        if (btn.dataset.busyLabel !== undefined) {
            // SAFETY: the markup this restores is the button's own, captured
            // from innerHTML a moment ago -- nothing from the network.
            btn.innerHTML = btn.dataset.busyLabel;
            delete btn.dataset.busyLabel;
        }
        btn.classList.remove('btn-busy');
        btn.removeAttribute('aria-busy');
        btn.removeAttribute('aria-disabled');
    }
    return true;
}

// clearBusyFor releases the form whose job wrote into `el` -- the log element,
// or anything inside it. The form names its target by selector, so the walk is
// up the ancestors looking for an id a busy form points at.
//
// The register of busy forms is the DOM (`[data-busy]`), not a Map in this
// module. It was a Map, and the button was never released by its own job
// (owner, 2026-09-20: "the spinner keeps spinning after the player appears"):
// async.js, which marks a form busy, is bundled into the page entry, while
// progressLog.js, which reads the job log, is a lazy chunk -- webpack gives
// each of them its own copy of this module, so the Map written on submit was
// not the Map read when the job ended. Only the safety timeout, fifteen
// minutes later, ever ended the spin. One DOM, one register.
export function clearBusyFor(el) {
    const busy = () => document.querySelectorAll('form[data-busy="true"]');
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
        if (!n.id) continue;
        // Every form busy for it: a card's form and the page's start form it
        // marked (markBusy) end with the same job.
        let cleared = false;
        for (const form of busy()) {
            if (form.getAttribute('data-async-target') === `#${n.id}`) cleared = clearBusy(form) || cleared;
        }
        if (cleared) return true;
    }
    return false;
}

// hasJob: a job's log (#log-<item>) with a job in it -- the action view
// (app/action.js marks the log itself), a player's view inside it (after a
// quiet move to the next episode syncPage leaves only that, in a fresh log),
// or a press still out for it.
export function hasJob(log) {
    if (log.hasAttribute('data-async-view') || log.querySelector('[data-async-view]')) return true;
    return Array.from(log.ownerDocument.querySelectorAll('form[data-busy="true"]'))
        .some((f) => f.getAttribute('data-async-target') === `#${log.id}`);
}

// clearPress: the forms busy from the press `form` made -- it, and the twin a
// swap rendered since (carryBusy) -- when the request itself is the ending.
export function clearPress(form, press) {
    clearBusy(form);
    if (!press) return;
    for (const f of document.querySelectorAll('form[data-busy="true"]')) {
        if (f.dataset.busyPress === press) clearBusy(f);
    }
}

// carryBusy: a busy form rendered anew. A swap that keeps the running job's
// log (lib/loadAsyncView.js) renders its start form again, and the fresh one
// is busy from the same press -- released by the job (app/action.js), a press
// on it still swallowed -- for what is left of the old one's timeout.
export function carryBusy(from, to, { now = Date.now } = {}) {
    if (!isBusy(from) || isBusy(to)) return false;
    const btn = submitButton(to);
    if (!btn) return false;
    const { busySince, busyPress, busyPressed } = from.dataset;
    const timeoutMs = from._busyTimer ? Math.max(1, BUSY_MAX_MS - (now() - Number(busySince))) : 0;
    clearBusy(from);
    show(to, btn, now, timeoutMs, busyPress);
    to.dataset.busySince = busySince;
    if (busyPressed) to.dataset.busyPressed = busyPressed;
    return true;
}

// countBusyPress counts presses on a working button, which nothing else
// sees: `.btn-busy` takes the pointer off the button, so the press lands on
// its form, and Umami tracks the button (data-umami-event), not the form.
// Those presses are what is left of the confusion the busy state was for --
// the viewer did not see that the page is working. One event per busy spell
// (the first press), with the action and how long it had been working, in
// seconds. Keyboard presses are swallowed in lib/async.js and not counted.
//
// A click listener ON each action form (lib/async.js binds it), not one on
// document: iOS Safari sends no click for a tap on an element that neither
// listens for clicks itself (or through an ancestor below <body>) nor has
// cursor:pointer, and a busy form had neither.
export function countBusyPress(e) {
    const form = e.currentTarget;
    if (!isBusy(form) || form.dataset.busyPressed) return;
    form.dataset.busyPressed = '1';
    if (!window.umami) return;
    const action = new URL(form.getAttribute('action') || '', window.location.href).pathname.match(ACTION_RE);
    const wait = Math.round((Date.now() - Number(form.dataset.busySince)) / 1000);
    window.umami.track('busy-press', { action: action ? action[1] : 'other', wait });
}

// resubmit starts the job again from its start form: the player's own
// restarts (lib/player/stream-restart.js, passthrough.js, preferred-lang.js).
// They replace the running job -- a fresh press, not a repeat -- and the busy
// state the first job left (until its player is shown) would swallow them in
// lib/async.js or lib/turnstileAction.js: a dead session or a refused codec
// before the first frame then restarted nothing.
export function resubmit(form) {
    clearBusy(form);
    // The submit is dispatched synchronously: Turnstile or async.js mark the
    // form inside requestSubmit(), and see the flag there.
    form._quietPress = true;
    try {
        form.requestSubmit();
    } finally {
        delete form._quietPress;
    }
}
