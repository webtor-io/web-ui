// HEVC passthrough in the player (docs/player.md, "Passthrough: errors and
// fallback"). A stream whose transcoder session hands the browser the
// source's HEVC as it is (data-video-route="passthrough") can fail where a
// re-encoded one would not: the browser declared a decoder that then does not
// cope. This module watches for that and gives the file up to the old route
// -- a visible restart, from the start (stage 3 spec, D8), which the
// declaration hook sends without `decode` for this file.
//
// Nothing here runs on any other route: the old route's error handling
// (hls-manager.js) is untouched.

import {
    rememberFallback, setPendingFallback, applyDeclaration, STRUCK_BY_CLASS,
} from './decode-declaration.js';

// Why a passthrough was given up -- the same closed set the server accepts
// (models.ParseFallbackReason).
export const REASONS = ['codecs_rejected', 'decode_error', 'media_error', 'src_unsupported', 'no_frames', 'user'];

// The failures that are the decoder's: they strike the class (two strikes
// on different files within 7 days take it out of the declaration,
// decode-declaration.js). Only where the element itself says its decoder
// failed (MediaError code 3) or no picture came while time ran. Not a
// codec string the browser refused (a build fault of ours, alerted on), not
// a media error hls.js raised without the element's word (a segment that did
// not parse is not the decoder), not a source the element refused (code 4:
// Safari says it for a failed master too -- not verified), not the viewer's
// own choice.
export const STRIKING = new Set(['decode_error', 'no_frames']);

// hls.js 1.6 error details (src/errors.ts).
const MANIFEST_INCOMPATIBLE_CODECS = 'manifestIncompatibleCodecsError';
const BUFFER_ADD_CODEC = 'bufferAddCodecError';
const MEDIA_ERROR = 'mediaError';

// MediaError codes (HTML).
const MEDIA_ERR_DECODE = 3;
const MEDIA_ERR_SRC_NOT_SUPPORTED = 4;

// One failure is often reported twice: the element's `error` and, at the
// next append, hls.js's bufferAppendError for the same thing. Reports this
// close together are one incident.
export const SAME_INCIDENT_MS = 1000;

// The watchdog: this long after the first `playing`, a stream whose time ran
// on by more than NO_FRAMES_MIN_ADVANCE_S with no picture has no decoder.
export const NO_FRAMES_AFTER_MS = 10000;
export const NO_FRAMES_MIN_ADVANCE_S = 2;

// passthroughHlsConfig is hls.js's config for a passthrough stream. The
// fragment policy is set explicitly: a 4K segment at the viewer's cap may
// take minutes, and hls.js's default gives it 120 s and then fetches it again
// from zero -- today's legacy settings mean up to 100 such refetches
// (config.ts turns fragLoading* into fragLoadPolicy only when none is set).
// maxLoadTimeMs comes from the job (data-frag-load-ms: twice the segment's
// time at the cap, 2-15 min); a timed-out segment is tried 3 times in all.
// errorRetry is exactly what the legacy settings make of it today, so an
// explicit policy does not quietly drop the 100 retries on HTTP errors.
export function passthroughHlsConfig(base, fragLoadMs) {
    const ms = Number(fragLoadMs) > 0 ? Number(fragLoadMs) : 120000;
    return {
        ...base,
        fragLoadPolicy: {
            default: {
                maxTimeToFirstByteMs: 10000,
                maxLoadTimeMs: ms,
                timeoutRetry: { maxNumRetry: 2, retryDelayMs: 0, maxRetryDelayMs: 0 },
                errorRetry: { maxNumRetry: 100, retryDelayMs: 1000, maxRetryDelayMs: 10000 },
            },
        },
    };
}

// framesCounted: some video on this page reported decoded frames, so the
// frame counter works in this browser (Android Chrome's native player
// reads 0 while it plays -- playback-quality, 2026-09). Until then a zero
// count proves nothing.
const FRAMES_KEY = '__wtFramesCounted';
export function framesCounted(win) {
    try { return !!win[FRAMES_KEY]; } catch (e) { return false; }
}
function markFramesCounted(win) {
    try { win[FRAMES_KEY] = true; } catch (e) { /* nothing to mark on */ }
}

function totalFrames(video) {
    try {
        const q = video.getVideoPlaybackQuality ? video.getVideoPlaybackQuality() : null;
        if (q && typeof q.totalVideoFrames === 'number') return q.totalVideoFrames;
    } catch (e) { /* no counter */ }
    if (typeof video.webkitDecodedFrameCount === 'number') return video.webkitDecodedFrameCount;
    return null;
}

