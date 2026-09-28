// How often Discover's reading of a release name is wrong, measured on the
// releases people actually open (docs/discover.md, "Video switches").
//
// release-video.js was measured on the file paths of opened films. Discover
// reads something else -- the names and titles addons send -- and its
// switches hide releases by that reading, so its error there is what
// matters and is not known. A click on a release records what the name
// said; the player, on the first frame of that release, compares it with
// the file's own codec (the job's media probe, data-video-codecs) and the
// transcoder's route, and sends one `discover-release-check`.
//
// The record lives in sessionStorage (this tab, this visit) for
// RELEASE_TTL_MS and is taken once. Every storage access is wrapped: the
// player reads this from an effect, and Preact drops a component's
// remaining effects after one of them throws.
//
// Nothing is imported: Discover writes the record and the player reads it,
// and neither should carry the other's code.

export const RELEASE_KEY = 'wt-discover-release';
export const RELEASE_TTL_MS = 30 * 60 * 1000;
export const RELEASE_EVENT = 'discover-release-check';

// rememberRelease: the release `hash` was opened from Discover, and this is
// what its name said (`video` from releaseVideo, `uhd` the 4K label).
export function rememberRelease(win, hash, video, uhd, now = Date.now()) {
    try {
        const st = win.sessionStorage;
        if (!st || !hash || !video) return;
        st.setItem(RELEASE_KEY, JSON.stringify({
            h: String(hash).toLowerCase(),
            codec: video.codec,
            hdr: video.hdr || '',
            dv5: !!video.dv5,
            uhd: !!uhd,
            at: now,
        }));
    } catch (e) {
        // No record: this release is not measured.
    }
}

// takeRelease: the record for `resourceId`, removed so it counts once;
// null for none, another release's, or one older than RELEASE_TTL_MS. A
// record for another release is left alone: the viewer may come back.
export function takeRelease(win, resourceId, now = Date.now()) {
    try {
        const st = win.sessionStorage;
        if (!st || !resourceId) return null;
        const r = JSON.parse(st.getItem(RELEASE_KEY) || 'null');
        if (!r || typeof r !== 'object' || r.h !== String(resourceId).toLowerCase()) return null;
        st.removeItem(RELEASE_KEY);
        if (typeof r.at !== 'number' || r.at > now || now - r.at > RELEASE_TTL_MS) return null;
        return r;
    } catch (e) {
        return null;
    }
}

// reportReleaseCheck sends the comparison for the element's release, if
// Discover recorded one; `src` is the file's codec as the player reads it
// (codec-support.js sourceCodec). Returns whether it sent. Never throws.
export function reportReleaseCheck(win, video, src, now = Date.now()) {
    try {
        const umami = win.umami;
        if (!umami || typeof umami.track !== 'function') return false;
        const d = video.dataset || {};
        const r = takeRelease(win, d.resourceId, now);
        if (!r) return false;
        umami.track(RELEASE_EVENT, {
            rel_codec: r.codec,
            rel_hdr: r.hdr,
            rel_dv5: r.dv5,
            rel_uhd: r.uhd,
            src: typeof src === 'string' ? src : 'unknown',
            route: d.videoRoute || '',
            reason: d.routeReason || '',
        });
        return true;
    } catch (e) {
        return false;
    }
}
