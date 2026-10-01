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
    VOD_FALLBACK_CLASS, VOD_FALLBACK_REASON, markVodRescue,
} from './decode-declaration.js';

// Why a passthrough was given up -- the same closed set the server accepts
// (models.ParseFallbackReason).
export const REASONS = ['codecs_rejected', 'decode_error', 'media_error', 'src_unsupported', 'no_frames', 'user', 'fragment_loop'];

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
// The error of an append operation (buffer-controller.ts onError): fatal
// once the element holds an error or after appendErrorMaxRetry, else left to
// hls.js's error controller.
const BUFFER_APPEND = 'bufferAppendError';
const MEDIA_ERROR = 'mediaError';

// recoveredByHls: hls.js has already called recoverMediaError for this
// non-fatal bufferAppendError. hls.js 1.6.14 does that in one place,
// error-controller.ts onErrorOut, which runs before any listener of ours
// (hls.ts registers it in the constructor): for an append error whose
// action is SendAlternateToPenaltyBox and resolved, and whose message says
// `MediaSource readyState: ended`. Only the message is read: that message
// is made by buffer-controller.ts onSBUpdateError alone, whose
// bufferAppendError always gets that action (getLevelSwitchAction), and one
// that reaches us non-fatal is resolved (else onErrorOut makes it fatal).
// A resolved one saying `open` -- the SourceBuffer's error event before the
// MediaSource ended: Chrome 154 on the old route's master, the audio a
// rendition of its own -- is recovered by nobody. Content steering resolves
// every non-fatal bufferAppendError (content-steering-controller.ts
// onError), a level switch the rest; neither touches the MediaSource.
function recoveredByHls(data) {
    let msg = '';
    try { msg = String((data.error && data.error.message) || ''); } catch (e) { msg = ''; }
    return /MediaSource readyState: ended/.test(msg);
}

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

const declOf = (video) => {
    try { return (video.dataset && video.dataset.decode) || ''; } catch (e) { return ''; }
};

// startAudioClass is the audio class of this start, for the events
// (stream-start, hevc-fallback, audio-fallback): what its declaration made
// of the session's audio as the job read it from the master
// (data-audio-class), where the start declared an audio token (data-decode)
// -- 'dolby', 'aac51', else 'none' (the stereo AAC of old). Not what a
// failure is charged to: that is the guards' (audioFallbackClass).
export function startAudioClass(video) {
    if (!video || !declaresAudio(declOf(video))) return 'none';
    return attrAudio(video) || 'none';
}

// An audio output that failed, not a decoder: Chromium's MediaError message
// is "<PipelineStatus>: <the first error its media log saw>"
// (content/renderer/media/batching_media_log.cc GetErrorMessageLocked), and
// an output device that goes away under a playing stream -- Bluetooth
// headphones disconnecting, a USB DAC unplugged: the audio sink's render
// error -- is AUDIO_RENDERER_ERROR (media/renderers/audio_renderer_impl.cc
// OnRenderError, which logs "audio render error"), a MediaError 3 like a
// decoder's (web_media_player_impl.cc PipelineErrorToNetworkState). It names
// the audio, but not the audio the declaration changed. Chromium main,
// read 2026-09-29; Chrome 154 puts the group before the code
// ("PipelineStatus::CHUNK_DEMUXER_ERROR_APPEND_FAILED: ...", seen), so the
// code is looked for anywhere in the text. Safari names no side in any
// MediaError (WebKit's players give no errorMessage: "Media failed to
// decode"), and Firefox raises its audio sink's error only for media
// without video (MediaDecoderStateMachine::OnMediaSinkAudioError).
const AUDIO_OUTPUT_ERROR = /AUDIO_RENDERER_ERROR/;

