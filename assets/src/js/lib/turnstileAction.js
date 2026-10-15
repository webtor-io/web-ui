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
import { isBusy, markBusy } from './actionBusy';
import { reveal } from './stickyStatus';

const ACTIONS = /\/(download-file|download-dir|preview-image|stream-audio|stream-video)$/;
const SILENT_TIMEOUT_MS = 15000;
const INTERACTIVE_TIMEOUT_MS = 120000;
const SCRIPT_WAIT_MS = 15000;
const READY = 'turnstileReady';
// When the page (and this module) loaded: a press waits for a script still
// on its way until SCRIPT_WAIT_MS after this, not after the press.
const LOADED_AT = Date.now();

// One state per page, not one per copy of this module. splitChunks is off by
// design (webpack.config.js: Go template helpers load entries independently),
// so the page entry and the lazy player chunk -- which calls silentToken()
// for a background start -- each get their own copy of this file. With the
// state in module scope the chunk's copy saw warmId === null and would render
// a SECOND widget into the one #turnstile-action container, and callbacks
// from the page's widget resolved a `pending` the chunk could not see. Same
// root cause as the job-start spinner that never stopped (actionBusy.js,
// 2026-09-20), and the same cure: keep shared state somewhere both copies
// look.
if (!window.__turnstileAction) window.__turnstileAction = { warmId: null, liveId: null, pending: null };
const st = window.__turnstileAction;

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
        callback: (token) => { if (st.pending) st.pending.finish(token); },
        'error-callback': () => { if (st.pending) st.pending.finish('', 'widget-error'); },
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
        if (st.warmId !== null) return;
        if (hasTurnstile()) { renderWarm(); return; }
        if (Date.now() - started < SCRIPT_WAIT_MS) setTimeout(tick, 250);
    };
    tick();
}

function renderWarm() {
    const el = container();
    if (!el || !hasTurnstile() || st.warmId !== null) return;
    try {
        st.warmId = turnstile.render(el, widgetOptions(() => {
            // Cloudflare wants a click: the hidden widget cannot take it,
            // and moving it kills it — render a live one where the person
            // looks, inside the step block of the form being submitted.
            if (!st.pending) return;
            // A start nobody pressed a button for (silentToken) has nowhere
            // to show a checkbox and nobody waiting to click it.
            if (st.pending.silentOnly) { st.pending.finish('', 'needs-interaction'); return; }
            dropWarm();
            renderLive(st.pending.slot);
        }));
    } catch (e) {
        st.warmId = null;
    }
}

function dropWarm() {
    if (st.warmId !== null) {
        try { turnstile.remove(st.warmId); } catch (e) { /* already gone */ }
        st.warmId = null;
    }
}

function renderLive(slot) {
    if (!slot || !hasTurnstile()) { if (st.pending) st.pending.finish('', 'no-live-slot'); return; }
    // The normal widget is 300 px wide: on a phone narrower than about 350 px
    // it ran past the card and widened the page. Compact (150x140) there.
    const narrow = slot.clientWidth > 0 && slot.clientWidth < 300;
    try {
        st.liveId = turnstile.render(slot, { ...widgetOptions(() => {
            if (st.pending) st.pending.interactive();
        }), size: narrow ? 'compact' : 'normal' });
        turnstile.execute(st.liveId);
        // The checkbox waits for a click for up to two minutes: where the
        // eye is, and the line says what to do -- the wait is the viewer's
        // now, not ours (no spinner, no bar). The widget's iframe sizes
        // itself later, so the slot holds its height (65 px, Turnstile's
        // normal size) from now.
        slot.classList.remove('empty:hidden');
        slot.style.minHeight = narrow ? '140px' : '65px';
        const block = slot.closest('.progress-alert');
        const stage = block && block.querySelector('.job-stage');
        const ask = container() && container().dataset.labelInteractive;
        if (stage && ask) {
            stage.querySelector('.job-stage-title').textContent = ask;
            stage.classList.add('job-stage-done');
        }
        // The slot itself, not the block: in a card's log (a retry form) the
        // block is the whole job log, taller than the screen, and pinning
        // its top pushed the checkbox out of view.
        reveal(slot);
    } catch (e) {
        st.liveId = null;
        if (st.pending) st.pending.finish('', 'live-render-failed');
    }
}

function dropLive() {
    if (st.liveId !== null) {
        try { turnstile.remove(st.liveId); } catch (e) { /* already gone */ }
        st.liveId = null;
    }
}

