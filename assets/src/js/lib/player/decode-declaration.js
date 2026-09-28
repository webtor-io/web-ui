// The page's HEVC passthrough declaration (docs/player.md, "The
// declaration"): the `decode` field a stream start sends, which
// content-transcoder reads to decide whether this browser gets the source's
// HEVC as it is or the H.264 it has always had. The page only declares; the
// transcoder decides.
//
// Who declares. Every browser since stage 5 (2026-09-28) but one that
// opened any page with `?passthrough=off` (localStorage `wt-passthrough`);
// `?passthrough=on` takes it back. Before stage 5 only a browser that opted
// in did: the transcoder is one for production and stage, so "production
// does not declare yet" could not be a deployment -- it was this per-browser
// switch (stage 3 spec, D1). A page that does not take part sends no
// `decode` field and, but for Discover, runs no probe: it costs nothing. Discover asks every browser (decodedTokens): its
// HEVC and HDR switches follow what the browser decodes, whether or not it
// declares (lib/discover/playback.js).
//
// What it declares -- all or nothing (D4):
//   1. not taking part                     -> no field;
//   2. this file failed passthrough here   -> no field (the old route for it,
//                                             its audio included);
//   3. the probe's video part answered     -> its tokens minus the ones the
//                                             memory of failures took away,
//                                             then, for a browser opted into
//                                             audio, the audio part's -- minus
//                                             the struck audio classes' and,
//                                             for a file whose audio failed
//                                             here, that class's
//                                             (AUDIO_DROP_BY_CLASS);
//                                             none at all -> no field;
//   4. the video part has not answered     -> the cached answer of this same
//                                             browser (User-Agent, 30 days);
//                                             none -> `unknown`, alone.
// HEVC tokens are never sent without the `hdr-pq` answer: the transcoder
// would read the missing token as "does not decode HDR" and refuse a 4K HDR
// film with a false reason. A check that did not answer is not a browser
// that cannot decode.
//
// The audio tokens (aac51, ac3, ec3: multichannel audio) are a part of their
// own, answered and remembered apart from the video part and appended to it.
// They have an opt-in of their own, independent of the video's:
// `?audio=on|off` on any page (localStorage `wt-audio`), and until the
// owner's device matrix passes for audio only a browser that opened
// `?audio=on` declares them (takesPartAudio) -- a page that takes part in
// the video declaration and not in the audio one sends exactly the video
// part, asks the browser nothing about audio and waits for nothing of it.
// Their one slow question (`aac51` over decodingInfo) has no deadline either,
// and until it answers the part is this browser's cached audio answer, else
// nothing: a missing audio token is only the stereo the transcoder has
// always made, so it cannot refuse anything falsely -- unlike `unknown`,
// which is about the video and is never sent with audio tokens (the server
// would drop it, and audio tokens alone read as "no HEVC"). Neither part
// waits for the other, and the video part -- all Discover asks
// (decodedTokens) -- is exactly what it was.
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
// The audio part's own opt-in (`?audio=on|off`), independent of OPTIN_KEY.
export const AUDIO_OPTIN_KEY = 'wt-audio';
export const CACHE_KEY = 'wt-decode';
export const AUDIO_CACHE_KEY = 'wt-decode-audio';
export const MEMORY_KEY = 'wt-decode-fallback';
export const MEMORY_TTL_MS = 7 * 24 * 3600 * 1000;
export const CACHE_TTL_MS = 30 * 24 * 3600 * 1000;
// Failures on this many different files within MEMORY_TTL_MS take a class
// of decoder out of the declaration (plan §5.1): one failure is the file's.
export const STRIKES = 2;
const STATE = '__wtDecode';
const UNKNOWN = 'unknown';

// The tokens a declaration may carry, in the transcoder's order
// (codec-support.js DECODE_TOKENS; a test pins the two together): the video
// part, then the audio part. Not imported from there: that module is the
// probe, and this one is in the layout of every page.
export const VIDEO_TOKENS = ['hevc8', 'hevc10', 'hevc8-2160', 'hevc10-2160', 'hevc-high', 'hdr-pq'];
export const AUDIO_TOKENS = ['aac51', 'ac3', 'ec3'];
export const TOKENS = [...VIDEO_TOKENS, ...AUDIO_TOKENS];

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

