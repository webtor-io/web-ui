/**
 * Session-timeline compensation for element-backed text tracks.
 *
 * Side-loaded subtitles (<track>: user uploads, OpenSubtitles, external)
 * carry cue times in absolute movie time. A transcoder session that starts
 * mid-movie (resume, seek) exposes a media timeline that begins at zero, so
 * without compensation those cues can never become active. The authored
 * times are stashed on each cue on first touch and every apply recomputes
 * from them, so repeated seeks never accumulate drift.
 *
 * HLS-managed tracks (created by hls.js from the manifest, no <track>
 * element) are already session-relative — callers must only pass
 * element-backed tracks here.
 */

// Where a cue that ends before the session start is kept. Any negative
// time works (the playhead is never negative, so neither the interval test
// nor the "missed cues" rule can reach it); one fixed value keeps the
// parked cues easy to recognise in a debugger.
export const PARKED_AT = -1;

// The viewer's own correction for a subtitle file that runs early or late
// (setTrackDelay). It lives ON the track, not in an argument: cues are
// re-shifted from five places (a seek, a late <track> load, a translation
// reload, ...) and every one of them would have to be handed the number --
// the one that was not would silently drop the viewer's correction on the
// next seek. Positive = subtitles later.
export const MAX_SUBTITLE_DELAY = 60;
export const SUBTITLE_DELAY_STEP = 0.25;

export function normalizeDelay(d) {
    if (typeof d !== 'number' || !isFinite(d)) return 0;
    const stepped = Math.round(d / SUBTITLE_DELAY_STEP) * SUBTITLE_DELAY_STEP;
    return Math.max(-MAX_SUBTITLE_DELAY, Math.min(MAX_SUBTITLE_DELAY, stepped));
}

export function setTrackDelay(track, delay) {
    if (track) track.__wtDelay = normalizeDelay(delay);
}

export function applyCueOffset(track, offset) {
    if (!track || !track.cues) return;
    const delay = track.__wtDelay || 0;
    for (const cue of track.cues) {
        if (cue.__absStart === undefined) {
            cue.__absStart = cue.startTime;
            cue.__absEnd = cue.endTime;
        }
        const start = cue.__absStart + delay - offset;
        const end = cue.__absEnd + delay - offset;
        if (end <= 0) {
            // Entirely before the session start — park it below zero,
            // where a session's playhead never is. Not at [0,0]: every
            // session starts at media time 0, and Chrome's cue interval
            // test is inclusive at both ends, so a zero-length cue at 0 is
            // active at the first frame — and stays drawn until the next
            // cue boundary, because the renderer only re-evaluates at
            // boundaries and 0 is already behind it. After a seek that put
            // every line from the start of the film up to the seek point
            // on screen at once (reproduced on stage 2026-09-17).
            cue.startTime = PARKED_AT;
            cue.endTime = PARKED_AT;
        } else {
            cue.startTime = Math.max(0, start);
            cue.endTime = end;
        }
    }
}

/**
 * Snapshot the given tracks so they survive an hls.js source reload —
 * hls.js flips element-backed tracks to 'disabled' AND clears their cue
 * lists when it reprocesses the media element on loadSource(). The cue
 * objects themselves stay alive (with their stashed authored times), so
 * re-adding them restores the track without refetching the VTT.
 */
export function captureTrackState(tracks) {
    const saved = [];
    for (const track of tracks) {
        if (!track) continue;
        saved.push({ track, mode: track.mode, cues: track.cues ? [...track.cues] : [] });
    }
    return saved;
}

// restoreCues puts a snapshot's cues back where the list was left empty.
// The one statement of that rule: a seek restores it together with the
// mode (restoreTrackState), a revision swap without (subtitle-track-reload).
export function restoreCues(saved) {
    for (const { track, cues } of saved) {
        if (!track || !cues.length) continue;
        if (track.cues && track.cues.length > 0) continue;
        for (const cue of cues) track.addCue(cue);
    }
}

export function restoreTrackState(saved) {
    for (const entry of saved) {
        entry.track.mode = entry.mode;
        restoreCues([entry]);
    }
}