// stepBlock writes the "checking" step into the form's job-log block —
// the same markup progressLog.js produces for a job's own lines, so it
// reads as the first step of the job — and returns the slot under it
// where a live widget goes. The server's reply to the submit replaces the
// whole block, step included. Forms without a log block get no step and
// the live widget is placed right after the form -- and so do forms that
// live INSIDE their own log block (the retry and "continue at slow speed"
// forms of the no-peers and slow cards): replacing the block would detach
// the very form, and requestSubmit() on a form that is not connected does
// nothing, so for an anonymous visitor those buttons did nothing at all.
function stepBlock(form) {
    const sel = form.getAttribute('data-async-target');
    const target = sel ? document.querySelector(sel) : null;
    const slot = document.createElement('div');
    slot.className = 'px-5 pt-2 empty:hidden';
    if (!target || target.contains(form)) {
        slot.className = 'mt-3';
        form.insertAdjacentElement('afterend', slot);
        return { slot, line: null, detach: () => slot.remove() };
    }
    // The job log's own stage line (views/action/post.html, .job-stage): the
    // check reads as the job's first step.
    const label = (container() && container().dataset.label) || '';
    const block = document.createElement('div');
    block.className = 'progress-alert progress-alert-block';
    block.setAttribute('data-turnstile-step', '');
    const stage = document.createElement('div');
    stage.className = 'job-stage';
    const row = document.createElement('div');
    row.className = 'job-stage-row';
    const spin = document.createElement('span');
    spin.className = 'loading loading-spinner loading-sm job-stage-spin';
    spin.setAttribute('aria-hidden', 'true');
    const title = document.createElement('span');
    title.className = 'job-stage-title';
    title.textContent = label;
    row.append(spin, title);
    const bar = document.createElement('progress');
    bar.className = 'progress job-stage-bar';
    bar.max = 100;
    stage.append(row, bar);
    block.append(stage, slot);
    target.replaceChildren(block);
    reveal(block);
    return { slot, line: null, stage, label, detach: () => {} };
}

// getToken resolves with a fresh token, or '' when the widget is missing,
// errored or silent past the deadline. The step block is left in place: a
// success is replaced by the job's own log, a refusal by the server's card.
function getToken(form) {
    return new Promise((resolve) => {
        if (!container()) { form.dataset.turnstileReason = 'no-script:0'; resolve(''); return; }
        if (!hasTurnstile()) {
            // The script is async/defer and may still be on its way: a slow
            // phone, or a deep link that presses at load (#action=stream,
            // app/resource/get.js) -- on stage, 3 of 7 throttled loads were
            // refused 0.5-3.4 s in. The step on screen, the press held, until
            // SCRIPT_WAIT_MS after the page loaded; only then fail closed.
            const step = stepBlock(form);
            let ended = false;
            const end = () => { ended = true; st.pending = null; };
            st.pending = { finish: () => { end(); resolve(''); }, interactive: () => {}, slot: step.slot };
            const tick = () => {
                if (ended) return;
                if (hasTurnstile()) { end(); getToken(form).then(resolve); return; }
                if (Date.now() - LOADED_AT >= SCRIPT_WAIT_MS) {
                    end();
                    form.dataset.turnstileReason = `no-script:${Date.now() - LOADED_AT}`;
                    resolve('');
                    return;
                }
                setTimeout(tick, 250);
            };
            tick();
            return;
        }
        const step = stepBlock(form);
        let done = false;
        let timer = null;
        const started = Date.now();
        const finish = (t, reason) => {
            if (done) return;
            done = true;
            clearTimeout(timer);
            st.pending = null;
            dropLive();
            form.dataset.turnstileReason = t ? '' : `${reason || 'unknown'}:${Date.now() - started}`;
            if (step.line) step.line.classList.replace('inprogress', t ? 'done' : 'error');
            // Ticked: the line goes back to working until the job's log
            // replaces it -- it said "tick the box" over an empty slot for
            // the length of the request.
            if (t && step.stage) {
                step.stage.querySelector('.job-stage-title').textContent = step.label;
                step.stage.classList.remove('job-stage-done');
                step.slot.style.minHeight = '';
            }
            step.detach();
            // A used warm widget is reset for the next start; one that
            // failed is dropped and rendered anew, an errored widget stays
            // errored.
            if (t && st.warmId !== null) { try { turnstile.reset(st.warmId); } catch (e) { dropWarm(); } }
            else dropWarm();
            if (st.warmId === null) renderWarm();
            resolve(t || '');
        };
        const interactive = () => {
            clearTimeout(timer);
            timer = setTimeout(() => finish('', 'interactive-timeout'), INTERACTIVE_TIMEOUT_MS);
        };
        st.pending = { finish, interactive, slot: step.slot };
        timer = setTimeout(() => finish('', 'silent-timeout'), SILENT_TIMEOUT_MS);
        if (st.warmId === null) renderWarm();
        if (st.warmId === null) { finish('', 'no-widget'); return; }
        try {
            turnstile.execute(st.warmId);
        } catch (e) {
            finish('', 'execute-failed');
        }
    });
}

