// Turnstile on job starts. The five action forms (download-file,
// download-dir, preview-image, stream-audio, stream-video) carry a token
// from an *invisible* Turnstile widget; handlers/action refuses an anonymous
// start without one. The widget lives in #turnstile-action (layouts/main),
// rendered once with execution "execute" so nothing happens until a submit.
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
// through untagged would make "don't load the script" the bypass.
const ACTIONS = /\/(download-file|download-dir|preview-image|stream-audio|stream-video)$/;
const TOKEN_TIMEOUT_MS = 6000;
const READY = 'turnstileReady';

let widgetId = null;
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

function ensureWidget() {
    const el = container();
    if (!el || typeof turnstile === 'undefined') return false;
    if (widgetId === null) {
        widgetId = turnstile.render(el, {
            sitekey: el.dataset.sitekey,
            execution: 'execute',
            callback: (token) => { if (pending) { const p = pending; pending = null; p(token); } },
            'error-callback': () => { if (pending) { const p = pending; pending = null; p(''); } },
            'expired-callback': () => {},
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
        const finish = (t) => { if (done) return; done = true; clearTimeout(timer); resolve(t || ''); };
        const timer = setTimeout(() => finish(''), TOKEN_TIMEOUT_MS);
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
        getToken().then((token) => {
            setToken(form, token);
            form.dataset[READY] = '1';
            if (submitter && form.contains(submitter)) form.requestSubmit(submitter);
            else form.requestSubmit();
        });
    }, true);
}
