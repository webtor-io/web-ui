// Which video codecs the browsers of the people who watch on Webtor can decode
// themselves (docs/player.md, "Codec support").
//
// The number behind one decision: whether content-transcoder should hand HEVC
// and AV1 through to the player (fMP4 HLS, `-c:v copy`) instead of
// re-encoding them to H.264. About a quarter of fresh sources are HEVC 1080p+
// or AV1; their re-encode runs slower than realtime and is most of the
// transcoder's CPU. A passthrough helps only the viewers whose browser can play
// the original, and nothing measured that share until this event.
//
// Viewers, not visitors: one `codec-support` event after the first `playing`
// of a <video> the player renders (whenPlaying), off the critical path
// (idle callback), at most once per browser per week (localStorage; once per
// page where storage is unavailable).
//
// The same module holds what the page will declare to the transcoder as
// `decode=` (decodeTokens: the HEVC passthrough plan's video tokens, and
// since 2026-09-28 the audio tokens of multichannel audio): the event
// carries those tokens as computed by that one function, so the share it
// measures is the share that will declare. And a second, later event,
// `playback-quality`, reads the element's dropped/total frames after a minute
// of playback: a browser that decodes HEVC in software says "yes" to every
// question above and can still drop half the frames of a 4K film without an
// error.
//
// Every browser API is reached through `env` (envFromWindow builds it from the
// page), so the probe is testable with fakes, and every access is wrapped: an
// old browser or a throwing API answers `false`, never an exception.

import { iosPlaysHlsJs } from './decode-declaration.js';

export const EVENT = 'codec-support';
export const STORAGE_KEY = 'wt-codec-support';
export const TTL_MS = 7 * 24 * 60 * 60 * 1000;
// On window, not in module scope: a module imported from two entries is
// duplicated with its own state (web-ui CLAUDE.md, splitChunks is off).
export const PAGE_FLAG = '__wtCodecSupport';

// The questions, as the MSE `isTypeSupported` strings. 1080p is HEVC level
// 4 (L120) / AV1 seq_level_idx 8 (level 4.0); 4K is HEVC level 5.1 (L153) /
// AV1 seq_level_idx 12 (level 5.0). hev1 (parameter sets in-band) is asked
// next to hvc1 because a `-c:v copy` of an MKV track can come out as either.
export const MSE_TYPES = {
    hvc: 'video/mp4; codecs="hvc1.1.6.L120.90"',
    hev: 'video/mp4; codecs="hev1.1.6.L120.90"',
    hvc10: 'video/mp4; codecs="hvc1.2.4.L120.90"',
    hvc4k: 'video/mp4; codecs="hvc1.1.6.L153.90"',
    av1: 'video/mp4; codecs="av01.0.08M.08"',
    av1_10: 'video/mp4; codecs="av01.0.08M.10"',
    av1_4k: 'video/mp4; codecs="av01.0.12M.08"',
};
const HLS_TYPE = 'application/vnd.apple.mpegurl';

// A 1080p film as the transcoder would pass it through. Firefox rejects a
// VideoConfiguration without every one of these fields.
const MC_VIDEO = { width: 1920, height: 1080, bitrate: 8000000, framerate: 24 };
const MC_TIMEOUT_MS = 3000;
const NO_DECODING = { ok: false, sm: false, pe: false };

const safe = (fn) => {
    try { return fn(); } catch (e) { return undefined; }
};
const isFn = (v) => typeof v === 'function';

// canPlay: the element answers "probably" or "maybe" — a yes of any
// strength. hls-manager.js takes the same truthiness for native HLS.
const canPlay = (env, type) => {
    const r = safe(() => env.canPlayType(type));
    return r === 'probably' || r === 'maybe';
};

// probeStatic answers everything that has a synchronous API.
//   mse    'mse' (MediaSource), 'mms' (ManagedMediaSource only: iPhone,
//          iOS 17.1+), 'none'. Where both exist the MediaSource answers.
//   hvc…   MSE isTypeSupported for each of MSE_TYPES.
//   n_hls  the element plays HLS itself (Safari, iOS).
//   n_hvc  the element plays HEVC in MP4 itself — what a native-HLS
//          passthrough would need.
//   n_av1  the same for AV1. iOS plays every HLS stream natively
//          (hls-manager.js skips hls.js there even with a
//          ManagedMediaSource), so for iPhone and iPad viewers these two,
//          not the MSE answers, are the passthrough question.
export function probeStatic(env = {}) {
    const MS = safe(() => env.MediaSource);
    const MMS = safe(() => env.ManagedMediaSource);
    const source = isFn(MS) ? MS : isFn(MMS) ? MMS : null;
    const out = { mse: isFn(MS) ? 'mse' : isFn(MMS) ? 'mms' : 'none' };
    for (const [key, type] of Object.entries(MSE_TYPES)) {
        out[key] = safe(() => source !== null && source.isTypeSupported(type) === true) === true;
    }
    out.n_hls = canPlay(env, HLS_TYPE);
    out.n_hvc = canPlay(env, MSE_TYPES.hvc);
    out.n_av1 = canPlay(env, MSE_TYPES.av1);
    return out;
}

