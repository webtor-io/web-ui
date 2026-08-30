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

export function applyCueOffset(track, offset) {
    if (!track || !track.cues) return;
    for (const cue of track.cues) {
        if (cue.__absStart === undefined) {
            cue.__absStart = cue.startTime;
            cue.__absEnd = cue.endTime;
        }
        const start = cue.__absStart - offset;
        const end = cue.__absEnd - offset;
        if (end <= 0) {
            // Entirely before the session start — park it where it can
            // never activate (startTime <= t < endTime is always false).
            cue.startTime = 0;
            cue.endTime = 0;
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

export function restoreTrackState(saved) {
    for (const { track, mode, cues } of saved) {
        track.mode = mode;
        if (cues.length && (!track.cues || track.cues.length === 0)) {
            for (const cue of cues) {
                track.addCue(cue);
            }
        }
    }
}