// The audio classes (multichannel audio; docs/player.md, "Multichannel
// audio and the fallback"): what a declaration made of a session's audio,
// and what an audio failure is charged to --
//   dolby: AC-3 / E-AC-3 copied as it is (a passthrough's fMP4 only);
//   aac51: AAC with more than two channels, copied or encoded to.
// A failure of one takes out, for the file that failed, AUDIO_DROP_BY_CLASS
// -- Dolby for dolby (the audio comes as AAC then, 5.1 where aac51 is
// declared, and the video keeps its route), every audio token for aac51
// (the stereo the transcoder has always made) -- and, after STRIKES files,
// AUDIO_STRUCK_BY_CLASS from every declaration: the class's own tokens.
export const AUDIO_STRUCK_BY_CLASS = {
    'dolby': ['ac3', 'ec3'],
    'aac51': ['aac51'],
};
export const AUDIO_DROP_BY_CLASS = {
    'dolby': ['ac3', 'ec3'],
    'aac51': ['aac51', 'ac3', 'ec3'],
};
const own = (o, k) => typeof k === 'string' && Object.prototype.hasOwnProperty.call(o, k);
export const isAudioClass = (cls) => own(AUDIO_STRUCK_BY_CLASS, cls);
// The tokens a strike against cls takes out, null for no class.
const struckBy = (cls) => (own(STRUCK_BY_CLASS, cls) ? STRUCK_BY_CLASS[cls]
    : own(AUDIO_STRUCK_BY_CLASS, cls) ? AUDIO_STRUCK_BY_CLASS[cls] : null);

// declaresAudio: does this declaration (a `decode` value, data-decode)
// carry an audio token -- could the transcoder have made its audio other
// than stereo AAC?
export function declaresAudio(decl) {
    if (typeof decl !== 'string' || !decl) return false;
    return decl.split(',').some((t) => AUDIO_TOKENS.includes(t.trim()));
}