// decoding asks mediaCapabilities about one stream (`config`, a
// MediaDecodingConfiguration). isTypeSupported can say yes to a codec the
// machine decodes in software at a fraction of realtime; `smooth` and above
// all `powerEfficient` (a hardware decoder) are the better hint. A rejection,
// a throw, a malformed answer or no answer within the timeout is "no".
async function decoding(mc, config, { timeoutMs, setTimer, clearTimer }) {
    let timer;
    try {
        const info = await Promise.race([
            mc.decodingInfo(config),
            new Promise((resolve) => { timer = setTimer(() => resolve(null), timeoutMs); }),
        ]);
        if (!info || typeof info !== 'object') return NO_DECODING;
        return { ok: info.supported === true, sm: info.smooth === true, pe: info.powerEfficient === true };
    } catch (e) {
        return NO_DECODING;
    } finally {
        if (timer !== undefined) clearTimer(timer);
    }
}

// ---- the declaration: `decode` tokens --------------------------------------
//
// What this browser decodes, as the list of tokens the page will send the
// transcoder in `decode=` (HEVC passthrough plan §2.2, with the owner's
// decisions of 2026-09-27): any support counts, not only a hardware decoder.
// A token is declared when the path this player would play an HLS stream on
// says yes to the token's codec string — MSE `isTypeSupported` where hls.js
// plays, the element's `canPlayType` where the element plays HLS itself. No
// mediaCapabilities and no `powerEfficient` for these: software decoding
// counts, and its cost (dropped frames) is measured separately
// (playback-quality below). The codec strings are the ones the declaration
// uses, so the event and the declaration ask the same questions.
//
// The transcoder reads these tokens; renaming one is a protocol change.
export const DECODE_HEVC = [
    ['hevc8', 'hvc1.1.6.L123.90'], // Main 8-bit, up to 1920×1080, level ≤ 4.1
    ['hevc10', 'hvc1.2.4.L123.90'], // Main10, up to 1920×1080, level ≤ 4.1
    ['hevc8-2160', 'hvc1.1.6.L153.90'], // Main 8-bit, up to 3840×2160, level ≤ 5.1
    ['hevc10-2160', 'hvc1.2.4.L153.90'], // Main10, up to 3840×2160, level ≤ 5.1
    ['hevc-high', 'hvc1.2.4.H153.90'], // tier High (UHD Blu-ray remuxes)
];
// The video tokens: they decide the video route. The audio tokens
// (DECODE_AUDIO_TOKENS, below) come after them; DECODE_TOKENS is the whole
// declaration, in the transcoder's order.
export const DECODE_VIDEO_TOKENS = [...DECODE_HEVC.map(([token]) => token), 'hdr-pq'];

// `hdr-pq`: the browser decodes PQ (HDR10). Asked of mediaCapabilities, the
// only API that takes a transfer function, as Main10 4K — where ~89% of the
// PQ sessions are (≈330 of ≈372 HEVC PQ sessions in a week) — and without
// hdrMetadataType (the question is decoding, not the metadata). A browser
// that decodes 1080p PQ but not 4K PQ is under-declared: its 1080p PQ
// sources are re-encoded, as they are today. The screen is not asked
// (owner's decision: variant A); `dynamic-range` reports it separately.
// `supported` is enough, as for the HEVC tokens. Where decodingInfo is
// missing the token is not declared.
export const PQ_CODEC = 'hvc1.2.4.L153.90';
const PQ_VIDEO = {
    width: 3840, height: 2160, bitrate: 25000000, framerate: 24,
    transferFunction: 'pq', colorGamut: 'rec2020',
};

// The form hls.js asks MediaSource in (mimeTypeForCodec,
// hls.js src/utils/codecs.ts), so the MSE answer is the one hls.js gets.
export const mseType = (codec) => `video/mp4;codecs=${codec}`;
// The RFC 6381 form, for the element's canPlayType and a 'file' decodingInfo.
export const fileType = (codec) => `video/mp4; codecs="${codec}"`;

