// The page's HEVC passthrough declaration (docs/player.md, "The
// declaration"): the `decode` field a stream start sends, which
// content-transcoder reads to decide whether this browser gets the source's
// HEVC as it is or the H.264 it has always had. The page only declares; the
// transcoder decides.
//
// Who declares. A browser takes part once `?passthrough=on` was opened on any
// page (localStorage `wt-passthrough`); `?passthrough=off` takes it out again.
// Until the owner's stage 5 nobody else does: the transcoder is one for
// production and stage, so "production does not declare yet" cannot be a
// deployment -- it is this per-browser opt-in (stage 3 spec, D1). A page that
// does not take part sends no `decode` field and runs no probe: it costs
// nothing.
//
// What it declares -- all or nothing (D4):
//   1. not taking part                     -> no field;
//   2. this file failed passthrough here   -> no field (the old route for it);
//   3. the probe answered                  -> its tokens minus the ones the
//                                             memory of failures took away;
//                                             none left -> no field;
//   4. the probe has not answered          -> the cached answer of this same
//                                             browser (User-Agent, 30 days);
//                                             none -> `unknown`.
// HEVC tokens are never sent without the `hdr-pq` answer: the transcoder
// would read the missing token as "does not decode HDR" and refuse a 4K HDR
// film with a false reason. A check that did not answer is not a browser
// that cannot decode.
//
// Every piece of state is on window.__wtDecode, not in this module: the
// module is imported from several entries (layout, resource page, the
// player chunk) and each copy would otherwise keep its own (web-ui
// CLAUDE.md, splitChunks is off). Every storage access is wrapped: a browser
// that throws on localStorage (blocked site data, some private modes) gets
// the page's own memory and never an exception -- this runs in layout.js,
// before Turnstile and the async navigation are set up.
//
// The probe itself (codec-support.js) is loaded only when the page takes
// part, so the layout entry carries this module and nothing of the probe.

export const OPTIN_KEY = 'wt-passthrough';
export const CACHE_KEY = 'wt-decode';
export const MEMORY_KEY = 'wt-decode-fallback';
export const MEMORY_TTL_MS = 7 * 24 * 3600 * 1000;
export const CACHE_TTL_MS = 30 * 24 * 3600 * 1000;
// Failures on this many different files within MEMORY_TTL_MS take a class
// of decoder out of the declaration (plan §5.1): one failure is the file's.
export const STRIKES = 2;
const STATE = '__wtDecode';
const UNKNOWN = 'unknown';

// The tokens a declaration may carry, in the transcoder's order
// (codec-support.js DECODE_TOKENS; a test pins the two together). Not
// imported from there: that module is the probe, and this one is in the
// layout of every page.
export const TOKENS = ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160', 'hevc-high', 'hdr-pq'];

// What a failure of a decoder class takes out: every token that covers the
// class (content-transcoder route.go `covers`: Main10 covers Main, a 2160
// token covers its depth at 1080). hevc-high and hdr-pq are never taken:
// the page cannot tell a tier or a PQ failure from a size one.
export const STRUCK_BY_CLASS = {
    'hevc8': ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160'],
    'hevc10': ['hevc10', 'hevc10-2160'],
    'hevc8-2160': ['hevc8-2160', 'hevc10-2160'],
    'hevc10-2160': ['hevc10-2160'],
};

function state(win) {
    let s = win[STATE];
    if (!s) {
        s = { optin: undefined, probe: null, fresh: null, ready: null, readyResolve: null, memory: null, hooked: false };
        s.ready = new Promise((r) => { s.readyResolve = r; });
        win[STATE] = s;
    }
    return s;
}

// storage is only ever reached through read and write, which catch: the
// getter itself throws in a browser with site data blocked.
function storage(win) {
    return win.localStorage || null;
}

function read(win, key) {
    try {
        const st = storage(win);
        return st ? st.getItem(key) : null;
    } catch (e) {
        return null;
    }
}

function write(win, key, value) {
    try {
        const st = storage(win);
        if (st) st.setItem(key, value);
    } catch (e) {
        // No storage: the page's own state is all there is.
    }
}

function userAgent(win) {
    try {
        return String(win.navigator.userAgent || '');
    } catch (e) {
        return '';
    }
}

// applyUrlSwitch reads `?passthrough=on|off` from the address and remembers
// it for this browser (and for this page, where there is no storage).
// Returns what it applied, or null.
export function applyUrlSwitch(win) {
    let v = null;
    try {
        v = new URLSearchParams(win.location.search).get('passthrough');
    } catch (e) {
        return null;
    }
    if (v !== 'on' && v !== 'off') return null;
    state(win).optin = v;
    write(win, OPTIN_KEY, v);
    return v;
}

