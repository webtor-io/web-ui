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

// A wait a seek starts on its own has no cap (it had one until 2026-09-18:
// 10 s, then 20 s). One upstream model call is 10-15 s and a cold start
// stacks more than one, so every cap that was tried gave up on real starts
// and played the film without subtitles -- the one outcome the hold exists
// to prevent. What ends it is the run getting ahead, the run dying, or the
// viewer: Keep watching, x, or the play button.
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
    seekWatchMs: 8000,
    seekWatchEveryMs: 1000,
    runMismatchLimit: 5,
    // The silent hold between a seek settling and the first answer about
    // it (see beginPreHold in Player.jsx): how long the film may stand
    // still with nothing said, before it plays regardless.
    seekPreHoldMaxMs: 1500,
    // How long the pill says "caught up" before it goes.
    caughtUpFlashMs: 3000,
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

// shouldBrake answers "is the viewer about to reach a line that is not
// translated yet" (owner, 2026-09-18: better to slow down before the line
// than to rewind after it). pendingFrom is where the first untranslated
// cue starts, so stopping CATCHUP_BRAKE_S before it loses nothing. It is
// asked on `timeupdate` (about four times a second), not on the 3 s tick:
// a tick can find the frontier 2.5 s ahead and the next one find it a
// second behind. The cost of a stale frontier is a pause one HEAD long --
// the caller kicks the poll, and an answer that is ahead ends the wait.
export const CATCHUP_BRAKE_S = 1;
export function shouldBrake(pendingFrom, playhead) {
    if (pendingFrom === null || pendingFrom === undefined) return false;
    if (!usablePlayhead(playhead)) return false;
    return playhead >= pendingFrom - CATCHUP_BRAKE_S;
}

// resumeRewind is how far back a wait that ended puts the film, in seconds.
// waitFrom is the frontier when the wait began: the start of the first line
// the viewer had no subtitle for. A wait that began before that line (the
// brake above) missed nothing and rewinds nothing -- going back would only
// replay what was heard. One that began past it goes back to the line plus
// a lead-in: 2 s is enough to enter the phrase and to absorb a seek landing
// on the keyframe before the target. Capped, because past ten seconds a
// rewind reads as the player having lost its place.
export const RESUME_REWIND_LEAD_S = 2;
export const RESUME_REWIND_MAX_S = 10;
export function resumeRewind(waitFrom, playhead) {
    if (waitFrom === null || waitFrom === undefined || !usablePlayhead(playhead)) return 0;
    const missed = playhead - waitFrom;
    if (!(missed > 0)) return 0;
    return Math.min(missed + RESUME_REWIND_LEAD_S, RESUME_REWIND_MAX_S);
}

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

// viewerFrontier is the frontier as the VIEWER has it, which is not the
// service's (owner, 2026-09-18: "subtitles do not keep up", with no banner
// to say why). The service answers "the next untranslated cue is at F";
// what is on screen is whatever revision the <track> last loaded, and that
// swap is throttled. On a live source the translation runs only a little
// ahead of the viewer, so the usual state was: the service is ahead, the
// banner is silent, and the cues are not in the track.
//
//   loaded       what the last swap brought in: { done, frontier } -- the
//                service's count and frontier at that moment. null before
//                the first swap.
//   coverageEnd  where the cues actually in the <track> end (movie time),
//                or null when that cannot be read. It stands in for
//                loaded.frontier when the swap happened with nothing
//                pending: everything known then was loaded, and whatever
//                the service translated since lies after it.
//
// While the service has nothing the track lacks, the two frontiers are the
// same thing and the service's answer stands.
export function viewerFrontier({ serviceFrontier = null, serviceDone = 0, loaded = null, coverageEnd = null } = {}) {
    const service = serviceFrontier === undefined ? null : serviceFrontier;
    if (!loaded || !((Number(serviceDone) || 0) > (Number(loaded.done) || 0))) return service;
    let mine = loaded.frontier;
    if (mine === null || mine === undefined) mine = coverageEnd;
    if (mine === null || mine === undefined) return service;
    return service === null ? mine : Math.min(service, mine);
}

// needsReload: the service has cues the track lacks, and the viewer is
// about to run out of the ones it has. This is what bypasses the swap
// throttle -- the throttle is there to spare the viewer a flicker, and a
// flicker beats no subtitles.
export function needsReload({ serviceDone = 0, loaded = null, frontier = null, playhead } = {}) {
    if (!loaded || !((Number(serviceDone) || 0) > (Number(loaded.done) || 0))) return false;
    if (frontier === null || frontier === undefined || !usablePlayhead(playhead)) return false;
    return frontier <= playhead + CATCHUP_CLEAR_MARGIN_S;
}

