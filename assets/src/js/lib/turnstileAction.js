// Turnstile on job starts. The five action forms (download-file,
// download-dir, preview-image, stream-audio, stream-video) carry a token
// from a Turnstile widget; handlers/action refuses an anonymous start
// without one. The widget is a *managed* one rendered with appearance
// "interaction-only": nothing is shown while Cloudflare can vouch for the
// visitor silently, and when it wants a click the checkbox appears in the
// form's job-log block, under a "checking that you are not a robot" step
// that is written there the moment the button is pressed. (An invisible
// widget was tried first: it never shows the checkbox, so a visitor
// Cloudflare is unsure about just fails.)
//
// Two widgets, because a Turnstile widget does not survive being moved in
// the DOM (the challenge iframe dies; checked on stage 2026-09-10):
//  - the *warm* one is rendered at page load into #turnstile-action, which
//    stays hidden in <body>; its challenge is loaded ahead of time so the
//    silent path costs one execute() — around a second — not a cold render;
//  - when the warm one asks for a click, it is dropped and a *live* one is
//    rendered fresh inside the step block, where the person sees it.
//
// The submit is intercepted in the capture phase on document — before
// lib/async.js's own submit listener on the form — stopped, the step line
// is written, the token is fetched, put into a hidden input, and the form
// is re-submitted; the second pass carries a marker and is let through.
// Tokens are single-use, so the marker is cleared after each pass and the
// warm widget is reset for the next one.
//
// Fail closed: if the Turnstile script never loads (blocker, network) the
// form goes out at once without a token and the server answers with the
// "couldn't confirm you're not a robot" card. Letting it through untagged
// would make "don't load the script" the bypass. A loaded script that
// stays silent gets SILENT_TIMEOUT_MS; a visible checkbox gets
// INTERACTIVE_TIMEOUT_MS — a person needs time to click.
const ACTIONS = /\/(download-file|download-dir|preview-image|stream-audio|stream-video)$/;
const SILENT_TIMEOUT_MS = 15000;
const INTERACTIVE_TIMEOUT_MS = 120000;
const SCRIPT_WAIT_MS = 15000;
const READY = 'turnstileReady';
const STEP_TAG = 'turnstile';

let warmId = null;
let liveId = null;
let pending = null;

function container() {
    return document.getElementById('turnstile-action');
}

function isActionForm(form) {
    if (!(form instanceof HTMLFormElement)) return false;
    let path;
    try { path = new URL(form.action, window.location.href).pathname; } catch (e) { return false; }
    return ACTIONS.test(path);
}

function hasTurnstile() {
    return typeof turnstile !== 'undefined' && turnstile && typeof turnstile.render === 'function';
}

// widgetOptions builds the render options; onInteractive is what to do
// when Cloudflare decides it wants a click from this widget.
function widgetOptions(onInteractive) {
    return {
        sitekey: container().dataset.sitekey,
        appearance: 'interaction-only',
        execution: 'execute',
        callback: (token) => { if (pending) pending.finish(token); },
        'error-callback': () => { if (pending) pending.finish(''); },
        'expired-callback': () => {},
        'before-interactive-callback': onInteractive,
    };
}

// warmUp renders the hidden widget once the script is there. The script is
// async/defer, so poll briefly; a page whose script never arrives simply
// has no warm widget and the submit path fails closed at once.
function warmUp() {
    const started = Date.now();
    const tick = () => {
        if (warmId !== null) return;
        if (hasTurnstile()) { renderWarm(); return; }
        if (Date.now() - started < SCRIPT_WAIT_MS) setTimeout(tick, 250);
    };
    tick();
}

function renderWarm() {
    const el = container();
    if (!el || !hasTurnstile() || warmId !== null) return;
    try {
        warmId = turnstile.render(el, widgetOptions(() => {
            // Cloudflare wants a click: the hidden widget cannot take it,
            // and moving it kills it — render a live one where the person
            // looks, inside the step block of the form being submitted.
            if (!pending) return;
            dropWarm();
            renderLive(pending.slot);
        }));
    } catch (e) {
        warmId = null;
    }
}

function dropWarm() {
    if (warmId !== null) {
        try { turnstile.remove(warmId); } catch (e) { /* already gone */ }
        warmId = null;
    }
}