// takesPart: does this page send a declaration? Before stage 5 only a
// browser that opted in; stage 5 makes it everyone but those who opted out
// (`!== 'off'`).
export function takesPart(win) {
    const s = state(win);
    const v = s.optin !== undefined ? s.optin : read(win, OPTIN_KEY);
    return v === 'on';
}

const tokensOf = (fresh) => [...fresh.hevc, ...(fresh.pq === 'yes' ? ['hdr-pq'] : [])];

// freshTokens: the probe's answer on this page, null until it is complete
// (the HEVC part is at once; `hdr-pq` waits for decodingInfo, with no
// deadline -- codec-support.js pqAnswer).
function freshTokens(win) {
    const f = state(win).fresh;
    return f && f.pq !== 'pending' ? tokensOf(f) : null;
}

function cachedTokens(win, now) {
    try {
        const c = JSON.parse(read(win, CACHE_KEY) || 'null');
        if (!c || c.ua !== userAgent(win) || !Array.isArray(c.tokens)) return null;
        if (typeof c.at !== 'number' || c.at > now || now - c.at > CACHE_TTL_MS) return null;
        return TOKENS.filter((t) => c.tokens.includes(t));
    } catch (e) {
        return null;
    }
}

// startProbe asks the browser once per page, in the background: the
// probe module is loaded here, not bundled into the layout. The answer is
// kept on the page and, complete, in the cache for the next page's first
// seconds.
export function startProbe(win, { load = () => import(/* webpackChunkName: "decode-probe" */ './codec-support.js'), env } = {}) {
    const s = state(win);
    if (s.probe) return s.probe;
    s.probe = (async () => {
        const cs = await load();
        const sup = cs.declarationSupport(env || cs.envFromWindow(win));
        s.fresh = { hevc: sup.hevc, pq: sup.hevc.length ? 'pending' : 'no' };
        if (s.fresh.pq === 'pending') s.fresh.pq = (await sup.pq) ? 'yes' : 'no';
        write(win, CACHE_KEY, JSON.stringify({ ua: userAgent(win), tokens: tokensOf(s.fresh), at: Date.now() }));
        s.readyResolve();
    })().catch(() => {
        // A probe that failed to load answers nothing: the cache, or
        // `unknown`, as for one that is still running.
    });
    return s.probe;
}

// whenDeclared resolves once the probe has answered completely, or after
// `ms`, whichever is first; at once where no probe runs.
export function whenDeclared(win, ms) {
    const s = state(win);
    if (!s.probe || freshTokens(win) !== null) return Promise.resolve();
    return Promise.race([s.ready, new Promise((r) => setTimeout(r, ms))]);
}

// ---- the memory of failures -------------------------------------------------

function emptyMemory() {
    return { sources: {}, strikes: {} };
}

function pruned(m, now) {
    const out = emptyMemory();
    for (const [src, at] of Object.entries((m && m.sources) || {})) {
        if (typeof at === 'number' && at <= now && now - at < MEMORY_TTL_MS) out.sources[src] = at;
    }
    for (const [cls, list] of Object.entries((m && m.strikes) || {})) {
        if (!STRUCK_BY_CLASS[cls] || !Array.isArray(list)) continue;
        const kept = list.filter((x) => x && typeof x.src === 'string' && typeof x.at === 'number' && x.at <= now && now - x.at < MEMORY_TTL_MS);
        if (kept.length) out.strikes[cls] = kept;
    }
    return out;
}

// loadMemory: the stored memory joined with the page's own (the page keeps
// it where there is no storage), both pruned to MEMORY_TTL_MS.
export function loadMemory(win, now = Date.now()) {
    const s = state(win);
    let stored = null;
    try {
        stored = JSON.parse(read(win, MEMORY_KEY) || 'null');
    } catch (e) {
        stored = null;
    }
    const a = pruned(stored, now);
    const b = pruned(s.memory, now);
    for (const [src, at] of Object.entries(b.sources)) {
        if (!a.sources[src] || a.sources[src] < at) a.sources[src] = at;
    }
    for (const [cls, list] of Object.entries(b.strikes)) {
        const have = a.strikes[cls] || [];
        for (const x of list) {
            if (!have.some((y) => y.src === x.src && y.at === x.at)) have.push(x);
        }
        a.strikes[cls] = have;
    }
    s.memory = a;
    return a;
}

