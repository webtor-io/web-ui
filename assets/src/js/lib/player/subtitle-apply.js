/**
 * The picker is the single source of truth for what is on screen, and this
 * module is the one place that writes that answer into the two things that
 * can draw subtitles: hls.js (embedded tracks, from the manifest) and the
 * media element's own <track>s (side-loaded — uploads, OpenSubtitles,
 * sidecars, AI translations).
 *
 * Why a module of its own, and why "disabled" everywhere:
 *
 * hls.js is not a passive renderer. Its SubtitleTrackController listens to
 * the media element's textTracks `change` event (`onTextTracksChanged`),
 * scans every *labeled* subtitle track, and picks one: the LAST track in
 * 'hidden' mode, or the FIRST in 'showing' (which wins and breaks). It then
 * maps that track back to a manifest index and calls setSubtitleTrack with
 * it. A track it cannot map — every element-backed <track> of ours — maps
 * to -1.
 *
 * So a manifest track left in 'hidden' is not "off". It is a standing
 * invitation: the next change event hands it back to hls.js, which starts
 * loading its cues and, if `subtitleDisplay` is still true, draws them
 * *on top of* the side-loaded track the viewer actually chose. That is the
 * overlap this module exists to prevent — measured on stage 2026-09-16 as
 * an embedded "Full (rus)" track sitting at mode 'showing' with 25 cues
 * while `hls.subtitleTrack === -1` and an AI Catalan <track> was showing
 * too.
 *
 * The other half of the same story: hls.js's `toggleTrackModes` (it runs on
 * every setSubtitleTrack, including -1, and on a subtitleDisplay change
 * while a track is selected) sets EVERY labeled subtitle track that is not
 * its own current one to 'disabled' — our element tracks included. So
 * hls.js will undo our element modes on its own transitions, which is why
 * callers re-assert this selection rather than applying it once.
 *
 * Ordering inside apply is load-bearing: the hls.js writes come first,
 * because they toggle modes themselves, and the element modes are written
 * afterwards over the top.
 */

// The provider of tracks that come out of the transcoder's HLS manifest.
// Everything else is side-loaded and exists as a <track> element.
const EMBEDDED_PROVIDER = 'MediaProbe';

// The id of the carrier chip that stands for "no subtitles". It is a chip
// like any other, and it is never a track.
const NONE_ID = 'none';

function textTracksOf(video) {
    if (!video || !video.textTracks) return [];
    return Array.from(video.textTracks);
}

/**
 * selectionFor reads a picker chip into the three fields this module needs.
 */
export function selectionFor(el) {
    if (!el) return null;
    return {
        id: el.getAttribute('data-id') || '',
        provider: el.getAttribute('data-provider') || '',
        mpId: el.getAttribute('data-mp-id'),
    };
}

/**
 * readSelection answers "what did the picker settle on" by reading the chip
 * that carries the marker. `null` means the question does not apply here —
 * no picker in the page (a bare embed), or a picker that has not marked
 * anything yet — and every caller treats that as "leave playback alone"
 * rather than as "off".
 */
export function readSelection(scope) {
    if (!scope || typeof scope.querySelector !== 'function') return null;
    return selectionFor(scope.querySelector('.subtitle[data-default="true"]'));
}

/**
 * isEmbedded says whether the selection is a manifest track hls.js drives.
 * A MediaProbe chip without a usable `data-mp-id` is not one: there is no
 * index to switch to, and pretending otherwise used to end in
 * `hls.subtitleTrack = NaN`, which hls.js logs and ignores — leaving
 * whatever was playing before on screen under a chip that says otherwise.
 */
export function isEmbedded(selection) {
    if (!selection || selection.provider !== EMBEDDED_PROVIDER) return false;
    return embeddedIndex(selection) !== null;
}

function embeddedIndex(selection) {
    const raw = selection ? selection.mpId : null;
    if (raw === null || raw === undefined || raw === '') return null;
    const n = parseInt(raw, 10);
    return Number.isNaN(n) ? null : n;
}

// wantedTrackID is the element track that must be showing, or '' when
// nothing element-backed should be ("None", or an embedded selection).
function wantedTrackID(selection) {
    if (!selection) return '';
    const id = selection.id || '';
    if (!id || id === NONE_ID) return '';
    return id;
}

/**
 * applySubtitleSelection writes `selection` into the player.
 *
 * `hls` may be null — native HLS (iOS) or an instance that does not exist
 * yet, which is the ordinary state during a mount: activateSubtitle can run
 * before the HLS instance is created. Then only the element modes are
 * written, and the hls.js half is re-applied later by initDefaultTracks.
 */
export function applySubtitleSelection(video, hls, selection) {
    if (!selection) return;

    if (isEmbedded(selection)) {
        if (hls) {
            // display before track: setting subtitleDisplay while no track
            // is selected does not toggle modes, and the track write that
            // follows applies both at once.
            hls.subtitleDisplay = true;
            hls.subtitleTrack = embeddedIndex(selection);
        }
        // hls.js's own toggleTrackModes has already disabled everything but
        // its current track. This is the same write for the case where
        // there is no hls.js at all, and it is what keeps a side-loaded
        // track from being fetched while an embedded one plays.
        for (const t of textTracksOf(video)) {
            if (t.id) t.mode = 'disabled';
        }
        return;
    }

    if (hls) {
        // -1 first: it sets the controller's trackId, so the
        // subtitleDisplay write behind it cannot toggle modes back on.
        hls.subtitleTrack = -1;
        hls.subtitleDisplay = false;
    }
    const wanted = wantedTrackID(selection);
    for (const t of textTracksOf(video)) {
        if (wanted && t.id === wanted) t.mode = 'showing';
        // 'disabled', never 'hidden'. A hidden element track is still
        // fetched by the browser, which with 40+ OpenSubtitles tracks meant
        // a burst of downloads per selection; a hidden hls.js-managed track
        // is the latch onTextTracksChanged grabs. Two different reasons,
        // one mode.
        else t.mode = 'disabled';
    }
}

/**
 * selectionHolds answers whether what the player is actually doing already
 * matches the selection. It is what keeps the re-assertion from looping:
 * every apply writes modes, every mode write wakes hls.js, and hls.js's
 * reaction is what calls us back.
 */
export function selectionHolds(video, hls, selection) {
    if (!selection) return true;

    if (isEmbedded(selection)) {
        if (hls && (hls.subtitleTrack !== embeddedIndex(selection) || !hls.subtitleDisplay)) return false;
        for (const t of textTracksOf(video)) {
            if (t.id && t.mode !== 'disabled') return false;
        }
        return true;
    }

    // hls.js holding a track at all means it latched onto one: nothing
    // embedded was chosen.
    if (hls && hls.subtitleTrack !== -1) return false;
    const wanted = wantedTrackID(selection);
    for (const t of textTracksOf(video)) {
        const expected = t.id && t.id === wanted ? 'showing' : 'disabled';
        if (t.mode !== expected) return false;
    }
    return true;
}
