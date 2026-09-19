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
// So a form that started a job is BUSY: its submit button is disabled and says
// "preparing…" with a spinner, and a second submit is swallowed. The log below
// the button keeps its place -- it is the detail, the button is the state.
//
// Every busy state has to end, or a job that dies quietly leaves a dead
// button. Three ways out: the job log says `close` or `error` (the ordinary
// endings, including "no peers" and the cap modal), the request itself failed,
// or the safety timeout expires.

// A job may legitimately run for minutes (p99 of a stream job is ~8 min), so
// this is a backstop against a lost SSE, not a deadline.
export const BUSY_MAX_MS = 15 * 60 * 1000;

// Which forms this is for: the five action endpoints that start a job. Other
// async forms (the watched toggle, profile settings) answer instantly and
// would only flash.
const ACTION_RE = /\/(download-file|download-dir|preview-image|stream-audio|stream-video)$/;

// The form that owns each target, so the job log can find its way back to the
// button. Keyed by the target selector the form names.
const busyForms = new Map();

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
    form.dataset.busy = 'true';
    form.dataset.busySince = String(now());
    // The label is replaced, not appended: the button keeps its width class
    // and its icon, and what changes is what it says about itself.
    btn.dataset.busyLabel = btn.innerHTML;
    const label = form.dataset.busyText || btn.dataset.busyText || '…';
    btn.innerHTML = '';
    const spinner = document.createElement('span');
    spinner.className = 'loading loading-spinner loading-xs';
    spinner.setAttribute('aria-hidden', 'true');
    const text = document.createElement('span');
    text.textContent = label;
    btn.appendChild(spinner);
    btn.appendChild(text);
    btn.disabled = true;
    btn.setAttribute('aria-busy', 'true');
    const target = form.getAttribute('data-async-target');
    if (target) busyForms.set(target, form);
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
    const target = form.getAttribute('data-async-target');
    if (target && busyForms.get(target) === form) busyForms.delete(target);
    const btn = submitButton(form);
    if (btn) {
        if (btn.dataset.busyLabel !== undefined) {
            // SAFETY: the markup this restores is the button's own, captured
            // from innerHTML a moment ago -- nothing from the network.
            btn.innerHTML = btn.dataset.busyLabel;
            delete btn.dataset.busyLabel;
        }
        btn.disabled = false;
        btn.removeAttribute('aria-busy');
    }
    return true;
}

// clearBusyFor releases the form whose job wrote into `el` -- the log element,
// or anything inside it. The form names its target by selector, so the walk is
// up the ancestors looking for an id a busy form points at.
export function clearBusyFor(el) {
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
        if (!n.id) continue;
        const form = busyForms.get(`#${n.id}`);
        if (form) return clearBusy(form);
    }
    return false;
}