function renderLive(slot) {
    if (!slot || !hasTurnstile()) { if (pending) pending.finish(''); return; }
    try {
        liveId = turnstile.render(slot, widgetOptions(() => {
            if (pending) pending.interactive();
        }));
        turnstile.execute(liveId);
    } catch (e) {
        liveId = null;
        if (pending) pending.finish('');
    }
}

function dropLive() {
    if (liveId !== null) {
        try { turnstile.remove(liveId); } catch (e) { /* already gone */ }
        liveId = null;
    }
}

// stepBlock writes the "checking" step into the form's job-log block —
// the same markup progressLog.js produces for a job's own lines, so it
// reads as the first step of the job — and returns the slot under it
// where a live widget goes. The server's reply to the submit replaces the
// whole block, step included. Forms without a log block get no step and
// the live widget is placed right after the form.
function stepBlock(form) {
    const sel = form.getAttribute('data-async-target');
    const target = sel ? document.querySelector(sel) : null;
    const slot = document.createElement('div');
    slot.className = 'px-5 pt-2 empty:hidden';
    if (!target || target === form) {
        slot.className = 'mt-3';
        form.insertAdjacentElement('afterend', slot);
        return { slot, line: null, detach: () => slot.remove() };
    }
    const label = (container() && container().dataset.label) || '';
    const block = document.createElement('div');
    block.className = 'progress-alert progress-alert-block mt-6 mb-10';
    const lt = document.createElement('div');
    lt.className = 'log-target';
    const pre = document.createElement('pre');
    pre.className = 'inprogress';
    pre.setAttribute('task-tag', STEP_TAG);
    const line = document.createElement('span');
    line.className = 'line';
    line.innerText = label;
    const status = document.createElement('span');
    status.className = 'task-status';
    line.appendChild(status);
    pre.appendChild(line);
    lt.appendChild(pre);
    lt.appendChild(slot);
    block.appendChild(lt);
    target.replaceChildren(block);
    return { slot, line: pre, detach: () => {} };
}

// getToken resolves with a fresh token, or '' when the widget is missing,
// errored or silent past the deadline. The step block is left in place: a
// success is replaced by the job's own log, a refusal by the server's card.
function getToken(form) {
    return new Promise((resolve) => {
        if (!hasTurnstile() || !container()) { resolve(''); return; }
        const step = stepBlock(form);
        let done = false;
        let timer = null;
        const finish = (t) => {
            if (done) return;
            done = true;
            clearTimeout(timer);
            pending = null;
            dropLive();
            if (step.line) step.line.classList.replace('inprogress', t ? 'done' : 'error');
            step.detach();
            // A used warm widget is reset for the next start; one that
            // failed is dropped and rendered anew, an errored widget stays
            // errored.
            if (t && warmId !== null) { try { turnstile.reset(warmId); } catch (e) { dropWarm(); } }
            else dropWarm();
            if (warmId === null) renderWarm();
            resolve(t || '');
        };
        const interactive = () => {
            clearTimeout(timer);
            timer = setTimeout(() => finish(''), INTERACTIVE_TIMEOUT_MS);
        };
        pending = { finish, interactive, slot: step.slot };
        timer = setTimeout(() => finish(''), SILENT_TIMEOUT_MS);
        if (warmId === null) renderWarm();
        if (warmId === null) { finish(''); return; }
        try {
            turnstile.execute(warmId);
        } catch (e) {
            finish('');
        }
    });
}

function setToken(form, token) {
    let input = form.querySelector('input[name="cf-turnstile-response"]');
    if (!input) {
        input = document.createElement('input');
        input.type = 'hidden';
        input.name = 'cf-turnstile-response';
        form.appendChild(input);
    }
    input.value = token;
}

export default function init() {
    if (!container()) return; // widget not configured: nothing to gate
    warmUp();
    document.addEventListener('submit', (e) => {
        const form = e.target;
        if (!isActionForm(form)) return;
        if (form.dataset[READY]) {
            // second pass, token attached — let async.js take it
            setTimeout(() => { delete form.dataset[READY]; }, 0);
            return;
        }
        e.preventDefault();
        e.stopImmediatePropagation();
        if (pending) return; // a check is already running for some form
        // Remember the button that submitted, so requestSubmit() keeps its
        // name/value (select.js and the download split button rely on it).
        const submitter = e.submitter || null;
        getToken(form).then((token) => {
            setToken(form, token);
            form.dataset[READY] = '1';
            if (submitter && form.contains(submitter)) form.requestSubmit(submitter);
            else form.requestSubmit();
        });
    }, true);
}