// createPassthroughGuard watches one passthrough player and calls
// fallback(reason) at most once:
//   - hls.js refused the stream's codec string (manifestIncompatibleCodecs,
//     or a fatal bufferAddCodec: the init's codec is the one hls.js adds)
//     -> codecs_rejected;
//   - a media failure -- hls.js's fatal MEDIA_ERROR, or the element's own
//     decode error (MediaError 3) on the hls.js path, which hls.js itself
//     does not listen for and learns of only at its next append (with a
//     full buffer that can be half a minute, or never) -- is recovered once
//     (recoverMediaError); the next one gives the file up: decode_error when
//     the element said its decoder failed, media_error otherwise;
//   - native HLS (no hls.js): the element's MediaError 3 -> decode_error,
//     4 -> src_unsupported; the element is all there is;
//   - the watchdog (native and hls.js): NO_FRAMES_AFTER_MS after the first
//     `playing`, in a tab that stayed visible, time ran on by more than
//     NO_FRAMES_MIN_ADVANCE_S and there is no picture -- videoWidth 0, or no
//     decoded frame where this page has seen the counter work -> no_frames.
// Network errors are not its business: they go on as on every route.
//
// fallback gets (reason, path): 'mse' once setHls was given an hls.js
// instance, 'native' otherwise. Returns { onHlsError(hls, data) -> handled,
// setHls(hls), fire(reason), dispose() }.
export function createPassthroughGuard({ video, fallback, win = window, doc = document,
    now = () => Date.now(), setTimer = setTimeout, clearTimer = clearTimeout }) {
    let done = false;
    let recovered = 0;
    let lastAt = -Infinity;
    let sawDecode = false;
    let timer = null;
    let hlsRef = null;
    let startedAt = null;
    let hiddenSeen = false;

    const fire = (reason) => {
        if (done) return false;
        done = true;
        dispose();
        try { fallback(reason, hlsRef ? 'mse' : 'native'); } catch (e) { /* the page goes on */ }
        return true;
    };
    const elementDecodeError = () => {
        try { return !!(video.error && video.error.code === MEDIA_ERR_DECODE); } catch (e) { return false; }
    };
    const mediaFailure = (hls) => {
        if (done) return true;
        if (elementDecodeError()) sawDecode = true;
        const t = now();
        if (t - lastAt < SAME_INCIDENT_MS) return true;
        lastAt = t;
        if (hls && recovered === 0) {
            recovered = 1;
            try { hls.recoverMediaError(); } catch (e) { /* nothing to recover */ }
            return true;
        }
        fire(sawDecode ? 'decode_error' : 'media_error');
        return true;
    };

    const onHlsError = (hls, data) => {
        if (done || !data) return done;
        if (data.details === MANIFEST_INCOMPATIBLE_CODECS || (data.details === BUFFER_ADD_CODEC && data.fatal)) {
            fire('codecs_rejected');
            return true;
        }
        if (data.fatal && data.type === MEDIA_ERROR) return mediaFailure(hls);
        return false;
    };

    const onElementError = () => {
        let code = 0;
        try { code = video.error ? video.error.code : 0; } catch (e) { code = 0; }
        if (hlsRef) {
            // hls.js reports what it knows itself; the element's own word
            // matters for the decoder only.
            if (code === MEDIA_ERR_DECODE) mediaFailure(hlsRef);
            return;
        }
        if (code === MEDIA_ERR_DECODE) fire('decode_error');
        else if (code === MEDIA_ERR_SRC_NOT_SUPPORTED) fire('src_unsupported');
    };

    const onVisibility = () => { if (doc.hidden) hiddenSeen = true; };
    const check = () => {
        timer = null;
        if (done || hiddenSeen || doc.hidden) return;
        let advanced = 0;
        try { advanced = video.currentTime - startedAt; } catch (e) { return; }
        if (!(advanced > NO_FRAMES_MIN_ADVANCE_S)) return;
        const frames = totalFrames(video);
        if (frames > 0) {
            markFramesCounted(win);
            return;
        }
        if (video.videoWidth === 0 || (frames === 0 && framesCounted(win))) fire('no_frames');
    };
    const onPlaying = () => {
        video.removeEventListener('playing', onPlaying);
        hiddenSeen = hiddenSeen || !!doc.hidden;
        startedAt = video.currentTime;
        timer = setTimer(check, NO_FRAMES_AFTER_MS);
    };

    video.addEventListener('error', onElementError);
    video.addEventListener('playing', onPlaying);
    doc.addEventListener('visibilitychange', onVisibility);

    function dispose() {
        video.removeEventListener('error', onElementError);
        video.removeEventListener('playing', onPlaying);
        doc.removeEventListener('visibilitychange', onVisibility);
        if (timer !== null) { clearTimer(timer); timer = null; }
    }

    return {
        onHlsError,
        setHls(h) { hlsRef = h; },
        fire,
        dispose,
        get done() { return done; },
    };
}

const START_FORM = 'form[action$="/stream-video"]';

function fieldValue(form, name) {
    const el = form.querySelector(`input[name="${name}"]`);
    return el ? el.value : '';
}