// hls.js does not trust the HEVC answers of Firefox on Windows and ignores
// them (userAgentHevcSupportIsInaccurate, hls.js src/utils/codecs.ts; the
// test pins this to the installed hls.js). A browser whose answer the player
// itself does not believe declares no HEVC — a question of the answer's
// accuracy, not of hardware.
export const HEVC_ANSWER_INACCURATE = /\(Windows.+Firefox\//i;
const hevcAnswerInaccurate = (env) => HEVC_ANSWER_INACCURATE.test(String(safe(() => env.userAgent) || ''));

// isIOSLike is hls-manager.js's rule for "play HLS natively even where MSE
// exists" (iPhone, iPod, iPad — and an iPad that says it is a Mac). The test
// pins the two to the same expression.
export function isIOSLike(env = {}) {
    const ua = String(safe(() => env.userAgent) || '');
    return /iPad|iPhone|iPod/.test(ua)
        || (safe(() => env.platform) === 'MacIntel' && safe(() => env.maxTouchPoints) > 1);
}

// hlsMediaSource is the MediaSource hls.js uses with its default
// preferManagedMediaSource: the managed one first (hls.js
// src/utils/mediasource-helper.ts, getMediaSource).
function hlsMediaSource(env) {
    return safe(() => env.ManagedMediaSource) || safe(() => env.MediaSource) || safe(() => env.WebKitMediaSource) || null;
}

// hlsJsSupported is Hls.isSupported() (hls.js src/is-supported.ts) over
// `env` instead of `self`: a MediaSource, a usable SourceBuffer where one is
// exposed, and one of the basic codecs. A copy rather than an import so the
// declaration does not pull hls.js into pages without a player; the test
// runs both against the same fakes.
export function hlsJsSupported(env = {}) {
    const ms = hlsMediaSource(env);
    if (!ms) return false;
    const sb = safe(() => env.SourceBuffer) || safe(() => env.WebKitSourceBuffer);
    if (sb && !safe(() => sb.prototype && isFn(sb.prototype.appendBuffer) && isFn(sb.prototype.remove))) return false;
    if (!isFn(safe(() => ms.isTypeSupported))) return false;
    const yes = (type) => safe(() => ms.isTypeSupported(type)) === true;
    return ['avc1.42E01E,mp4a.40.2', 'av01.0.01M.08', 'vp09.00.50.08'].some((c) => yes(`video/mp4;codecs=${c}`))
        || ['mp4a.40.2', 'fLaC'].some((c) => yes(`audio/mp4;codecs=${c}`));
}

// decodePath is how this browser's player would play an HLS stream, by
// hls-manager.js createHls: 'mse' (hls.js), 'native' (the element's own HLS:
// iOS, unless it opted into hls.js -- env.iosHlsJs, decode-declaration.js
// iosPlaysHlsJs -- or where hls.js cannot run), 'none' (neither — no HLS here).
export function decodePath(env = {}) {
    if ((!isIOSLike(env) || env.iosHlsJs === true) && hlsJsSupported(env)) return 'mse';
    return canPlay(env, HLS_TYPE) ? 'native' : 'none';
}

// hevcDecodeTokens is the synchronous part of the declaration: the HEVC
// tokens, in DECODE_HEVC order.
export function hevcDecodeTokens(env = {}, path = decodePath(env)) {
    if (path === 'none' || hevcAnswerInaccurate(env)) return [];
    let yes;
    if (path === 'mse') {
        const ms = hlsMediaSource(env);
        yes = (codec) => safe(() => ms.isTypeSupported(mseType(codec))) === true;
    } else {
        yes = (codec) => canPlay(env, fileType(codec));
    }
    return DECODE_HEVC.filter(([, codec]) => yes(codec)).map(([token]) => token);
}

// decodesPQ answers `hdr-pq`, on the MSE path only ('media-source'). The
// same Firefox-on-Windows rule: the question is asked of an HEVC codec.
//
// Native HLS -- iOS and iPadOS, where the element plays HLS itself -- is
// never asked: it refuses a PQ passthrough variant without a word. The
// iPhone fetches the master and nothing after it, no variant playlist, no
// error, so no fallback either (2026-09-29: 0 of 9 PQ passthrough sessions
// on iPhones got past the master in 24 h; the owner's iPhone on five real
// files: PQ 0 of 3, SDR 2 of 2 -- one 4K at 6.4 Mbit/s, one 1080p at
// 40 Mbit/s -- VIDEO-RANGE the only difference in the master). decodingInfo
// said yes there all the same ('file'). A PQ source is re-encoded for these
// viewers, as before passthrough.
const pqConfig = () => ({
    type: 'media-source',
    video: { contentType: mseType(PQ_CODEC), ...PQ_VIDEO },
});

async function decodesPQ(env, path, opts) {
    if (path !== 'mse' || hevcAnswerInaccurate(env)) return false;
    const mc = safe(() => env.mediaCapabilities);
    if (safe(() => isFn(mc.decodingInfo)) !== true) return false;
    const info = await decoding(mc, pqConfig(), opts);
    return info.ok;
}

// supportedAnswer asks decodingInfo one question without a deadline: it
// resolves with the browser's answer whenever it comes. A rejection, a
// throw or a malformed answer is an answer ("no"); only silence is not --
// the promise then stays pending.
function supportedAnswer(mc, config) {
    try {
        return Promise.resolve(mc.decodingInfo(config))
            .then((info) => !!info && typeof info === 'object' && info.supported === true, () => false);
    } catch (e) {
        return Promise.resolve(false);
    }
}

// pqAnswer is the `hdr-pq` question without a deadline (supportedAnswer). A
// browser without decodingInfo has answered too: the token is never
// declared there. The event above treats 3 s of silence as "no" (it counts
// viewers, and a count has to close); the declaration must not: a check
// that did not answer is not a browser that cannot decode
// (decode-declaration.js).
function pqAnswer(env, path) {
    if (path !== 'mse' || hevcAnswerInaccurate(env)) return Promise.resolve(false);
    const mc = safe(() => env.mediaCapabilities);
    if (safe(() => isFn(mc.decodingInfo)) !== true) return Promise.resolve(false);
    return supportedAnswer(mc, pqConfig());
}

// ---- the audio tokens --------------------------------------------------------
//
// Multichannel audio (content-transcoder; owner's go of 2026-09-28): about
// 40% of sources carry more than two channels -- E-AC-3 24%, AC-3 7%, AAC
// 7% of 1126 in a day -- and every one is downmixed to stereo today. The
// transcoder keeps the channels only for a browser that says it decodes
// them:
//   aac51  AAC-LC with up to six channels: a 5.1 AAC track is copied, and
//          every other multichannel track is encoded to AAC 5.1 instead of
//          stereo (MPEG-TS and fMP4 alike);
//   ac3    AC-3 copied as it is -- in fMP4 only, i.e. a passthrough session;
//   ec3    E-AC-3 copied as it is, Atmos (JOC) included -- fMP4 only: the
//          full hls.js build refuses E-AC-3 in MPEG-TS (tsdemuxer.ts).
// For audio a missing token is exactly the audio the transcoder has always
// made (stereo AAC); nothing is refused for want of one.
//
// Independent of the HEVC tokens: asked and declared whatever the browser
// says about HEVC, Firefox on Windows included. hls.js distrusts that
// browser's HEVC answers and no audio answer of any browser (codecs.ts,
// mediacapabilities-helper.ts; the test pins it to the installed hls.js).
export const AAC51_TOKEN = 'aac51';
export const AAC51_CODEC = 'mp4a.40.2';
// The AAC 5.1 question for mediaCapabilities: six channels, 48 kHz, at the
// 5.1 rate of the spec (384 kbit/s). Only `channels` is what is asked; the
// other two make it a stream a browser would really be handed.
export const AAC51_AUDIO = { channels: '6', bitrate: 384000, samplerate: 48000 };
export const DECODE_DOLBY = [
    ['ac3', 'ac-3'],
    ['ec3', 'ec-3'],
];
export const DECODE_AUDIO_TOKENS = [AAC51_TOKEN, ...DECODE_DOLBY.map(([token]) => token)];
export const DECODE_TOKENS = [...DECODE_VIDEO_TOKENS, ...DECODE_AUDIO_TOKENS];

// hls.js's spelling of an audio type (mimeTypeForCodec(codec, 'audio')), so
// the MSE answer is the one hls.js gets; the RFC 6381 one for the element.
export const audioMseType = (codec) => `audio/mp4;codecs=${codec}`;
export const audioFileType = (codec) => `audio/mp4; codecs="${codec}"`;

// dolbyDecodeTokens: `ac3` and `ec3`, known at once. On the MSE path the
// question hls.js asks itself before it keeps a level or an audio rendition
// (level-controller.ts isAudioSupported: its MediaSource -- the managed one
// first -- and its spelling); on the native path the element's canPlayType.
export function dolbyDecodeTokens(env = {}, path = decodePath(env)) {
    if (path === 'none') return [];
    let yes;
    if (path === 'mse') {
        const ms = hlsMediaSource(env);
        yes = (codec) => safe(() => ms.isTypeSupported(audioMseType(codec))) === true;
    } else {
        yes = (codec) => canPlay(env, audioFileType(codec));
    }
    return DECODE_DOLBY.filter(([, codec]) => yes(codec)).map(([token]) => token);
}

// `aac51`. isTypeSupported has no word for channels -- every MSE browser
// with AAC says yes to mp4a.40.2 -- so on the MSE path the one API that
// takes a channel count answers: decodingInfo as 'media-source', the same
// audio question hls.js asks itself for a level whose audio rendition has
// more than two CHANNELS (mediacapabilities-helper.ts). `supported` is
// enough, as for `hdr-pq`; without decodingInfo the token is not declared.
// On the native path the element's canPlayType for AAC.
const aac51Config = () => ({ type: 'media-source', audio: { contentType: audioMseType(AAC51_CODEC), ...AAC51_AUDIO } });

// decodesAac51 is the events' question: a deadline, silence is "no".
async function decodesAac51(env, path, opts) {
    if (path === 'none') return false;
    if (path === 'native') return canPlay(env, audioFileType(AAC51_CODEC));
    const mc = safe(() => env.mediaCapabilities);
    if (safe(() => isFn(mc.decodingInfo)) !== true) return false;
    return (await decoding(mc, aac51Config(), opts)).ok;
}

// aac51Answer is the declaration's: no deadline (supportedAnswer), the rule
// of pqAnswer.
function aac51Answer(env, path) {
    if (path === 'none') return Promise.resolve(false);
    if (path === 'native') return Promise.resolve(canPlay(env, audioFileType(AAC51_CODEC)));
    const mc = safe(() => env.mediaCapabilities);
    if (safe(() => isFn(mc.decodingInfo)) !== true) return Promise.resolve(false);
    return supportedAnswer(mc, aac51Config());
}

// declarationSupport is what the page declares (decode-declaration.js), in
// three parts: the HEVC tokens, known at once; `pq`, a promise of the
// `hdr-pq` answer; `audio`, a promise of the audio tokens in
// DECODE_AUDIO_TOKENS order, which settles once `aac51` has its answer (the
// Dolby ones are known at once and wait with it). Neither promise has a
// deadline, and neither waits for the other. The same questions, codec
// strings and path as decodeTokens: the event measures the share that
// declares. Without an HEVC token there is nothing for `hdr-pq` to qualify,
// and the question is not asked; the audio is asked all the same -- unless
// `opts.audio` is false (a page that does not declare audio,
// decode-declaration.js takesPartAudio): then nothing is asked about it and
// `audio` is null.
export function declarationSupport(env = {}, opts = {}) {
    const path = decodePath(env);
    const hevc = hevcDecodeTokens(env, path);
    const askAudio = opts.audio !== false;
    const dolby = askAudio ? dolbyDecodeTokens(env, path) : [];
    return {
        path,
        hevc,
        pq: hevc.length ? pqAnswer(env, path) : Promise.resolve(false),
        audio: askAudio ? aac51Answer(env, path).then((aac) => [...(aac ? [AAC51_TOKEN] : []), ...dolby]) : null,
    };
}

const timing = ({ timeoutMs = MC_TIMEOUT_MS, setTimer = setTimeout, clearTimer = clearTimeout } = {}) => (
    { timeoutMs, setTimer, clearTimer });

async function decodeSupport(env, opts) {
    const path = decodePath(env);
    const t = timing(opts);
    const tokens = hevcDecodeTokens(env, path);
    const dolby = dolbyDecodeTokens(env, path);
    const [pq, aac] = await Promise.all([decodesPQ(env, path, t), decodesAac51(env, path, t)]);
    if (pq) tokens.push('hdr-pq');
    if (aac) tokens.push(AAC51_TOKEN);
    tokens.push(...dolby);
    return { path, tokens };
}

// decodeTokens is the declaration: the tokens this browser declares, in
// DECODE_TOKENS order ([] for none). The HEVC and Dolby ones are known at
// once; `hdr-pq` and, on the MSE path, `aac51` wait for decodingInfo (side
// by side, at most `timeoutMs` each, 3 s; a timeout is "no"). Never
// rejects. The declaration (stage 3) and every event here use this one
// function.
export async function decodeTokens(env = {}, opts = {}) {
    try {
        return (await decodeSupport(env, opts)).tokens;
    } catch (e) {
        return [];
    }
}

// dynamicRange: what the screen says it shows — 'high', 'standard', or
// 'unknown' where the media feature (or matchMedia) is missing. Reported,
// never declared.
export function dynamicRange(env = {}) {
    const mm = safe(() => env.matchMedia);
    if (!isFn(mm)) return 'unknown';
    if (safe(() => mm('(dynamic-range: high)').matches) === true) return 'high';
    if (safe(() => mm('(dynamic-range: standard)').matches) === true) return 'standard';
    return 'unknown';
}

// probeCodecSupport is probeStatic plus the mediaCapabilities answers:
//   mc                   decodingInfo exists at all;
//   mc_hvc, mc_hvc_sm, mc_hvc_pe   HEVC Main 1080p: supported, smooth,
//                                  powerEfficient;
//   mc_av1, mc_av1_sm, mc_av1_pe   the same for AV1 8-bit 1080p;
// plus the declaration:
//   hevc8 … hdr-pq, aac51, ac3, ec3
//                        one boolean per DECODE_TOKENS entry;
//   decode               the tokens joined with ',' — the `decode=` value
//                        this browser would send ('' for none);
//   decode_path          the path they were asked on: mse / native / none;
//   dynamic-range        high / standard / unknown.
// Never rejects.
export async function probeCodecSupport(env = {}, opts = {}) {
    const out = probeStatic(env);
    const mc = safe(() => env.mediaCapabilities);
    out.mc = safe(() => isFn(mc.decodingInfo)) === true;
    const t = timing(opts);
    const mse = (contentType) => ({ type: 'media-source', video: { contentType, ...MC_VIDEO } });
    const [h, a, decl] = await Promise.all([
        out.mc ? decoding(mc, mse(MSE_TYPES.hvc), t) : NO_DECODING,
        out.mc ? decoding(mc, mse(MSE_TYPES.av1), t) : NO_DECODING,
        decodeSupport(env, t).catch(() => ({ path: 'none', tokens: [] })),
    ]);
    out.mc_hvc = h.ok;
    out.mc_hvc_sm = h.sm;
    out.mc_hvc_pe = h.pe;
    out.mc_av1 = a.ok;
    out.mc_av1_sm = a.sm;
    out.mc_av1_pe = a.pe;
    for (const token of DECODE_TOKENS) out[token] = decl.tokens.includes(token);
    out.decode = decl.tokens.join(',');
    out.decode_path = decl.path;
    out['dynamic-range'] = dynamicRange(env);
    return out;
}

// envFromWindow collects the probe's inputs from a page. `video` is any media
// element to ask canPlayType of (the answer does not depend on its state); a
// detached one is made when none is given. matchMedia is bound to the
// window: called bare it throws Illegal invocation.
export function envFromWindow(win, video) {
    const el = video || safe(() => win.document.createElement('video'));
    const canPlayType = el && isFn(safe(() => el.canPlayType)) ? (type) => el.canPlayType(type) : undefined;
    const matchMedia = isFn(safe(() => win.matchMedia)) ? (q) => win.matchMedia(q) : undefined;
    const nav = safe(() => win.navigator);
    return {
        MediaSource: safe(() => win.MediaSource),
        ManagedMediaSource: safe(() => win.ManagedMediaSource),
        WebKitMediaSource: safe(() => win.WebKitMediaSource),
        SourceBuffer: safe(() => win.SourceBuffer),
        WebKitSourceBuffer: safe(() => win.WebKitSourceBuffer),
        canPlayType,
        mediaCapabilities: safe(() => nav.mediaCapabilities),
        userAgent: safe(() => nav.userAgent),
        platform: safe(() => nav.platform),
        maxTouchPoints: safe(() => nav.maxTouchPoints),
        iosHlsJs: safe(() => iosPlaysHlsJs(win)) === true,
        matchMedia,
    };
}

// Codecs ffprobe reports for a cover picture (a "video" stream with the
// attached_pic disposition), not for the film.
const STILLS = new Set(['mjpeg', 'png', 'bmp', 'gif', 'webp', 'jpeg2000', 'tiff']);

// sourceCodec reads the source file's video codec off `data-video-codecs`
// (stream_video.html: the codec of every video stream the job's media probe
// saw, space separated) as 'h264' | 'hevc' | 'av1' | 'other', or 'unknown'
// when the page has no probe (the attribute is absent) or no film stream.
export function sourceCodec(attr) {
    if (typeof attr !== 'string') return 'unknown';
    const first = attr.trim().toLowerCase().split(/\s+/).find((c) => c && !STILLS.has(c));
    if (!first) return 'unknown';
    if (first === 'h264' || first === 'hevc' || first === 'av1') return first;
    return 'other';
}

// playbackPath: how this player plays its source — hls.js over MSE, the
// element's own HLS (iOS, and Safari without hls.js), or a plain file. The
// HLS test is the one useHls makes.
export function playbackPath(hls, sourceUrl) {
    if (hls) return 'hlsjs';
    const url = typeof sourceUrl === 'string' ? sourceUrl : '';
    return url.includes('.m3u8') || url.includes('mpegurl') ? 'native' : 'direct';
}

function storageOf(win) {
    return safe(() => win.localStorage) || null;
}

// lastSent is the time of this browser's last report: 0 for never, null
// when there is no storage to ask.
function lastSent(storage) {
    if (!storage) return null;
    const raw = safe(() => storage.getItem(STORAGE_KEY));
    if (raw === undefined) return null;
    const ts = Number(raw);
    return Number.isFinite(ts) ? ts : 0;
}

// An idle callback where there is one; a timer where there is none, or where
// the one there is throws (a polyfill, a patched window).
function defaultSchedule(win) {
    return (fn) => {
        const ric = safe(() => win.requestIdleCallback);
        if (isFn(ric) && safe(() => { ric.call(win, fn, { timeout: 10000 }); return true; }) === true) return;
        setTimeout(fn, 2000);
    };
}

const umamiOf = (win) => {
    const u = safe(() => win.umami);
    return u && isFn(u.track) ? u : null;
};

// reportCodecSupport sends the event unless this browser sent one in the last
// TTL_MS or this page already has. `extra` is what the player knows about the
// stream (src, tc, pl, emb — see docs/player.md). Returns whether a report was
// scheduled. Without window.umami it does nothing and remembers nothing, so a
// page where analytics loads late is not written off for a week.
//
// Never throws. The player calls it from a useEffect when the element is
// already playing at mount, and Preact drops a component's remaining pending
// effects after one of them throws — player_ready among them.
export function reportCodecSupport(extra = {}, deps = {}) {
    try {
        return report(extra, deps);
    } catch (e) {
        return false;
    }
}

function report(extra, deps) {
    const win = deps.win || (typeof window !== 'undefined' ? window : null);
    if (!win) return false;
    const storage = 'storage' in deps ? deps.storage : storageOf(win);
    const now = deps.now || (() => Date.now());
    const schedule = deps.schedule || defaultSchedule(win);
    const probe = deps.probe || probeCodecSupport;
    const env = deps.env || (() => envFromWindow(win));

    if (safe(() => win[PAGE_FLAG]) === true) return false;
    if (!umamiOf(win)) return false;
    const last = lastSent(storage);
    const t = now();
    // A stamp from the future (a clock set back) is no reason to stay quiet.
    if (last !== null && last > 0 && last <= t && t - last < TTL_MS) return false;
    safe(() => { win[PAGE_FLAG] = true; });

    schedule(async () => {
        let data;
        try {
            data = { ...(await probe(env())), ...extra };
        } catch (e) {
            return;
        }
        const umami = umamiOf(win);
        if (!umami) return;
        // Stamped when it is actually sent: a tab closed before the idle
        // moment has reported nothing, and should report next time.
        if (storage) safe(() => storage.setItem(STORAGE_KEY, String(now())));
        safe(() => umami.track(EVENT, data));
    });
    return true;
}

// isPlaying: the element is presenting frames right now (not paused, not
// ended, enough data to move).
function isPlaying(video) {
    return safe(() => !video.paused && !video.ended && video.readyState >= 3) === true;
}

// whenPlaying calls `fn` once, on the element's first `playing` — or at once
// when it is already playing: a direct file with `autoplay` in the markup can
// start before the player mounts, and its `playing` has already gone by.
// Returns the cleanup.
export function whenPlaying(video, fn) {
    let done = false;
    const fire = () => {
        if (done) return;
        done = true;
        video.removeEventListener('playing', fire);
        fn();
    };
    if (isPlaying(video)) {
        fire();
        return () => {};
    }
    video.addEventListener('playing', fire);
    return () => {
        done = true;
        video.removeEventListener('playing', fire);
    };
}

// ---- playback-quality --------------------------------------------------------
//
// The declaration counts software decoding as support (owner's decision), so
// its risk — a slow machine decoding 4K HEVC drops frames without a decoder
// error — is measured here: the element's own frame counters after a minute
// of playback. Today (no passthrough yet) this is the baseline, and it
// already sees software HEVC: MP4 HEVC sources go to the player as they are.
//
// One `playback-quality` event per page load, when the first video on it has
// played QUALITY_AFTER_S seconds of media; not tied to codec-support's
// weekly sample, because this is a fact about a play, not about a browser.
// A separate event rather than a field of codec-support: that one goes out at
// the first frame and counts viewers; holding it for a minute would drop
// everyone who stops earlier and break the series.
//
// Cheap by construction: one listener doing arithmetic on `timeupdate`, one
// getVideoPlaybackQuality() call at the mark, the send in an idle callback.
// Nothing here touches the element's state.

export const QUALITY_EVENT = 'playback-quality';
export const QUALITY_PAGE_FLAG = '__wtPlaybackQuality';
export const QUALITY_AFTER_S = 60;
// A timeupdate step longer than this is a jump (a seek, a gap hls.js
// skipped), not playback: timeupdate fires every 15–250 ms while the media
// plays, so a real step is a fraction of a second even at the fastest rate
// the player offers. A step back (a seek back, a new source from 0) is no
// playback either. The two bounds are the whole rule: listening to
// `seeking` as well would only drop the rare forward seek under 2 s.
const MAX_STEP_S = 2;

const count = (n) => typeof n === 'number' && Number.isFinite(n) && n >= 0;

// playbackQuality reads the element's frame counters since its last load:
// {dropped, total}, or null where the API is missing, throws, or answers
// something that is not a pair of counts.
export function playbackQuality(video) {
    const q = safe(() => (isFn(video.getVideoPlaybackQuality) ? video.getVideoPlaybackQuality() : null));
    if (!q || typeof q !== 'object') return null;
    const dropped = safe(() => q.droppedVideoFrames);
    const total = safe(() => q.totalVideoFrames);
    if (!count(dropped) || !count(total)) return null;
    return { dropped, total };
}

// afterPlayed calls `fn(played, hidden)` once, when `seconds` of media have
// played on the element: the sum of the small forward steps between
// timeupdates (MAX_STEP_S). `hidden` is whether any counted step happened
// while isHidden() said so — a background tab may stop rendering video, and
// its frame counts read differently. Returns the cleanup.
export function afterPlayed(video, seconds, fn, isHidden = () => false) {
    let played = 0;
    let last = null;
    let hidden = false;
    let done = false;
    const onTime = () => {
        if (done) return;
        const t = safe(() => video.currentTime);
        if (typeof t !== 'number' || !Number.isFinite(t)) return;
        if (last !== null) {
            const step = t - last;
            if (step > 0 && step <= MAX_STEP_S) {
                played += step;
                if (safe(isHidden) === true) hidden = true;
            }
        }
        last = t;
        if (played >= seconds) {
            stop();
            safe(() => fn(played, hidden));
        }
    };
    const stop = () => {
        done = true;
        video.removeEventListener('timeupdate', onTime);
    };
    video.addEventListener('timeupdate', onTime);
    return stop;
}

// watchPlaybackQuality sends `playback-quality` for `video` once it has
// played a minute, unless this page already has: the frame counters, what was
// played, the declaration (`decode`) and `extra` — what the player knows
// about the stream, the same src/tc/pl/emb as codec-support; a function is
// called at the mark, when the player has settled on a path. Without
// window.umami at the mark nothing is sent and the page is not marked.
// Returns the cleanup; never throws.
export function watchPlaybackQuality(video, extra = {}, deps = {}) {
    try {
        const win = deps.win || (typeof window !== 'undefined' ? window : null);
        if (!win || !video) return () => {};
        const schedule = deps.schedule || defaultSchedule(win);
        const tokens = deps.tokens || (() => decodeTokens(envFromWindow(win)));
        const isHidden = deps.hidden || (() => safe(() => win.document.hidden) === true);
        return afterPlayed(video, QUALITY_AFTER_S, (played, hidden) => {
            const q = playbackQuality(video);
            if (!q) return;
            if (safe(() => win[QUALITY_PAGE_FLAG]) === true) return;
            if (!umamiOf(win)) return;
            safe(() => { win[QUALITY_PAGE_FLAG] = true; });
            const height = safe(() => video.videoHeight);
            const rate = safe(() => video.playbackRate);
            const data = {
                dropped: q.dropped,
                total: q.total,
                played: Math.round(played),
                height: count(height) ? height : 0,
                rate: typeof rate === 'number' && Number.isFinite(rate) ? rate : 1,
                hidden,
                ...((isFn(extra) ? safe(extra) : extra) || {}),
            };
            // No frames at all is a reading too (a decoder that never
            // started); it has no share.
            if (q.total > 0) data.drop_pct = Math.round((q.dropped / q.total) * 10000) / 100;
            schedule(async () => {
                let decode = '';
                try {
                    decode = (await tokens()).join(',');
                } catch (e) {
                    // The frames are the point; a failed declaration is ''.
                }
                const umami = umamiOf(win);
                if (!umami) return;
                safe(() => umami.track(QUALITY_EVENT, { ...data, decode }));
            });
        }, isHidden);
    } catch (e) {
        return () => {};
    }
}
