// Turnstile on job starts. The five action forms (download-file,
// download-dir, preview-image, stream-audio, stream-video) carry a token
// from a Turnstile widget; handlers/action refuses an anonymous start
// without one. The widget is a *managed* one rendered with appearance
// "interaction-only": nothing is shown while Cloudflare can vouch for the
// visitor silently, and when it wants a click the checkbox appears right
// under the form that was submitted — the container is moved next to it
// before the first render. (An invisible widget was tried first: it never
// shows the checkbox, so a visitor Cloudflare is unsure about just fails.)
//
// The submit is intercepted in the capture phase on document — before
// lib/async.js's own submit listener on the form — stopped, the token is
// fetched, written into a hidden input, and the form is re-submitted; the
// second pass carries a marker and is let through. Tokens are single-use,
// so the marker and the widget are reset after each pass.
//
// Fail closed: if the Turnstile script never loads (blocker, network) the
// form goes out after TOKEN_TIMEOUT_MS without a token and the server
// answers with the "couldn't confirm you're not a robot" card. Letting it
// through untagged would make "don't load the script" the bypass. A
// visible interactive challenge gets INTERACTIVE_TIMEOUT_MS instead —
// a person needs time to click.
const ACTIONS = /\/(download-file|download-dir|preview-image|stream-audio|stream-video)$/;
const TOKEN_TIMEOUT_MS = 6000;
const INTERACTIVE_TIMEOUT_MS = 120000;
const READY = 'turnstileReady';

let widgetId = null;
let pending = null;
let interactive = false;

function container() {
    return document.getElementById('turnstile-action');
}

function isActionForm(form) {
    if (!(form instanceof HTMLFormElement)) return false;
    let path;
    try { path = new URL(form.action, window.location.href).pathname; } catch (e) { return false; }
    return ACTIONS.test(path);
}

// place puts the widget container inside the form's job-log area
// (data-async-target: the full-width block under the buttons where the
// progress and the player will render), so an interactive challenge shows
// exactly where the job's own messages will. The log render replaces that
// block's innerHTML, so the widget must be gone before the form goes out:
// park() removes it and returns the container to <body>. A widget is
// therefore rendered fresh for every submit — it is execute-mode, cheap.
function place(form) {
    const el = container();
    if (!el) return;
    const sel = form.getAttribute('data-async-target');
    const target = sel ? document.querySelector(sel) : null;
    if (target && target !== form) target.appendChild(el);
    else form.insertAdjacentElement('afterend', el);
}

function park() {
    const el = container();
    if (widgetId !== null && typeof turnstile !== 'undefined') {
        try { turnstile.remove(widgetId); } catch (e) { /* already gone */ }
    }
    widgetId = null;
    if (el) { el.classList.add('hidden'); document.body.appendChild(el); }
}

// The container is shown only while Cloudflare wants a click.
function show(on) {
    const el = container();
    if (el) el.classList.toggle('hidden', !on);
}

function ensureWidget() {
    const el = container();
    if (!el || typeof turnstile === 'undefined') return false;
    if (widgetId === null) {
        interactive = false;
        widgetId = turnstile.render(el, {
            sitekey: el.dataset.sitekey,
            appearance: 'interaction-only',
            execution: 'execute',
            callback: (token) => { show(false); if (pending) { const p = pending; pending = null; p(token); } },
            'error-callback': () => { show(false); if (pending) { const p = pending; pending = null; p(''); } },
            'expired-callback': () => {},
            'before-interactive-callback': () => { interactive = true; show(true); if (pending && pending.extend) pending.extend(); },
        });
    }
    return widgetId !== null;
}

// getToken resolves with a fresh token, or '' when the widget is missing,
// errored or silent for TOKEN_TIMEOUT_MS.
function getToken() {
    return new Promise((resolve) => {
        if (!ensureWidget()) { resolve(''); return; }
        let done = false;
        let timer = null;
        const finish = (t) => { if (done) return; done = true; clearTimeout(timer); show(false); resolve(t || ''); };
        timer = setTimeout(() => finish(''), TOKEN_TIMEOUT_MS);
        // Cloudflare decided to show the checkbox: give the person time.
        finish.extend = () => { clearTimeout(timer); timer = setTimeout(() => finish(''), INTERACTIVE_TIMEOUT_MS); };
        pending = finish;
        try {
            turnstile.reset(widgetId);
            turnstile.execute(widgetId);
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
        // Remember the button that submitted, so requestSubmit() keeps its
        // name/value (select.js and the download split button rely on it).
        const submitter = e.submitter || null;
        place(form);
        getToken().then((token) => {
            park();
            setToken(form, token);
            form.dataset[READY] = '1';
            if (submitter && form.contains(submitter)) form.requestSubmit(submitter);
            else form.requestSubmit();
        });
    }, true);
}
