// HEVC passthrough in the player (docs/player.md, "Passthrough: errors and
// fallback"). A stream whose transcoder session hands the browser the
// source's HEVC as it is (data-video-route="passthrough") can fail where a
// re-encoded one would not: the browser declared a decoder that then does not
// cope. This module watches for that and gives the file up to the old route
// -- a visible restart, from the start (stage 3 spec, D8), which the
// declaration hook sends without `decode` for this file.
//
// Nothing here runs on any other route but one: a start that declared
// multichannel audio (createAudioGuard, below). The old route's error
// handling for every other stream (hls-manager.js) is untouched.

import {
    rememberFallback, setPendingFallback, applyDeclaration, declarationFor, declaresAudio, isAudioClass, STRUCK_BY_CLASS,
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
// A SourceBuffer's own `error` event (buffer-controller.ts onSBUpdateError):
// the append error algorithm, i.e. the data of that buffer could not be
// taken. A decoder failing later does not raise it, nor does an append
// thrown at once because the MediaSource has already ended in error (that
// one is bufferAppendError, named after whichever buffer appended next).
const BUFFER_APPENDING = 'bufferAppendingError';
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

// ---- multichannel audio: which side failed, and what the audio is -----------

// Codec names of AC-3 / E-AC-3: the fMP4 sample entries hls.js parses from
// an init (passthrough-remuxer.ts getParsedTrackCodec) and their RFC 6381
// `mp4a.a5` / `mp4a.a6` spellings a CODECS attribute may use.
const DOLBY_CODEC = /(^|[\s,])(ec-3|ac-3|mp4a\.a5|mp4a\.a6)(?=$|[\s,])/i;

// audioOfTrack is what the declaration made of the audio hls.js buffers, from
// the track of its BUFFER_CODECS event (types/buffer.ts): 'dolby' for AC-3 /
// E-AC-3; 'aac51' for more than two channels, and for 0 -- channel
// configuration 0 in the ADTS of MPEG-TS, the layout in a PCE (hls.js builds
// its AudioSpecificConfig from that header, adts.ts, and the browser cannot
// place the channels); null for one or two, what the transcoder has always
// made; 'aac' for a codec that is not Dolby with no channel count (an fMP4
// track: passthrough-remuxer.ts reports none) -- not Dolby, count unknown;
// undefined where there is no track to read.
export function audioOfTrack(track) {
    if (!track || typeof track !== 'object') return undefined;
    if (DOLBY_CODEC.test(String(track.codec || '')) || DOLBY_CODEC.test(String(track.levelCodec || ''))) return 'dolby';
    const n = track.metadata ? track.metadata.channelCount : undefined;
    if (typeof n !== 'number') return track.codec ? 'aac' : undefined;
    return n > 2 || n === 0 ? 'aac51' : null;
}

// The session's audio as the job read it from the master
// (data-audio-class: CHANNELS over 2, or an AC-3 / E-AC-3 CODECS), null
// without one.
function attrAudio(video) {
    let c = '';
    try { c = (video.dataset && video.dataset.audioClass) || ''; } catch (e) { c = ''; }
    return isAudioClass(c) ? c : null;
}

// messageSide: the side the element's MediaError message names, where it
// names only one ('audio' / 'video'), else null. The text is the browser's
// own and not a standard (not verified in a real browser which ones name
// the stream).
export function messageSide(video) {
    let msg = '';
    try { msg = String((video.error && video.error.message) || ''); } catch (e) { msg = ''; }
    const a = /audio/i.test(msg);
    const v = /video/i.test(msg);
    if (a === v) return null;
    return a ? 'audio' : 'video';
}

// bufferSide: the side of the SourceBuffer an hls.js error names, where the
// error is that buffer's own -- its append failed (bufferAppendingError), or
// the browser refused its codec (a fatal bufferAddCodecError) -- else null.
function bufferSide(data) {
    if (!data || !(data.details === BUFFER_APPENDING || (data.details === BUFFER_ADD_CODEC && data.fatal))) return null;
    return data.sourceBufferName === 'audio' || data.sourceBufferName === 'video' ? data.sourceBufferName : null;
}

// audioFallbackClass is the audio class a failure is charged to, or null for
// the rules the video has always had:
//   - no audio the declaration changed (audio null), no picture while time
//     ran (no_frames), the viewer's own choice (user) -> null;
//   - the audio's failure, as far as anyone says (fault 'audio') -> its
//     class;
//   - nobody says which side (fault null) and the audio is Dolby -> dolby:
//     Dolby as it is is the part of such a session no browser was handed
//     before, and giving it up keeps the video's route. Where the video was
//     at fault the restart fails again without Dolby, and that failure goes
//     the video's way;
//   - the video's failure, or an AAC 5.1 nobody blamed -> null.
export function audioFallbackClass({ reason, fault = null, audio = null }) {
    if (!isAudioClass(audio) || reason === 'no_frames' || reason === 'user') return null;
    if (fault === 'audio') return audio;
    if (fault === null && audio === 'dolby') return 'dolby';
    return null;
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

// A failure pinned on one side (bufferSide) counts for the incident that
// follows it within this long. The pin is an append error, after which the
// MediaSource has ended and the next report comes at the next append; where
// hls.js recovered by itself instead (onErrorOut, a level switch) no report
// of ours follows, and the pin must not wait for an unrelated failure.
export const FAULT_TTL_MS = 30000;

const liveError = (video) => {
    try { return !!(video.error && video.error.code); } catch (e) { return false; }
};

// createIncidents counts one player's media failures: the first is
// recovered (hls.recoverMediaError, after onRecover), the next gives up
// (giveUp). Two reports within SAME_INCIDENT_MS are one incident. After the
// recovery, though, the report in that window may be the new attachment
// failing at once -- and nothing may follow it: hls.js stops loading on a
// fatal error. The recovery reloads the element, which clears its error and
// drops its queued error events (HTML, the media element load algorithm;
// hls.js detachMedia calls load()), so an element that holds an error at
// the window's end is playing the attachment that failed: that is the next
// incident. Without an hls.js instance there is nothing to recover: the
// first report gives up.
function createIncidents({ video, now, setTimer, clearTimer, onRecover, giveUp }) {
    let recovered = 0;
    let lastAt = -Infinity;
    let recheck = null;
    return {
        failure(hls) {
            const t = now();
            if (t - lastAt < SAME_INCIDENT_MS) {
                if (recovered && recheck === null) {
                    recheck = setTimer(() => {
                        recheck = null;
                        if (liveError(video)) giveUp();
                    }, Math.max(0, lastAt + SAME_INCIDENT_MS - t));
                }
                return;
            }
            lastAt = t;
            if (hls && recovered === 0) {
                recovered = 1;
                onRecover();
                try { hls.recoverMediaError(); } catch (e) { /* nothing to recover */ }
                return;
            }
            giveUp();
        },
        dispose() {
            if (recheck !== null) { clearTimer(recheck); recheck = null; }
        },
    };
}

// createAudioSide keeps what a guard knows of the session's audio: what
// hls.js buffers (onBufferCodecs, audioOfTrack), else the master's word
// (data-audio-class) -- and where hls.js knows only that it is not Dolby
// ('aac'), AAC 5.1 if the master names any changed audio (a master with a
// Dolby rendition may have an AAC 5.1 one beside it), else the stereo of
// old; and the side the last failure was pinned on (bufferSide within
// FAULT_TTL_MS, else the element's message). A start that declared no
// audio token (data-decode; a browser not opted into audio) has no audio
// class whatever hls.js reports: its declaration changed nothing there,
// and its failures keep the video's rules.
function createAudioSide(video, now) {
    let trackAudio;
    let fault = null;
    let faultAt = -Infinity;
    return {
        onBufferCodecs(data) {
            const a = audioOfTrack(data && (data.audio || data.audiovideo));
            if (a !== undefined) trackAudio = a;
        },
        noteError(data) {
            const side = bufferSide(data);
            if (side && !(fault && now() - faultAt < FAULT_TTL_MS)) {
                fault = side;
                faultAt = now();
            }
        },
        forget() { fault = null; },
        audio: () => {
            let decl = '';
            try { decl = (video.dataset && video.dataset.decode) || ''; } catch (e) { decl = ''; }
            if (!declaresAudio(decl)) return null;
            const master = attrAudio(video);
            if (trackAudio === undefined) return master;
            if (trackAudio === 'aac') return master ? 'aac51' : null;
            return trackAudio;
        },
        fault: () => (fault && now() - faultAt < FAULT_TTL_MS ? fault : messageSide(video)),
    };
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
//     the element said its decoder failed, media_error otherwise
//     (createIncidents);
//   - native HLS (no hls.js): the element's MediaError 3 -> decode_error,
//     4 -> src_unsupported; the element is all there is;
//   - the watchdog (native and hls.js): NO_FRAMES_AFTER_MS after the first
//     `playing`, in a tab that stayed visible, time ran on by more than
//     NO_FRAMES_MIN_ADVANCE_S and there is no picture -- videoWidth 0, or no
//     decoded frame where this page has seen the counter work -> no_frames.
// Network errors are not its business: they go on as on every route.
//
// fallback gets (reason, path, audio): path 'mse' once setHls was given an
// hls.js instance, 'native' otherwise; audio the audio class the failure is
// charged to (audioFallbackClass: the audio hls.js buffers or the master
// names, and the side the failure was pinned on), null for the video's
// fallback. Returns { onHlsError(hls, data) -> handled,
// onBufferCodecs(data), setHls(hls), fire(reason), dispose() }.
export function createPassthroughGuard({ video, fallback, win = window, doc = document,
    now = () => Date.now(), setTimer = setTimeout, clearTimer = clearTimeout }) {
    let done = false;
    let sawDecode = false;
    let timer = null;
    let hlsRef = null;
    let startedAt = null;
    let hiddenSeen = false;
    const side = createAudioSide(video, now);

    const fire = (reason) => {
        if (done) return false;
        done = true;
        dispose();
        const audio = audioFallbackClass({ reason, fault: side.fault(), audio: side.audio() });
        try { fallback(reason, hlsRef ? 'mse' : 'native', audio); } catch (e) { /* the page goes on */ }
        return true;
    };
    const elementDecodeError = () => {
        try { return !!(video.error && video.error.code === MEDIA_ERR_DECODE); } catch (e) { return false; }
    };
    const incidents = createIncidents({
        video, now, setTimer, clearTimer,
        onRecover: () => side.forget(),
        giveUp: () => {
            if (elementDecodeError()) sawDecode = true;
            fire(sawDecode ? 'decode_error' : 'media_error');
        },
    });
    const mediaFailure = (hls) => {
        if (done) return true;
        if (elementDecodeError()) sawDecode = true;
        incidents.failure(hls);
        return true;
    };

    const onHlsError = (hls, data) => {
        if (done || !data) return done;
        side.noteError(data);
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
        incidents.dispose();
    }

    return {
        onHlsError,
        onBufferCodecs: side.onBufferCodecs,
        setHls(h) { hlsRef = h; },
        fire,
        dispose,
        get done() { return done; },
    };
}

// createAudioGuard watches a stream on any other route (old route: MPEG-TS,
// H.264 as it has always been) whose start declared audio tokens
// (Player.jsx, declaresAudio(data-decode)). Where the declaration changed
// the audio -- what hls.js buffers says so (more than two channels, a PCE,
// Dolby), else the master (data-audio-class) -- and nobody pins the failure
// on the video (bufferSide, messageSide):
//   - hls.js: a fatal media error is recovered once, and the next gives the
//     file up (createIncidents): decode_error where the element said
//     MediaError 3, else media_error. The measured case is AAC whose layout
//     is in a PCE: Chrome refuses the append (MediaError 4,
//     CHUNK_DEMUXER_ERROR_APPEND_FAILED, hls.js bufferAppendError), and a
//     recovery only fails again;
//   - native HLS: the element's MediaError 3 -> decode_error, 4 ->
//     src_unsupported, at once.
// Everything else goes on exactly as on the old route: onHlsError returns
// false and hls-manager.js recovers every fatal media error as it always
// has, and the element's errors are nobody's on the hls.js path. It never
// looks at the element's errors there, which hls.js learns of at its next
// append, as on every old-route stream.
//
// fallback gets (reason, path, audio) as the passthrough guard's does,
// audio never null. Returns { onHlsError, onBufferCodecs, setHls, dispose }.
export function createAudioGuard({ video, fallback, now = () => Date.now(),
    setTimer = setTimeout, clearTimer = clearTimeout }) {
    let done = false;
    let hlsRef = null;
    const side = createAudioSide(video, now);
    const ours = () => isAudioClass(side.audio()) && side.fault() !== 'video';
    const code = () => {
        try { return video.error ? video.error.code : 0; } catch (e) { return 0; }
    };

    const fire = (reason) => {
        if (done) return false;
        done = true;
        dispose();
        try { fallback(reason, hlsRef ? 'mse' : 'native', side.audio()); } catch (e) { /* the page goes on */ }
        return true;
    };
    const incidents = createIncidents({
        video, now, setTimer, clearTimer,
        onRecover: () => side.forget(),
        giveUp: () => fire(code() === MEDIA_ERR_DECODE ? 'decode_error' : 'media_error'),
    });

    const onHlsError = (hls, data) => {
        if (done || !data) return done;
        side.noteError(data);
        if (!(data.fatal && data.type === MEDIA_ERROR) || !ours()) return false;
        incidents.failure(hls);
        return true;
    };

    const onElementError = () => {
        if (hlsRef || done || !ours()) return;
        const c = code();
        if (c === MEDIA_ERR_DECODE) fire('decode_error');
        else if (c === MEDIA_ERR_SRC_NOT_SUPPORTED) fire('src_unsupported');
    };
    video.addEventListener('error', onElementError);

    function dispose() {
        video.removeEventListener('error', onElementError);
        incidents.dispose();
    }

    return {
        onHlsError,
        onBufferCodecs: side.onBufferCodecs,
        setHls(h) { hlsRef = h; },
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
// `decode` is the declaration an audio fallback keeps (null: none).
function restartEmbed(win, doc, reason, cls, decode = null) {
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
    if (decode) add('decode', decode);
    add('decode-fallback', reason);
    add('decode-class', cls);
    doc.body.append(form);
    form.submit();
}

// restartFile starts the file again, visibly, from the start, with why --
// the restart the video's fallback and the audio's share (below): the
// embed's POST, the page's start form, the deep link, or a reload. What it
// declares is the declaration hook's (decode-declaration.js): nothing for a
// video class, the declaration without the class's drop for an audio one.
function restartFile({ win, doc, d, resourceId, itemId, reason, cls, navigate }) {
    if (win._embedSettings) {
        let decode = null;
        if (isAudioClass(cls)) {
            try { decode = declarationFor(win, { resourceId, itemId }); } catch (e) { decode = null; }
        }
        restartEmbed(win, doc, reason, cls, decode);
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
    return restartFile({ win, doc, d, resourceId, itemId, reason, cls, navigate });
}

// The audio failures that strike their class: the decoder's (MediaError 3)
// and a media error -- on the audio side that is the append the browser
// refused (AAC whose layout is in a PCE), the declared capability failing
// on what the transcoder made of it. Wider than the video's STRIKING
// because a struck audio class costs little -- stereo, or AAC instead of
// Dolby, for 7 days -- where a struck video class refuses 4K. Not a codec
// string refused (a build fault, alerted on), not a source refused.
export const AUDIO_STRIKING = new Set(['decode_error', 'media_error']);

// fallbackAudio restarts this file without what the declaration made of its
// audio (docs/player.md, "Multichannel audio and the fallback"): `cls` is
// the audio class the failure is charged to -- dolby restarts it without
// ac3/ec3 (a passthrough stays one: the audio comes as AAC), aac51 without
// any audio token (the stereo the transcoder has always made).
//   1. the memory: this file's audio class, and a strike against it where
//      AUDIO_STRIKING;
//   2. Umami audio-fallback {reason, cls, path, route} -- not hevc-fallback,
//      whose count stays the video's;
//   3. the restart, as fallbackToOldRoute's; the server counts it by its
//      class (webui_passthrough_fallback_total{class="dolby"|"aac51"}).
// Returns which restart it took.
export function fallbackAudio({ video, reason, cls, path = 'mse', win = window, doc = document,
    track = (name, data) => { if (win.umami) win.umami.track(name, data); },
    navigate = (u) => loadDocument(win.location, u) }) {
    if (!isAudioClass(cls)) return fallbackToOldRoute({ video, reason, path, win, doc, track, navigate });
    const d = video.dataset || {};
    const resourceId = d.resourceId || '';
    const itemId = d.itemId || '';
    try {
        rememberFallback(win, { resourceId, itemId, cls, strike: AUDIO_STRIKING.has(reason) });
    } catch (e) { /* no memory: the restart's note still leaves the class out */ }
    try { track('audio-fallback', { reason, cls, path, route: d.videoRoute || '' }); } catch (e) { /* no telemetry */ }
    return restartFile({ win, doc, d, resourceId, itemId, reason, cls, navigate });
}
