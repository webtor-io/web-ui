// The job-start buttons (Watch, Download, …) while their job is running.
//
// Measured 2026-09-19: from the first click to the first frame the median is
// 58 s and only 4.8% of viewers are playing within 10 s -- the wait is the
// torrent swarm, not our code (a cold file spends ~21 s warming up and
// ~6 s buffering, and the slow third of jobs takes over a minute). The button
// stayed live through all of it, so 59% of streaming sessions pressed it more
// than once; two thirds of the clicks that recorded nothing were simply the
// same button again, a median 17 s later. Each of those is another POST and
// another job.
//
// So a form that started a job is BUSY: its submit button keeps its label,
// swaps its icon for a spinner and goes dim (`.btn-busy` -- not `disabled`: it
// is working, not refusing), and a second submit is swallowed (lib/async.js).
// The log below the button keeps its place -- it is the detail, the button is
// the state.
//
// Every busy state has to end, or a job that dies quietly leaves a dead
// button. The ways out: the job log reports an ending (`close`, `error` --
// no peers, the cap modal --, `rendertemplate` -- the player is up --,
// `download`, `redirect`; lib/progressLog.js), the request itself failed
// (lib/async.js), or the safety timeout below expires.

// A job may legitimately run for minutes (p99 of a stream job is ~8 min), so
// this is a backstop against a lost SSE, not a deadline.
export const BUSY_MAX_MS = 15 * 60 * 1000;

// Which forms this is for: the five action endpoints that start a job. Other
// async forms (the watched toggle, profile settings) answer instantly and
// would only flash.
const ACTION_RE = /\/(download-file|download-dir|preview-image|stream-audio|stream-video)$/;

export function isActionForm(form) {
    if (!form || form.tagName !== 'FORM') return false;
    let path;
    try {
        path = new URL(form.getAttribute('action') || '', window.location.href).pathname;
    } catch (e) {
        return false;
    }
    return ACTION_RE.test(path);
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
    // One target, one working button. Watch and Download of a file card
    // share a log container (#log-<item>), and a new job's response replaces
    // whatever log was in it -- so a form still busy for that target has just
    // lost the only thing that could release it. Release it here.
    const target = form.getAttribute('data-async-target');
    if (target) {
        for (const other of document.querySelectorAll('form[data-busy="true"]')) {
            if (other !== form && other.getAttribute('data-async-target') === target) clearBusy(other);
        }
    }
    form.dataset.busy = 'true';
    form.dataset.busySince = String(now());
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
    return true;
}

export function clearBusy(form) {
    if (!form || !isBusy(form)) return false;
    delete form.dataset.busy;
    delete form.dataset.busySince;
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
        for (const form of busy()) {
            if (form.getAttribute('data-async-target') === `#${n.id}`) return clearBusy(form);
        }
    }
    return false;
}
