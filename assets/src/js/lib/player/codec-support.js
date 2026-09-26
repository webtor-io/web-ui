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
// Every browser API is reached through `env` (envFromWindow builds it from the
// page), so the probe is testable with fakes, and every access is wrapped: an
// old browser or a throwing API answers `false`, never an exception.

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

// probeStatic answers everything that has a synchronous API.
//   mse    'mse' (MediaSource), 'mms' (ManagedMediaSource only: iPhone,
//          iOS 17.1+), 'none'. Where both exist the MediaSource answers.
//   hvc…   MSE isTypeSupported for each of MSE_TYPES.
//   n_hls  the element plays HLS itself (Safari, iOS).
//   n_hvc  the element plays HEVC in MP4 itself — what a native-HLS
//          passthrough would need.
export function probeStatic(env = {}) {
    const MS = safe(() => env.MediaSource);
    const MMS = safe(() => env.ManagedMediaSource);
    const source = isFn(MS) ? MS : isFn(MMS) ? MMS : null;
    const out = { mse: isFn(MS) ? 'mse' : isFn(MMS) ? 'mms' : 'none' };
    for (const [key, type] of Object.entries(MSE_TYPES)) {
        out[key] = safe(() => source !== null && source.isTypeSupported(type) === true) === true;
    }
    const canPlay = (type) => {
        const r = safe(() => env.canPlayType(type));
        return r === 'probably' || r === 'maybe';
    };
    out.n_hls = canPlay(HLS_TYPE);
    out.n_hvc = canPlay(MSE_TYPES.hvc);
    return out;
}

// decoding asks mediaCapabilities about one 1080p stream through MSE.
// isTypeSupported can say yes to a codec the machine decodes in software at a
// fraction of realtime; `smooth` and above all `powerEfficient` (a hardware
// decoder) are the better hint. A rejection, a throw, a malformed answer or no
// answer within the timeout is "no".
async function decoding(mc, contentType, { timeoutMs, setTimer, clearTimer }) {
    let timer;
    try {
        const info = await Promise.race([
            mc.decodingInfo({ type: 'media-source', video: { contentType, ...MC_VIDEO } }),
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

// probeCodecSupport is probeStatic plus the mediaCapabilities answers:
//   mc                   decodingInfo exists at all;
//   mc_hvc, mc_hvc_sm, mc_hvc_pe   HEVC Main 1080p: supported, smooth,
//                                  powerEfficient;
//   mc_av1, mc_av1_sm, mc_av1_pe   the same for AV1 8-bit 1080p.
// Never rejects.
export async function probeCodecSupport(env = {}, {
    timeoutMs = MC_TIMEOUT_MS,
    setTimer = setTimeout,
    clearTimer = clearTimeout,
} = {}) {
    const out = probeStatic(env);
    const mc = safe(() => env.mediaCapabilities);
    out.mc = safe(() => isFn(mc.decodingInfo)) === true;
    const opts = { timeoutMs, setTimer, clearTimer };
    const [h, a] = out.mc
        ? await Promise.all([decoding(mc, MSE_TYPES.hvc, opts), decoding(mc, MSE_TYPES.av1, opts)])
        : [NO_DECODING, NO_DECODING];
    out.mc_hvc = h.ok;
    out.mc_hvc_sm = h.sm;
    out.mc_hvc_pe = h.pe;
    out.mc_av1 = a.ok;
    out.mc_av1_sm = a.sm;
    out.mc_av1_pe = a.pe;
    return out;
}

// envFromWindow collects the probe's inputs from a page. `video` is any media
// element to ask canPlayType of (the answer does not depend on its state); a
// detached one is made when none is given.
export function envFromWindow(win, video) {
    const el = video || safe(() => win.document.createElement('video'));
    const canPlayType = el && isFn(safe(() => el.canPlayType)) ? (type) => el.canPlayType(type) : undefined;
    return {
        MediaSource: safe(() => win.MediaSource),
        ManagedMediaSource: safe(() => win.ManagedMediaSource),
        canPlayType,
        mediaCapabilities: safe(() => win.navigator.mediaCapabilities),
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

function defaultSchedule(win) {
    return (fn) => {
        const ric = safe(() => win.requestIdleCallback);
        if (isFn(ric)) ric.call(win, fn, { timeout: 10000 });
        else setTimeout(fn, 2000);
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
export function reportCodecSupport(extra = {}, deps = {}) {
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