function state(win) {
    let s = win[STATE];
    if (!s) {
        s = { optin: undefined, audioOptin: undefined, probe: null, audio: null, askAudio: false, fresh: null, ready: null, readyResolve: null, memory: null, hooked: false, pending: null };
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

// urlSwitch reads `?<param>=on|off` from the address; null for anything
// else.
function urlSwitch(win, param) {
    let v = null;
    try {
        v = new URLSearchParams(win.location.search).get(param);
    } catch (e) {
        return null;
    }
    return v === 'on' || v === 'off' ? v : null;
}

// applyUrlSwitch reads `?passthrough=on|off` from the address and remembers
// it for this browser (and for this page, where there is no storage).
// Returns what it applied, or null.
export function applyUrlSwitch(win) {
    const v = urlSwitch(win, 'passthrough');
    if (v === null) return null;
    state(win).optin = v;
    write(win, OPTIN_KEY, v);
    return v;
}

// applyAudioUrlSwitch is the same for the audio part: `?audio=on|off`,
// remembered as AUDIO_OPTIN_KEY. Neither switch touches the other.
export function applyAudioUrlSwitch(win) {
    const v = urlSwitch(win, 'audio');
    if (v === null) return null;
    state(win).audioOptin = v;
    write(win, AUDIO_OPTIN_KEY, v);
    return v;
}

// takesPart: does this page send a declaration? Every browser but one
// that opted out with `?passthrough=off` (stage 5, 2026-09-28; before it,
// only a browser that opted in with `?passthrough=on`).
export function takesPart(win) {
    const s = state(win);
    const v = s.optin !== undefined ? s.optin : read(win, OPTIN_KEY);
    return v !== 'off';
}

// takesPartAudio: may this page's declaration carry the audio tokens? Only
// a browser that opted in with `?audio=on`, until the owner's device matrix
// passes for audio; its own stage 5 is then `=== 'on'` -> `!== 'off'`,
// separate from the video's. The tokens go out only where the page takes
// part in the declaration at all (declaresAudioPart).
export function takesPartAudio(win) {
    const s = state(win);
    const v = s.audioOptin !== undefined ? s.audioOptin : read(win, AUDIO_OPTIN_KEY);
    return v === 'on';
}

const declaresAudioPart = (win) => takesPart(win) && takesPartAudio(win);

const videoOf = (fresh) => [...fresh.hevc, ...(fresh.pq === 'yes' ? ['hdr-pq'] : [])];

// freshVideo: the video part of the probe's answer on this page, null until
// it is complete (the HEVC tokens are at once; `hdr-pq` waits for
// decodingInfo, with no deadline -- codec-support.js pqAnswer).
function freshVideo(win) {
    const f = state(win).fresh;
    return f && f.pq !== 'pending' ? videoOf(f) : null;
}

// freshAudio: the audio part, null until `aac51` has its answer (no
// deadline either -- codec-support.js aac51Answer).
function freshAudio(win) {
    const f = state(win).fresh;
    return f && Array.isArray(f.audio) ? f.audio : null;
}

// cachedTokens: this browser's answer for one part as a page remembered it
// (`key`: CACHE_KEY for the video part, AUDIO_CACHE_KEY for the audio one),
// only the part's own tokens; null for none, another browser's, or one
// older than CACHE_TTL_MS.
function cachedTokens(win, key, allowed, now) {
    try {
        const c = JSON.parse(read(win, key) || 'null');
        if (!c || c.ua !== userAgent(win) || !Array.isArray(c.tokens)) return null;
        if (typeof c.at !== 'number' || c.at > now || now - c.at > CACHE_TTL_MS) return null;
        return allowed.filter((t) => c.tokens.includes(t));
    } catch (e) {
        return null;
    }
}

// complete: the probe on this page has answered every part it asked --
// the video part, and the audio part only where it was asked (startProbe).
const complete = (win) => freshVideo(win) !== null && (!state(win).askAudio || freshAudio(win) !== null);

const videoTokens = (win, now) => freshVideo(win) ?? cachedTokens(win, CACHE_KEY, VIDEO_TOKENS, now);
const audioTokens = (win, now) => freshAudio(win) ?? cachedTokens(win, AUDIO_CACHE_KEY, AUDIO_TOKENS, now);

// startProbe asks the browser once per page, in the background: the
// probe module is loaded here, not bundled into the layout. Each part of
// the answer is kept on the page and, once complete, in its own cache for
// the next page's first seconds. The audio part is asked only where the
// page declares it (declaresAudioPart, at this moment): a browser that
// has not opted into audio is asked nothing about it here (the
// codec-support event measures it apart). Returns the promise of the video
// part -- what Discover waits for before it reads the answer again
// (usePlaybackContext): the audio part never holds it up.
export function startProbe(win, { load = () => import(/* webpackChunkName: "decode-probe" */ './codec-support.js'), env } = {}) {
    const s = state(win);
    if (s.probe) return s.probe;
    s.askAudio = declaresAudioPart(win);
    const answered = () => {
        if (complete(win)) s.readyResolve();
    };
    const support = (async () => {
        const cs = await load();
        const sup = cs.declarationSupport(env || cs.envFromWindow(win), { audio: s.askAudio });
        s.fresh = { hevc: sup.hevc, pq: sup.hevc.length ? 'pending' : 'no', audio: null };
        return sup;
    })();
    s.probe = support.then(async (sup) => {
        const f = s.fresh;
        if (f.pq === 'pending') f.pq = (await sup.pq) ? 'yes' : 'no';
        write(win, CACHE_KEY, JSON.stringify({ ua: userAgent(win), tokens: videoOf(f), at: Date.now() }));
        answered();
    }).catch(() => {
        // A probe that failed to load answers nothing: the cache, or
        // `unknown`, as for one that is still running.
    });
    if (!s.askAudio) return s.probe;
    s.audio = support.then(async (sup) => {
        // A probe without an audio part answers none. A rejection is an
        // answer too; only silence leaves it pending.
        const a = await Promise.resolve(sup.audio).then((v) => v, () => []);
        s.fresh.audio = AUDIO_TOKENS.filter((t) => Array.isArray(a) && a.includes(t));
        write(win, AUDIO_CACHE_KEY, JSON.stringify({ ua: userAgent(win), tokens: s.fresh.audio, at: Date.now() }));
        answered();
    }).catch(() => {
        // Nothing loaded: the audio cache, or no audio tokens.
    });
    return s.probe;
}

// whenDeclared resolves once the probe has answered completely -- the video
// part, and the audio part where it was asked (startProbe) -- or after
// `ms`, whichever is first; at once where no probe runs. A page that does
// not declare audio never waits for it: a deep link and an embed would
// otherwise hold their start up to `ms` for tokens they do not send.
export function whenDeclared(win, ms) {
    const s = state(win);
    if (!s.probe || complete(win)) return Promise.resolve();
    return Promise.race([s.ready, new Promise((r) => setTimeout(r, ms))]);
}

// ---- the memory of failures -------------------------------------------------

// The memory: `sources` -- the files that failed passthrough here (no
// declaration for them); `audio` -- the files whose audio failed here, by
// class ({src: {dolby: at}}: their declaration leaves that class's
// AUDIO_DROP_BY_CLASS out); `strikes` -- per class, video or audio, the
// files it failed on.
function emptyMemory() {
    return { sources: {}, strikes: {}, audio: {} };
}

const fresh = (at, now) => typeof at === 'number' && at <= now && now - at < MEMORY_TTL_MS;

function pruned(m, now) {
    const out = emptyMemory();
    for (const [src, at] of Object.entries((m && m.sources) || {})) {
        if (fresh(at, now)) out.sources[src] = at;
    }
    for (const [cls, list] of Object.entries((m && m.strikes) || {})) {
        if (!struckBy(cls) || !Array.isArray(list)) continue;
        const kept = list.filter((x) => x && typeof x.src === 'string' && fresh(x.at, now));
        if (kept.length) out.strikes[cls] = kept;
    }
    for (const [src, byCls] of Object.entries((m && m.audio) || {})) {
        if (!byCls || typeof byCls !== 'object') continue;
        const kept = {};
        for (const [cls, at] of Object.entries(byCls)) {
            if (isAudioClass(cls) && fresh(at, now)) kept[cls] = at;
        }
        if (Object.keys(kept).length) out.audio[src] = kept;
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
    for (const [src, byCls] of Object.entries(b.audio)) {
        const have = a.audio[src] || {};
        for (const [cls, at] of Object.entries(byCls)) {
            if (!have[cls] || have[cls] < at) have[cls] = at;
        }
        a.audio[src] = have;
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
            for (const t of struckBy(cls)) out.add(t);
        }
    }
    return out;
}

// rememberFallback records a stream that failed on this page and restarts:
// the file, and, when `strike`, a strike against the class that failed. For
// a video class (or `unknown`) the file's next start sends no declaration;
// for an audio class it leaves that class's AUDIO_DROP_BY_CLASS out.
export function rememberFallback(win, { resourceId, itemId, cls, strike = false }, now = Date.now()) {
    const m = loadMemory(win, now);
    const src = sourceKey(resourceId, itemId);
    if (src && isAudioClass(cls)) m.audio[src] = { ...(m.audio[src] || {}), [cls]: now };
    else if (src) m.sources[src] = now;
    if (src && strike && struckBy(cls)) {
        m.strikes[cls] = [...(m.strikes[cls] || []), { src, at: now }];
    }
    saveMemory(win, m);
    return m;
}

// decodedTokens: which video this browser decodes as far as this site
// knows -- the video part of the probe's answer on this page, else this
// browser's cached one, minus the classes the memory of failures took away;
// null until there is an answer ([] for a browser that decodes none).
// Whether the page takes part plays no role: Discover's switches ask it of
// every browser. The audio part is not in it, and neither its answer nor
// its silence changes it: Discover's questions are about HEVC and PQ.
export function decodedTokens(win, now = Date.now()) {
    const toks = videoTokens(win, now);
    if (toks === null) return null;
    const struck = struckTokens(loadMemory(win, now));
    return toks.filter((t) => !struck.has(t));
}

// declaredTokens: which video this page declares for a file with no failure
// of its own -- null where it declares nothing (not taking part, or no
// answer and no cache yet), else the video tokens ([] for a browser that
// decodes none, whatever audio it declares).
export function declaredTokens(win, now = Date.now()) {
    if (!takesPart(win)) return null;
    return decodedTokens(win, now);
}

// declarationFor is the `decode` value a start of this file sends, or null
// for none (the rules at the top): the video part, then the audio part --
// only where the browser opted into audio (takesPartAudio). A
// restart note (below) for a video class is a failed passthrough -- no
// declaration; for an audio class it takes that class's drop out, as the
// memory does once the file is remembered.
export function declarationFor(win, { resourceId, itemId } = {}, now = Date.now()) {
    if (!takesPart(win)) return null;
    const m = loadMemory(win, now);
    const src = sourceKey(resourceId, itemId);
    const pending = src ? pendingFor(win, src, now) : null;
    if (src && (m.sources[src] || (pending && !isAudioClass(pending.cls)))) return null;
    const video = videoTokens(win, now);
    if (video === null) return UNKNOWN;
    const leftOut = new Set(struckTokens(m));
    for (const cls of [...Object.keys((src && m.audio[src]) || {}), ...(pending ? [pending.cls] : [])]) {
        if (isAudioClass(cls)) for (const t of AUDIO_DROP_BY_CLASS[cls]) leftOut.add(t);
    }
    const audio = declaresAudioPart(win) ? (audioTokens(win, now) ?? []) : [];
    const kept = [...video, ...audio].filter((t) => !leftOut.has(t));
    return kept.length ? kept.join(',') : null;
}

// ---- the restart after a failed passthrough ----------------------------------

// A restart of the file whose passthrough just failed (Player.jsx, the
// player's fallback) carries why -- decode-fallback and decode-class: the
// server's counter and log, and the text of a 4K refusal
// (docs/user_errors.md) -- and declares nothing for that file, whatever the
// memory says: a browser without storage keeps no memory past this page,
// and the restart may be a new page (a deep link). A restart after the
// audio failed (an audio class) declares the rest: the declaration without
// that class's AUDIO_DROP_BY_CLASS, so the video keeps its route. The note
// lives on the page until the next player comes up (clearPendingFallback)
// or PENDING_TTL_MS passes: both of Turnstile's passes carry it, and no
// later start does -- a field left on the form would put `/fb=` in the key
// of a start that restarted nothing and count a fallback that did not
// happen.
export const PENDING_TTL_MS = 2 * 60 * 1000;

export function setPendingFallback(win, { resourceId, itemId, reason, cls }, now = Date.now()) {
    const src = sourceKey(resourceId, itemId);
    if (!src || !reason) return;
    state(win).pending = { src, reason: String(reason), cls: String(cls || 'unknown'), at: now };
}

export function clearPendingFallback(win) {
    state(win).pending = null;
}

function pendingFor(win, src, now) {
    const p = state(win).pending;
    if (!p || p.src !== src || p.at > now || now - p.at >= PENDING_TTL_MS) return null;
    return p;
}

// pendingFallbackFor: the restart note for this file, or null.
export function pendingFallbackFor(win, { resourceId, itemId } = {}, now = Date.now()) {
    return pendingFor(win, sourceKey(resourceId, itemId), now);
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

// setHidden puts a hidden field with value on the form, or takes it off
// for null.
function setHidden(form, name, value) {
    let input = form.querySelector(`input[name="${name}"]`);
    if (value === null) {
        if (input) input.remove();
        return;
    }
    if (!input) {
        input = form.ownerDocument.createElement('input');
        input.type = 'hidden';
        input.name = name;
        form.appendChild(input);
    }
    input.value = value;
}

// applyDeclaration puts the declaration on the form, or takes a stale one
// off, and the same for a restart's fallback fields (only the restart of
// the file that failed carries them). Idempotent: it runs on every pass of
// a submit.
export function applyDeclaration(form, win, now = Date.now()) {
    if (!isStreamVideoForm(form, win)) return;
    const file = { resourceId: fieldValue(form, 'resource-id'), itemId: fieldValue(form, 'item-id') };
    setHidden(form, 'decode', declarationFor(win, file, now));
    const p = pendingFallbackFor(win, file, now);
    setHidden(form, 'decode-fallback', p ? p.reason : null);
    setHidden(form, 'decode-class', p ? p.cls : null);
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
        applyAudioUrlSwitch(win);
        if (takesPart(win)) startProbe(win);
        installSubmitHook(doc, win);
    } catch (e) {
        try { console.error('decode declaration:', e); } catch (e2) { /* nothing to report to */ }
    }
}