// silentToken is a token for a job start the page makes on the viewer's
// behalf, with no form being submitted and nothing on screen to host a
// checkbox (the player re-rendering its subtitles dialog for another
// preferred language). Only the silent path: when Cloudflare wants a click
// the answer is "", and the caller falls back to a start the viewer can see.
// "" as well when the widget is not configured or a check is already
// running.
const BACKGROUND_TIMEOUT_MS = 8000;
export function silentToken() {
    return new Promise((resolve) => {
        if (!hasTurnstile() || !container() || st.pending) { resolve(''); return; }
        let done = false;
        let timer = null;
        const finish = (t) => {
            if (done) return;
            done = true;
            clearTimeout(timer);
            st.pending = null;
            if (t && st.warmId !== null) { try { turnstile.reset(st.warmId); } catch (e) { dropWarm(); } }
            else dropWarm();
            if (st.warmId === null) renderWarm();
            resolve(t || '');
        };
        st.pending = { finish, interactive: () => finish(''), slot: null, silentOnly: true };
        timer = setTimeout(() => finish(''), BACKGROUND_TIMEOUT_MS);
        if (st.warmId === null) renderWarm();
        if (st.warmId === null) { finish(''); return; }
        try {
            turnstile.execute(st.warmId);
        } catch (e) {
            finish('');
        }
    });
}

function hidden(form, name, value) {
    let input = form.querySelector(`input[name="${name}"]`);
    if (!input) {
        input = document.createElement('input');
        input.type = 'hidden';
        input.name = name;
        form.appendChild(input);
    }
    input.value = value;
}

// setToken writes the token and, when there is none, why — the server logs
// the reason next to siteverify's code, so a refusal can be read as "the
// script never ran", "the widget was silent for 15 s" or "the checkbox was
// shown and nobody clicked it for two minutes".
function setToken(form, token) {
    hidden(form, 'cf-turnstile-response', token);
    hidden(form, 'cf-turnstile-reason', token ? '' : (form.dataset.turnstileReason || 'unknown'));
    delete form.dataset.turnstileReason;
}

// closeOwnDialog: a press dropped because another form's check is running
// must not leave the dialog its onclick opened (TAR/ZIP, partials/list.html)
// spinning over a job that never started.
function closeOwnDialog(form) {
    const sel = form.getAttribute('data-async-target');
    const target = sel ? document.querySelector(sel) : null;
    const dialog = target && target.closest('dialog');
    if (!dialog || !dialog.open) return;
    if (st.pending && st.pending.slot && dialog.contains(st.pending.slot)) return; // that check is this dialog's
    dialog.close();
}

export default function init() {
    if (!container()) return; // widget not configured: nothing to gate
    warmUp();
    document.addEventListener('submit', (e) => {
        const form = e.target;
        if (!isActionForm(form)) return;
        // A signed-in account is not checked by the server, so it gets no
        // step and no widget either. The layout renders the container only
        // for anonymous visitors; _userId (set by the nav partial, which
        // async navigation re-renders) covers a sign-in within the same
        // page session.
        if (window._userId) return;
        if (form.dataset[READY]) {
            // second pass, token attached — let async.js take it
            setTimeout(() => { delete form.dataset[READY]; }, 0);
            return;
        }
        e.preventDefault();
        e.stopImmediatePropagation();
        // A working button's form, pressed again (Enter: the pointer cannot
        // reach a busy button): swallowed here, as lib/async.js would -- the
        // check would spend a token and write its step over the running
        // job's log.
        if (isBusy(form)) return;
        // A check whose step went with its log (another file picked, the
        // page swapped) has nobody waiting on it: ended here, or every press
        // on the page was dropped until its two-minute deadline.
        if (st.pending && st.pending.slot && !st.pending.slot.isConnected) st.pending.finish('', 'detached');
        if (st.pending) { // a check is already running for some form
            closeOwnDialog(form);
            return;
        }
        // Busy from the press, not from the token: the check takes a second,
        // fifteen when Cloudflare is slow, two minutes when it wants a click,
        // and the button looked idle through all of it. async.js lets the
        // second pass of a busy form through (turnstileReady).
        markBusy(form);
        // Remember the button that submitted, so requestSubmit() keeps its
        // name/value (select.js and the download split button rely on it).
        const submitter = e.submitter || null;
        getToken(form).then((token) => {
            setToken(form, token);
            // In a task of its own, not as a microtask of this press: an
            // answer that comes at once (no script yet on a slow phone, a
            // blocked one, a widget that failed) resolves inside the press's
            // own submit dispatch, where a browser ignores requestSubmit().
            // The press then did nothing -- and with the button busy from
            // the press, it stayed dead for BUSY_MAX_MS.
            setTimeout(() => {
                if (!form.isConnected) return; // swapped away meanwhile: nothing to send
                form.dataset[READY] = '1';
                if (submitter && form.contains(submitter)) form.requestSubmit(submitter);
                else form.requestSubmit();
            }, 0);
        });
    }, true);
}