// fallbackURL: this page pointed at the file, starting it at once, with why
// (app/resource/get.js reads the reason from the hash).
export function fallbackURL(href, path, reason, cls) {
    const u = new URL(href);
    if (path) {
        u.searchParams.set('file', path);
        u.searchParams.delete('file-idx');
    }
    const h = new URLSearchParams();
    h.set('action', 'stream');
    h.set('decode-fallback', reason);
    h.set('decode-class', cls);
    u.hash = h.toString();
    return u.pathname + u.search + u.hash;
}

// loadDocument goes to `url` by loading the page, which is what starts the
// deep link: app/resource/get.js reads the hash once, when the page loads.
// A URL that differs from the address only by its fragment is a fragment
// navigation -- the document stays, get.js does not run again, and the
// viewer is left on the failed player. That is the fallback's usual case,
// not an edge: the quiet move to the next file has already put
// `?file=<next>` in the address (next-item-go.js pushState), so the
// deep link for that file differs from it only by the hash. There the
// address is replaced (no second history entry for the same file) and the
// page reloaded. `loc` is a Location.
export function loadDocument(loc, url) {
    const target = new URL(url, loc.href);
    const bare = (u) => { const x = new URL(u); x.hash = ''; return x.href; };
    if (bare(target.href) === bare(loc.href)) {
        loc.replace(target.href);
        loc.reload();
        return 'reload';
    }
    loc.assign(target.href);
    return 'assign';
}

// restartEmbed starts the embed again the way it was started (app/embed/
// check.js initEmbed: a POST of its settings), with the fallback's fields and
// without a declaration -- not a reload: the embed's page is the answer to a
// POST, and a reload would send the same body again, `decode` included.
function restartEmbed(win, doc, reason, cls) {
    const form = doc.createElement('form');
    form.setAttribute('method', 'post');
    form.setAttribute('enctype', 'multipart/form-data');
    const add = (name, value) => {
        const i = doc.createElement('input');
        i.setAttribute('type', 'hidden');
        i.setAttribute('name', name);
        i.setAttribute('value', value);
        form.append(i);
    };
    add('_csrf', win._CSRF || '');
    add('_sessionID', win._sessionID || '');
    add('settings', JSON.stringify(win._embedSettings));
    add('decode-fallback', reason);
    add('decode-class', cls);
    doc.body.append(form);
    form.submit();
}

// fallbackToOldRoute gives this file up to the old route (the stage 3 spec's
// machine, §6):
//   1. the memory: this file, and a strike against its class where the
//      failure is the decoder's (STRIKING);
//   2. Umami hevc-fallback {reason, cls, path};
//   3. the restart, visibly, from the start:
//      - in an embed: its POST again (restartEmbed);
//      - on the resource page whose start form is this file's: the form,
//        with the restart note on the page (decode-declaration.js
//        setPendingFallback: the hook puts the fallback's fields on it and
//        no declaration) -- the same path as the button, Turnstile included;
//      - the start form is another file's (the player moved on to the next
//        episode quietly and the page is not brought up to date yet, e.g. in
//        fullscreen): this page for this file, started by its deep link with
//        the reason in the hash -- loaded, even where the address already
//        names the file (loadDocument);
//      - there is no start form: a reload.
// Returns which restart it took.
export function fallbackToOldRoute({ video, reason, path = 'mse', win = window, doc = document,
    track = (name, data) => { if (win.umami) win.umami.track(name, data); },
    navigate = (u) => loadDocument(win.location, u) }) {
    const d = video.dataset || {};
    const resourceId = d.resourceId || '';
    const itemId = d.itemId || '';
    const cls = STRUCK_BY_CLASS[d.videoClass] ? d.videoClass : 'unknown';
    try {
        rememberFallback(win, { resourceId, itemId, cls, strike: STRIKING.has(reason) });
    } catch (e) { /* no memory: the restart still goes without a declaration */ }
    try { track('hevc-fallback', { reason, cls, path }); } catch (e) { /* no telemetry */ }
    if (win._embedSettings) {
        restartEmbed(win, doc, reason, cls);
        return 'embed';
    }
    const form = doc.querySelector(START_FORM);
    if (form && fieldValue(form, 'resource-id') === resourceId && fieldValue(form, 'item-id') === itemId) {
        setPendingFallback(win, { resourceId, itemId, reason, cls });
        // The fields now, not only from the layout's submit hook: this must
        // not depend on the hook being installed on the page. The hook, where
        // it is, writes the same on each of Turnstile's passes.
        applyDeclaration(form, win);
        form.requestSubmit();
        return 'form';
    }
    if (form) {
        navigate(fallbackURL(win.location.href, d.path, reason, cls));
        return 'navigate';
    }
    win.location.reload();
    return 'reload';
}
