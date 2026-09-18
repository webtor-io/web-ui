// Is the AI translation behind the viewer, and is it safe to let them go
// again?
//
// A live (embedded-track) translation runs alongside the transcode, so it
// can fall behind the playhead: the film plays on, the cues for what is on
// screen are not written yet, and the viewer sees nothing. The service
// says where its frontier is (`X-Subtitle-Pending-From`, movie time); the
// player knows where the viewer is; these two predicates turn the pair
// into the two answers the banner needs.
//
// Both margins exist because the comparison is noisier than it looks. The
// playhead is `video.currentTime + seekOffset`, and the offset a
// transcoder session reports is the segment boundary it actually started
// on, not the second that was asked for — it shifts by up to ~1.7 s
// between runs. A threshold under 2 s would therefore flip on arithmetic
// rather than on anything that happened.
export const CATCHUP_TRAIL_MARGIN_S = 2;
export const CATCHUP_CLEAR_MARGIN_S = 5;

// catchUpTiming.seekHoldMaxMs bounds the wait a seek starts on its own. A
// seek already costs the viewer a pause while the transcoder restarts, so
// waiting there for the new position's subtitles reads as part of the
// seek — but only for so long: one slow upstream batch must not turn a seek
// into a hang. Past it the film plays and the banner goes back to offering
// Wait, which has no cap because the viewer chose it.
//
// seekWatchMs is the window after a seek settles in which every answer may
// start that hold, asked again every seekWatchEveryMs. A window rather than
// the first answer, because the first ones can predate the new run's cues
// (the transcoder closes a subtitle segment only on the next cue, and the
// service reads it a moment later) and "nothing pending" then means
// "nothing read yet". A silent stretch never shows anything pending, so it
// is never held.
//
// runMismatchLimit: consecutive answers about another run (outside that
// window) after which the player stops naming its run and takes answers at
// their word — see Player.jsx runMismatchRef.
//
// An object rather than constants so the wiring tests can shorten them.
export const catchUpTiming = {
    seekHoldMaxMs: 10000,
    // A file job retargets only at a batch boundary (seconds of upstream
    // work in flight) and then owes at least one more upstream call for the
    // batch at the new position, so a 10 s cap expired with nothing to show
    // almost every time.
    seekHoldMaxMsFile: 20000,
    seekWatchMs: 8000,
    seekWatchEveryMs: 1000,
    runMismatchLimit: 5,
    // The silent hold between a seek settling and the first answer about
    // it (see beginPreHold in Player.jsx): how long the film may stand
    // still with nothing said, before it plays regardless.
    seekPreHoldMaxMs: 1500,
};

// A NaN playhead is a <video> with no timeline yet (no metadata, no
// source). It is not "at 0": answering the questions against it would make
// the player pause a film that has not started, which is the one outcome
// this feature must never produce. Both predicates therefore read it as
// "nothing to worry about" rather than as a position.
function usablePlayhead(playhead) {
    return typeof playhead === 'number' && Number.isFinite(playhead);
}

// trailing answers "is the translation behind where the viewer is (or is
// about to be)".
//
// The two margins are not one threshold with a rounding error: between
// them sits a band where neither answer is wrong, and inside it the
// previous answer is kept. Without that, a run hovering around the
// playhead — which is exactly what a healthy live translation does — would
// show and hide the banner every three seconds.
export function trailing(prev, pendingFrom, playhead) {
    if (pendingFrom === null || pendingFrom === undefined) return false;
    if (!usablePlayhead(playhead)) return false;
    if (pendingFrom <= playhead + CATCHUP_TRAIL_MARGIN_S) return true;
    if (pendingFrom >= playhead + CATCHUP_CLEAR_MARGIN_S) return false;
    return Boolean(prev);
}

// caughtUp answers "is it safe to resume a viewer who chose to wait".
//
// Not the negation of trailing(): resuming needs the clear margin on its
// own, without the hysteresis band, because the band is there to stop a
// banner flickering and this decides whether to start playback. The
// absent header means the service has nothing pending ahead — the
// strongest form of caught up, and the answer for every older service too,
// which is what stops a wait from becoming permanent when the header is
// not coming.
export function caughtUp(pendingFrom, playhead) {
    if (pendingFrom === null || pendingFrom === undefined) return true;
    if (!usablePlayhead(playhead)) return true;
    return pendingFrom >= playhead + CATCHUP_CLEAR_MARGIN_S;
}

// remaining is how many cues the banner says are left. `total` is a
// snapshot on a live source, so this is an estimate and the copy says
// "~"; the clamp is what keeps a total that has not caught up with `done`
// from rendering as a negative count.
// nothingCountedYet: the run has answered, and has not counted a single
// cue -- `0/0`, the service's "registered nothing yet". No frontier comes
// with such an answer (there is nothing to stand one on), so trailing()
// and caughtUp() both read it as "not behind". That is right for a run the
// player merely found in that state, and wrong for one the viewer has just
// started: there, nothing counted means nothing translated, at the very
// spot they are watching. Only the player knows which it is, so this is a
// separate question and the caller combines them.
export function nothingCountedYet(p) {
    if (!p || p.final) return false;
    return (Number(p.total) || 0) === 0 && (Number(p.done) || 0) === 0;
}

export function remaining(p) {
    if (!p) return 0;
    const total = Number(p.total) || 0;
    const done = Number(p.done) || 0;
    return Math.max(0, total - done);
}
