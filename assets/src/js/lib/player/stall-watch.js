// Whether the film is moving is a fact about the clock, not about events.
// After a seek past the buffer `waiting` may never come, and `seeked` /
// `canplay` come too early: they fire when the first fragment is in, which is
// well before the picture moves again. The viewer got a frozen frame with no
// spinner, then a spinner that gave up while the frame was still frozen
// (owner, 2026-09-20).
//
// So the player samples currentTime every frame and asks this: not paused,
// not ended, and the clock has not changed for stallMs -> stalled, until it
// changes again. Pure of the DOM and of time, see stall-watch.test.js.

// Getting OUT of a stall asks for more than the clock: while hls.js sits in a
// buffer hole it nudges currentTime forward in small jumps, and the first
// version took each nudge for "moving" -- the spinner went out with the frame
// still frozen (owner, 2026-09-20, second report). So recovery needs
// evidence of playback, not of a changed number:
//   - with a frame counter (video, getVideoPlaybackQuality): RECOVER_FRAMES
//     frames presented since the stall began. One is not enough -- a finished
//     seek paints its target frame and then stands;
//   - without one (audio, old browsers): the clock advancing on every sample
//     for RECOVER_MS. A nudge is a single jump; playback is continuous.

// Longer than any frame gap at 0.5x, shorter than a viewer's patience.
export const STALL_MS = 300;
export const RECOVER_FRAMES = 3;
export const RECOVER_MS = 250;

export function createStallWatch({ stallMs = STALL_MS, recoverFrames = RECOVER_FRAMES, recoverMs = RECOVER_MS } = {}) {
    let lastTime = null;
    let lastAdvanceAt = 0;
    let stalled = false;
    let framesAtStall = null;
    let runStartedAt = null; // start of the current unbroken run of advancing samples

    const reset = (currentTime, now) => {
        lastTime = currentTime;
        lastAdvanceAt = now;
        runStartedAt = null;
    };

    return {
        isStalled: () => stalled,
        // sample returns 'stalled' / 'moving' on a CHANGE of verdict and null
        // otherwise, so the caller sets state twice per stall, not per frame.
        // `frames` is the presented-frame counter, or undefined if there is none.
        sample(currentTime, now, { idle = false, frames } = {}) {
            // Paused, ended, or a session seek in flight: nothing is expected
            // to move, and the wait must not count towards the next stall.
            // Going idle ends a stall quietly -- nothing moved, and the
            // caller's pause/end handling owns the spinner then.
            if (idle || lastTime === null) {
                reset(currentTime, now);
                stalled = false;
                return null;
            }
            const advanced = currentTime !== lastTime;
            if (!stalled) {
                if (advanced) { reset(currentTime, now); return null; }
                if (now - lastAdvanceAt > stallMs) {
                    stalled = true;
                    framesAtStall = typeof frames === 'number' ? frames : null;
                    runStartedAt = null;
                    return 'stalled';
                }
                return null;
            }
            // Stalled: look for playback, not for a changed number.
            if (advanced) {
                lastTime = currentTime;
                lastAdvanceAt = now;
                if (runStartedAt === null) runStartedAt = now;
            } else {
                runStartedAt = null;
            }
            const byFrames = framesAtStall !== null && typeof frames === 'number';
            const recovered = byFrames
                ? frames - framesAtStall >= recoverFrames
                : runStartedAt !== null && now - runStartedAt >= recoverMs;
            if (!recovered) return null;
            stalled = false;
            reset(currentTime, now);
            return 'moving';
        },
    };
}