function saveMemory(win, m) {
    state(win).memory = m;
    write(win, MEMORY_KEY, JSON.stringify(m));
}

const sourceKey = (resourceId, itemId) => (resourceId && itemId ? `${resourceId}/${itemId}` : '');

// struckTokens: the tokens the memory takes out -- the classes that failed
// on STRIKES different files within the memory's lifetime.
export function struckTokens(m) {
    const out = new Set();
    for (const [cls, list] of Object.entries(m.strikes)) {
        if (new Set(list.map((x) => x.src)).size >= STRIKES) {
            for (const t of STRUCK_BY_CLASS[cls]) out.add(t);
        }
    }
    return out;
}

// rememberFallback records a passthrough that failed on this page: the file
// (its next start sends no declaration), and, when `strike`, a strike
// against the decoder class that failed.
export function rememberFallback(win, { resourceId, itemId, cls, strike = false }, now = Date.now()) {
    const m = loadMemory(win, now);
    const src = sourceKey(resourceId, itemId);
    if (src) m.sources[src] = now;
    if (src && strike && STRUCK_BY_CLASS[cls]) {
        m.strikes[cls] = [...(m.strikes[cls] || []), { src, at: now }];
    }
    saveMemory(win, m);
    return m;
}

// declaredTokens: what this page declares for a file with no failure of its
// own -- null where it declares nothing (not taking part, or no answer and
// no cache yet), else the tokens ([] for a browser that decodes none).
export function declaredTokens(win, now = Date.now()) {
    if (!takesPart(win)) return null;
    const toks = freshTokens(win) ?? cachedTokens(win, now);
    if (toks === null) return null;
    const struck = struckTokens(loadMemory(win, now));
    return toks.filter((t) => !struck.has(t));
}

// declarationFor is the `decode` value a start of this file sends, or null
// for none (the rules at the top).
export function declarationFor(win, { resourceId, itemId } = {}, now = Date.now()) {
    if (!takesPart(win)) return null;
    const m = loadMemory(win, now);
    const src = sourceKey(resourceId, itemId);
    if (src && m.sources[src]) return null;
    const toks = freshTokens(win) ?? cachedTokens(win, now);
    if (toks === null) return UNKNOWN;
    const struck = struckTokens(m);
    const out = toks.filter((t) => !struck.has(t));
    return out.length ? out.join(',') : null;
}

// ---- the forms -------------------------------------------------------------

// isStreamVideoForm: the start of a video stream -- the only start with a
// video decoder to declare.
export function isStreamVideoForm(form, win) {
    if (!form || form.tagName !== 'FORM') return false;
    try {
        return /\/stream-video$/.test(new URL(form.getAttribute('action') || '', win.location.href).pathname);
    } catch (e) {
        return false;
    }
}

const fieldValue = (form, name) => {
    const el = form.querySelector(`input[name="${name}"]`);
    return el ? el.value : '';
};

// applyDeclaration puts the declaration on the form, or takes a stale one
// off. Idempotent: it runs on every pass of a submit.
export function applyDeclaration(form, win) {
    if (!isStreamVideoForm(form, win)) return;
    const d = declarationFor(win, { resourceId: fieldValue(form, 'resource-id'), itemId: fieldValue(form, 'item-id') });
    let input = form.querySelector('input[name="decode"]');
    if (d === null) {
        if (input) input.remove();
        return;
    }
    if (!input) {
        input = form.ownerDocument.createElement('input');
        input.type = 'hidden';
        input.name = 'decode';
        form.appendChild(input);
    }
    input.value = d;
}

// installSubmitHook: a capture listener on the document that (re)writes the
// field on every submit of a stream start. It does not care where it stands
// among the other capture listeners: Turnstile stops the first pass and
// re-submits (turnstileAction.js), and whichever pass reaches the async
// submit has been through here. Once per page.
export function installSubmitHook(doc, win) {
    const s = state(win);
    if (s.hooked) return;
    s.hooked = true;
    doc.addEventListener('submit', (e) => {
        try {
            applyDeclaration(e.target, win);
        } catch (err) {
            // A declaration that cannot be made is none: the old route.
        }
    }, true);
}

// initDecodeDeclaration is what the layout runs on every page, before
// Turnstile and the async navigation: whatever goes wrong in here must not
// take those down with it.
export function initDecodeDeclaration(win, doc) {
    try {
        applyUrlSwitch(win);
        if (takesPart(win)) startProbe(win);
        installSubmitHook(doc, win);
    } catch (e) {
        try { console.error('decode declaration:', e); } catch (e2) { /* nothing to report to */ }
    }
}