// messageSide: the side the element's MediaError message names, where it
// names only one ('audio' / 'video'), else null -- and null for the audio's
// output failing (AUDIO_OUTPUT_ERROR), which is no decoder's. The text is
// the browser's own and not a standard: Chromium's decoder failures name
// their stream ("audio decode error!", "Failed to send audio packet for
// decoding", "audio decoder initialization failed"), Safari's never do.
export function messageSide(video) {
    let msg = '';
    try { msg = String((video.error && video.error.message) || ''); } catch (e) { msg = ''; }
    if (AUDIO_OUTPUT_ERROR.test(msg)) return null;
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
//     ran (no_frames), the viewer's own choice (user), a fragment loaded
//     again and again (fragment_loop: a video fragment the browser dropped,
//     fragment-loop.js) -> null;
//   - the audio's failure, as far as there is evidence (fault 'audio': the
//     audio SourceBuffer's own append failed or its codec was refused, or
//     the element's MediaError message names the audio's decoder) -> its
//     class;
//   - the video's failure (fault 'video') -> null;
//   - one nobody pinned on a side, in a session whose buffered audio is
//     Dolby -> dolby: the restart goes without ac3/ec3 and keeps the HEVC
//     route. The two wrong answers do not cost the same: a false `dolby`
//     strike costs this browser Dolby for 7 days (the audio comes as AAC 5.1
//     instead), a false HEVC strike costs it passthrough and 4K for 7 days.
//     WebKit names no side in any MediaError (17 of the 28 hevc-fallback
//     events in the 22 h to 2026-09-29 19:30Z were WebKit's native
//     decode_error), so the ambiguous case is the common one there, and the
//     cheap wrong answer goes first: if the video was the one failing, the
//     start without Dolby fails again and the video's rules take it from
//     there (one extra restart); a Dolby decoder that really fails is struck
//     after two files like any class, and Dolby goes, not HEVC. (From 29 to
//     30 September the branch charged the ambiguous case to the video; the
//     review showed that a Dolby failure systematic on some WebKit device
//     would then have taken its HEVC classes out one by one.)
//   - one nobody pinned, with AAC 5.1 (or a Dolby master whose buffered
//     track is AAC) -> null: a false aac51 strike takes every multichannel
//     token, dearer than a restart, and nothing marks AAC 5.1 as the likely
//     failure.
export function audioFallbackClass({ reason, fault = null, audio = null }) {
    if (!isAudioClass(audio) || reason === 'no_frames' || reason === 'user' || reason === 'fragment_loop') return null;
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
// hls.js resolved it by itself instead (onErrorOut's recovery, content
// steering, a level switch) no fatal report follows, and the pin must not
// wait for an unrelated failure.
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
// first report gives up. `held()`, asked when a later incident comes, says
// the recovery worked (createAudioGuard: clean playback since it, or too
// long ago to be the same fault): that incident is a first one again. The
// passthrough guard passes none -- its rule is unchanged.
// `recovered: true` reports an incident hls.js has already recovered from by
// itself (createAudioGuard, below): it counts as the one recovery, and no
// second recoverMediaError is made for it. `sameMs` is the window of "one
// incident told twice" -- 0 for a guard whose reports are never the same
// failure twice (createAudioGuard: each is a fresh pin on the audio buffer).
function createIncidents({ video, now, setTimer, clearTimer, onRecover, giveUp, held = () => false, sameMs = SAME_INCIDENT_MS }) {
    let recovered = 0;
    let lastAt = -Infinity;
    let recheck = null;
    return {
        failure(hls, { recovered: byHls = false } = {}) {
            const t = now();
            if (t - lastAt < sameMs) {
                if (recovered && recheck === null) {
                    recheck = setTimer(() => {
                        recheck = null;
                        if (liveError(video)) giveUp();
                    }, Math.max(0, lastAt + sameMs - t));
                }
                return;
            }
            lastAt = t;
            if (recovered && held()) recovered = 0;
            if ((hls || byHls) && recovered === 0) {
                recovered = 1;
                onRecover();
                if (!byHls) {
                    try { hls.recoverMediaError(); } catch (e) { /* nothing to recover */ }
                }
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
// FAULT_TTL_MS, else the element's message) and what said so (`by`, for
// the audio-fallback event: 'buffer' -- that SourceBuffer's own append
// failed, 'codec' -- its codec was refused, 'message' -- the element's
// MediaError message). A start that declared no
// audio token (data-decode; a browser opted out with ?audio=off) has no audio
// class whatever hls.js reports: its declaration changed nothing there,
// and its failures keep the video's rules.
function createAudioSide(video, now) {
    let trackAudio;
    let fault = null;
    let faultBy = null;
    let faultAt = -Infinity;
    const pinned = () => fault && now() - faultAt < FAULT_TTL_MS;
    return {
        onBufferCodecs(data) {
            const a = audioOfTrack(data && (data.audio || data.audiovideo));
            if (a !== undefined) trackAudio = a;
        },
        noteError(data) {
            const side = bufferSide(data);
            if (side && !pinned()) {
                fault = side;
                faultBy = data.details === BUFFER_ADD_CODEC ? 'codec' : 'buffer';
                faultAt = now();
            }
        },
        forget() { fault = null; },
        audio: () => {
            if (!declaresAudio(declOf(video))) return null;
            const master = attrAudio(video);
            if (trackAudio === undefined) return master;
            if (trackAudio === 'aac') return master ? 'aac51' : null;
            return trackAudio;
        },
        fault: () => (pinned() ? fault : messageSide(video)),
        by: () => (pinned() ? faultBy : messageSide(video) ? 'message' : null),
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
// fallback gets (reason, path, audio, by): path 'mse' once setHls was given an
// hls.js instance, 'native' otherwise; audio the audio class the failure is
// charged to (audioFallbackClass: the audio hls.js buffers or the master
// names, and the side the failure was pinned on), null for the video's
// fallback; by what blamed the audio (createAudioSide; 'unpinned' for Dolby
// charged first with nothing pinned), null with audio null. Returns { onHlsError(hls, data) -> handled,
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
        try { fallback(reason, hlsRef ? 'mse' : 'native', audio, audio ? (side.by() || 'unpinned') : null); } catch (e) { /* the page goes on */ }
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

// A stream that has played this much media since a recovery -- or, on
// native HLS, since it started -- has shown that the audio the declaration
// made decodes here: a failure after that is not charged to it
// (createAudioGuard).
export const CLEAN_PLAY_S = 30;
// A failure this long after the recovery is not the same fault told again,
// however little played in between (a viewer who paused): it is a first
// one. The measured re-failure comes within seconds -- the recovery fetches
// the same segment again.
export const RELATED_MS = 5 * 60 * 1000;

// A timeupdate step longer than this is a jump -- a seek, a gap hls.js
// skipped, the reload of a recovery (the element goes to 0 and hls.js puts
// it back) -- not playback; a step back is none either. codec-support.js
// counts the minute of playback-quality by the same rule.
const MAX_STEP_S = 2;

// playedClock counts the seconds of media the element plays from now on,
// for as long as it is not disposed of: one listener doing arithmetic on
// `timeupdate`, nothing else touched.
function playedClock(video) {
    let played = 0;
    let last = null;
    const onTime = () => {
        let t;
        try { t = video.currentTime; } catch (e) { return; }
        if (typeof t !== 'number' || !Number.isFinite(t)) return;
        if (last !== null) {
            const step = t - last;
            if (step > 0 && step <= MAX_STEP_S) played += step;
        }
        last = t;
    };
    video.addEventListener('timeupdate', onTime);
    return {
        played: () => played,
        dispose: () => video.removeEventListener('timeupdate', onTime),
    };
}

// createAudioGuard watches a stream on any other route (old route: MPEG-TS,
// H.264 as it has always been) whose start declared audio tokens
// (Player.jsx, declaresAudio(data-decode)) and restarts it without them only
// for a failure that is the audio's -- never for one that is not, and never
// for two failures with the audio shown working in between. It acts where
// the declaration changed the audio (what hls.js buffers says so: more than
// two channels, a PCE, Dolby; else the master, data-audio-class):
//   - hls.js: only a media error pinned on the audio (fault 'audio': the
//     audio SourceBuffer's own append failed -- hls.js bufferAppendingError,
//     or a fatal bufferAddCodecError on it -- within FAULT_TTL_MS, or the
//     element's MediaError message names the audio alone). A fatal one is
//     recovered here. A non-fatal bufferAppendError after the pin is
//     hls.js's own recovery only where its error controller made one
//     (recoveredByHls: `MediaSource readyState: ended`; onErrorOut runs
//     before this listener) -- that counts as the recovery, and none is
//     made on top of it. Any other non-fatal one (`readyState: open`:
//     resolved by content steering or a level switch, recovered by nobody,
//     the element left in MediaError 4 with nothing after it) is recovered
//     here, as a fatal one. The next incident gives the file up
//     (createIncidents): decode_error where the element said MediaError 3,
//     else media_error -- unless, since the recovery, CLEAN_PLAY_S of media
//     played or RELATED_MS passed: then it is a first one again. Giving up
//     also stops hls.js (stopLoad, detachMedia): its own recovery would
//     otherwise go on until the page is replaced. The measured case is AAC
//     whose layout is in a PCE (ADTS channel configuration 0): Chrome
//     refuses the audio append (bufferAppendingError on the audio buffer,
//     then a bufferAppendError; MediaError 4 CHUNK_DEMUXER_ERROR_APPEND_FAILED).
//     Reproduced in Chrome 154 with hls.js 1.6.14 (2026-09-29) on two shapes:
//     the old route's master (EXT-X-MEDIA audio rendition beside the video
//     level, as content-transcoder writes it) -- the first failure non-fatal
//     with `readyState: open`, nobody recovering, a dead player; and a
//     muxed media playlist -- each failure after a recovery non-fatal with
//     `readyState: ended`, hls.js recovering ~1 000 times a second.
//     A media error nobody pinned -- a video
//     decoder, a segment that did not parse, a bufferAppendError named after
//     whichever buffer appended next once the MediaSource had ended -- is
//     hls-manager.js's, which recovers it as on every old-route stream;
//   - native HLS (iOS with ?mms=off, or without a ManagedMediaSource; since
//     2026-09-30 an iPhone plays through hls.js otherwise): nothing names a
//     side (Safari's MediaError messages are empty, and there is no
//     SourceBuffer), and nothing recovers: an
//     element in error is dead, and without a guard it stays so. So only
//     while the stream has not played CLEAN_PLAY_S of media: MediaError 3 ->
//     decode_error, 4 -> src_unsupported. There the restart from the start
//     takes nothing from the viewer, and the audio is what the declaration
//     changed at the start of an H.264 stream; after it the audio has
//     decoded here, and an error is the old route's, as on every stream
//     (no restart). A message that names the video alone still keeps it the
//     video's.
// Everything else goes on exactly as on the old route: onHlsError returns
// false and hls-manager.js recovers every fatal media error as it always
// has, and the element's errors are nobody's on the hls.js path (hls.js
// learns of them at its next append, as on every old-route stream).
//
// fallback gets (reason, path, audio, by, place); place is captured before detach,
// including a first recovery that already detached the failed attachment.
// audio never null, by 'native' for the native rule. Returns { onHlsError,
// onBufferCodecs, setHls, dispose }.
export function createAudioGuard({ video, fallback, now = () => Date.now(),
    setTimer = setTimeout, clearTimer = clearTimeout }) {
    let done = false;
    let hlsRef = null;
    const side = createAudioSide(video, now);
    const changed = () => isAudioClass(side.audio());
    const code = () => {
        try { return video.error ? video.error.code : 0; } catch (e) { return 0; }
    };
    const clock = playedClock(video);
    let recoveredAt = -Infinity;
    let playedAtRecovery = 0;
    let recoveredPlace = null;

    const fire = (reason) => {
        if (done) return false;
        done = true;
        dispose();
        // recoverMediaError may itself have detached the failed attachment.
        // Preserve its last real place when the immediate re-failure reads 0.
        const rawPlace = { at: video.currentTime || 0, play: !video.paused };
        const place = rawPlace.at === 0 && recoveredPlace?.at > 0 ? recoveredPlace : rawPlace;
        if (hlsRef) {
            // Nothing more to load or recover for this file: without this,
            // hls.js's own recovery of an ended MediaSource re-attaches and
            // fails again until the restart replaces the player.
            try { hlsRef.stopLoad(); } catch (e) { /* stopped */ }
            try { hlsRef.detachMedia(); } catch (e) { /* detached */ }
        }
        // by: what blamed the audio; on native HLS nothing does -- the rule
        // before CLEAN_PLAY_S of playback.
        try { fallback(reason, hlsRef ? 'mse' : 'native', side.audio(), side.by() || 'native', place); } catch (e) { /* the page goes on */ }
        return true;
    };
    const incidents = createIncidents({
        video, now, setTimer, clearTimer,
        onRecover: () => {
            recoveredPlace = { at: video.currentTime || 0, play: !video.paused };
            side.forget();
            recoveredAt = now();
            playedAtRecovery = clock.played();
        },
        held: () => clock.played() - playedAtRecovery >= CLEAN_PLAY_S || now() - recoveredAt >= RELATED_MS,
        giveUp: () => fire(code() === MEDIA_ERR_DECODE ? 'decode_error' : 'media_error'),
        // Each report here is a failure of its own: one audio append failure
        // makes one bufferAppendError (the pin before it is not a report),
        // and the recovery forgets the pin, so a pinned report after it is
        // the new attachment failing -- at once, where hls.js recovers by
        // itself in a few milliseconds.
        sameMs: 0,
    });

    const onHlsError = (hls, data) => {
        if (done || !data) return done;
        side.noteError(data);
        if (data.type !== MEDIA_ERROR || !changed() || side.fault() !== 'audio') return false;
        if (data.fatal) {
            incidents.failure(hls);
            return true;
        }
        if (data.details !== BUFFER_APPEND) return false;
        // hls.js has recovered it by itself: counted as the recovery, and
        // left to hls-manager.js, which only warns of a non-fatal error.
        if (recoveredByHls(data)) {
            incidents.failure(hls, { recovered: true });
            return false;
        }
        // hls.js resolved it without a recovery (content steering, a level
        // switch): the element holds its error and nothing else follows --
        // the guard's to recover, or the player stays dead.
        incidents.failure(hls);
        return true;
    };

    const onElementError = () => {
        if (hlsRef || done || !changed() || side.fault() === 'video') return;
        if (clock.played() >= CLEAN_PLAY_S) return;
        const c = code();
        if (c === MEDIA_ERR_DECODE) fire('decode_error');
        else if (c === MEDIA_ERR_SRC_NOT_SUPPORTED) fire('src_unsupported');
    };
    video.addEventListener('error', onElementError);

    function dispose() {
        video.removeEventListener('error', onElementError);
        clock.dispose();
        incidents.dispose();
    }

    return {
        onHlsError,
        onBufferCodecs: side.onBufferCodecs,
        setHls(h) { hlsRef = h; },
        dispose,
        clearRestartPlace() { recoveredPlace = null; },
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

// postEmbedStart starts the embed again the way it was started (app/embed/
// check.js initEmbed: a POST of its settings) with `fields` ([name, value]
// pairs, a null value left out) -- not a reload: the embed's page is the
// answer to a POST, and a reload would send the same body again, `decode`
// included. Also the stream restart's (stream-restart.js).
export function postEmbedStart(win, doc, fields = []) {
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
    for (const [name, value] of fields) {
        if (value !== null && value !== undefined && value !== '') add(name, value);
    }
    doc.body.append(form);
    form.submit();
}

// restartEmbed: the embed's POST again with the fallback's fields and
// without a declaration. `decode` is the declaration an audio fallback keeps
// (null: none).
function restartEmbed(win, doc, reason, cls, decode = null) {
    postEmbedStart(win, doc, [['decode', decode], ['decode-fallback', reason], ['decode-class', cls]]);
}

// restartFile starts the file again, visibly, from the start, with why --
// the restart the video's fallback and the audio's share (below): the
// embed's POST, the page's start form, the deep link, or a reload. What it
// declares is the declaration hook's (decode-declaration.js): nothing for a
// video class, the declaration without the class's drop for an audio one.
function restartFile({ win, doc, d, resourceId, itemId, reason, cls, navigate, restart }) {
    if (restart) { restart({ reason, cls }); return 'background'; }
    if (win._embedSettings) {
        let decode = null;
        if (isAudioClass(cls) || cls === VOD_FALLBACK_CLASS) {
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

// Visible transport after an off-page start requires interaction or cannot mount.
// The policy has already recorded the codec failure; do not strike/count twice.
export function restartFallback({ video, reason, cls, win = window, doc = document }) {
    const d = video.dataset || {};
    return restartFile({ win, doc, d, resourceId: d.resourceId || '', itemId: d.itemId || '', reason, cls,
        navigate: (url) => loadDocument(win.location, url) });
}

// fallbackToOldRoute gives this file up to the old route (the stage 3 spec's
// machine, §6):
//   1. the memory: this file, and a strike against its class where the
//      failure is the decoder's (STRIKING);
//   2. Umami hevc-fallback {reason, cls, path, audio} -- audio the start's
//      audio class (startAudioClass): a Dolby decoder failing with nothing
//      to blame it is counted here, per class;
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
    navigate = (u) => loadDocument(win.location, u), restart }) {
    const d = video.dataset || {};
    const resourceId = d.resourceId || '';
    const itemId = d.itemId || '';
    const cls = STRUCK_BY_CLASS[d.videoClass] ? d.videoClass : 'unknown';
    try {
        rememberFallback(win, { resourceId, itemId, cls, strike: STRIKING.has(reason) });
    } catch (e) { /* no memory: the restart still goes without a declaration */ }
    try { track('hevc-fallback', { reason, cls, path, audio: startAudioClass(video) }); } catch (e) { /* no telemetry */ }
    return restartFile({ win, doc, d, resourceId, itemId, reason, cls, navigate, restart });
}

// VOD_REASON: why a stream nginx-vod served was given up (vod-guard.js) --
// its own reason, not codecs_rejected, whose count alerts as a build fault of
// the passthrough (models.ParseFallbackReason accepts it).
export const VOD_REASON = VOD_FALLBACK_REASON;

// fallbackToTranscoder restarts a file whose nginx-vod stream this browser
// refused (vod-guard.js), visibly, from the start, as the passthrough's
// fallback does (restartFile) -- with decode-fallback=vod_codecs, which the
// server sends to the transcoder (jobs/scripts/vod_route.go), and the whole
// declaration (class VOD_FALLBACK_CLASS): the refusal may be the audio's,
// and a video the browser decodes keeps its passthrough. No memory: the
// server's rules send the next start of the same file to the transcoder
// where they can tell, and nothing is struck. Umami vod-fallback {reason,
// mime} -- not hevc-fallback, whose count is the passthrough's.
export function fallbackToTranscoder({ video, mime = '', win = window, doc = document,
    track = (name, data) => { if (win.umami) win.umami.track(name, data); },
    navigate = (u) => loadDocument(win.location, u), restart }) {
    const d = video.dataset || {};
    const file = { resourceId: d.resourceId || '', itemId: d.itemId || '' };
    try { track('vod-fallback', { reason: VOD_REASON, mime }); } catch (e) { /* no telemetry */ }
    // Every later start of this file on the page carries vod_codecs too
    // (decode-declaration.js markVodRescue).
    try { markVodRescue(win, file); } catch (e) { /* the restart still goes */ }
    return restartFile({ win, doc, d, ...file, reason: VOD_REASON, cls: VOD_FALLBACK_CLASS, navigate, restart });
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
//   2. Umami audio-fallback {reason, cls, path, route, audio, by} -- not
//      hevc-fallback, whose count stays the video's; audio the start's
//      class (startAudioClass; cls may differ: the master's Dolby with an
//      AAC 5.1 rendition in play), by what blamed the audio ('buffer',
//      'codec', 'message', 'native', 'unpinned' -- Dolby charged first on a
//      failure nobody pinned, audioFallbackClass);
//   3. the restart, as fallbackToOldRoute's; the server counts it by its
//      class (webui_passthrough_fallback_total{class="dolby"|"aac51"}).
// Returns which restart it took.
export function fallbackAudio({ video, reason, cls, path = 'mse', by = '', win = window, doc = document,
    track = (name, data) => { if (win.umami) win.umami.track(name, data); },
    navigate = (u) => loadDocument(win.location, u), restart }) {
    if (!isAudioClass(cls)) return fallbackToOldRoute({ video, reason, path, win, doc, track, navigate, restart });
    const d = video.dataset || {};
    const resourceId = d.resourceId || '';
    const itemId = d.itemId || '';
    try {
        rememberFallback(win, { resourceId, itemId, cls, strike: AUDIO_STRIKING.has(reason) });
    } catch (e) { /* no memory: the restart's note still leaves the class out */ }
    try {
        track('audio-fallback', { reason, cls, path, route: d.videoRoute || '', audio: startAudioClass(video), by: by || '' });
    } catch (e) { /* no telemetry */ }
    return restartFile({ win, doc, d, resourceId, itemId, reason, cls, navigate, restart });
}
