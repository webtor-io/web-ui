// readStreamUrl is the stream's URL as the page rendered it: the video's
// first <source>, or its src attribute. Never a blob: URL.
//
// hls.js on a ManagedMediaSource (Safari on a Mac) replaces the element's
// <source> children with its own <source src="blob:..."> on attach
// (hls.js buffer-controller: removeSourceChildren, addSource). Player.jsx
// read the URL on every render, so the render after the attach handed
// useHls that blob as a new source: useHls destroyed the instance and,
// the blob not being an .m3u8, set video.src to it -- a URL hls.js had
// already revoked. Safari failed it ("WebKitBlobResource error 1", media
// error 4) and the player hung with nothing to fall back from
// (Safari 26.6, 2026-09-29). Chrome's plain MediaSource sets video.src and
// leaves <source> alone, which is why only Safari on a Mac broke.
//
// null when the element holds no real URL (only hls.js's blob): the
// caller keeps the last one it read.
export function readStreamUrl(videoEl) {
    if (!videoEl || !videoEl.querySelectorAll) return null;
    for (const s of videoEl.querySelectorAll('source')) {
        const u = s.getAttribute('src');
        if (u && !u.startsWith('blob:')) return u;
    }
    const a = videoEl.getAttribute('src');
    return a && !a.startsWith('blob:') ? videoEl.src : null;
}
