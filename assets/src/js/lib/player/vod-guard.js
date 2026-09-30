// A stream nginx-vod serves as the source is (~vod/hls/…: an MP4's first
// video and audio track, muxed) whose codecs this browser refuses.
//
// hls.js cannot add the SourceBuffer (addSourceBuffer throws
// NotSupportedError: a bufferAddCodecError, fatal with one level) and the old
// route's handling (hls-manager.js) recovers every fatal media error, so it
// tried again every ~110 ms for as long as the page was open (2026-09-30, a
// 2160p HEVC MP4 with E-AC-3 in Chrome 154). The server sends the MP4s it
// can tell apart to the transcoder before they reach the page
// (jobs/scripts/vod_route.go); this is for the rest: the codec refusal is
// given up at once, loading stopped, and the file restarted with
// decode-fallback=vod_codecs, which the server sends to the transcoder
// (docs/player.md, "An MP4 whose audio nginx-vod cannot hand the browser").
//
// Only a refusal of the codecs: recoverMediaError cannot change what the
// browser decodes. Any other fatal media error is hls-manager's, as before.
// Once given up, every later fatal error is swallowed -- hls-manager must not
// start recovering the stream the restart is leaving.

export const VOD_CODEC_REFUSALS = new Set(['bufferAddCodecError', 'manifestIncompatibleCodecsError']);

// isVodStream: the player's source is nginx-vod's (rest-api's ~vod URL).
export function isVodStream(url) {
    return /~vod\//.test(String(url || ''));
}

// createVodGuard: the guard of a player whose source is nginx-vod's, in
// hls-manager's guard slot ({ setHls, onHlsError, onBufferCodecs }).
// giveUp(details, mimeType) is told once.
export function createVodGuard({ giveUp = () => {} } = {}) {
    let done = false;
    return {
        get done() { return done; },
        setHls() {},
        onBufferCodecs() {},
        onHlsError(hls, data) {
            if (!data || !data.fatal) return false;
            if (done) return true;
            if (!VOD_CODEC_REFUSALS.has(data.details)) return false;
            done = true;
            try { hls.stopLoad(); } catch (e) { /* the restart goes on */ }
            try { giveUp(data.details, String(data.mimeType || '')); } catch (e) { /* the page goes on */ }
            return true;
        },
    };
}

// composeGuards: two guards in the one slot, the first asked first; either
// taking an error takes it. The audio guard stays what it was (setHls,
// onBufferCodecs are its alone).
export function composeGuards(first, second) {
    if (!first) return second;
    if (!second) return first;
    return {
        setHls(hls) { first.setHls(hls); second.setHls(hls); },
        onBufferCodecs(data) {
            if (typeof first.onBufferCodecs === 'function') first.onBufferCodecs(data);
            if (typeof second.onBufferCodecs === 'function') second.onBufferCodecs(data);
        },
        onHlsError(hls, data) { return first.onHlsError(hls, data) || second.onHlsError(hls, data); },
    };
}
