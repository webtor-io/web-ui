// What the old route does with a fatal media error nobody else took (a
// guard: passthrough.js, vod-guard.js), since 2026-10-01.
//
// It used to recover every one, however many (hls-manager.js): where the
// browser cannot take the stream at all -- a codec it refuses, a segment it
// cannot decode -- the recovery fails the same way at once, and the player
// recovered every ~110 ms for as long as the page was open, with no word to
// the viewer (7 player-dead why=recovering on nginx-vod in 12 h,
// 2026-09-30). hls.js's own advice for a fatal media error (API.md, "Fatal
// Error Recovery") instead, each step's window counted from the step before:
//   1. recoverMediaError();
//   2. another within MEDIA_RETRY_WINDOW_MS of it: swapAudioCodec() and
//      recoverMediaError() -- an audio codec string that was wrong;
//   3. another within MEDIA_RETRY_WINDOW_MS of that: give up -- stopLoad()
//      and onGiveUp(data), the player's one message (stream-restart.js
//      mediaGiveUp: the card, whose button restarts the stream at the
//      viewer's place).
// An error further than the window from the step before starts at 1 again:
// an occasional failure mid-film is recovered as it always was. Given up,
// every further fatal media error stops loading and tells onGiveUp again --
// something else may have started loading since (the stall watch, a seek
// under the card), and a failure must never go by without the card, which
// the player shows once. A new source (MANIFEST_LOADING: a session seek, a
// restart on the same instance) starts at 1 again: reset().

export const MEDIA_RETRY_WINDOW_MS = 3000;

export function createMediaRecovery(hls, { now = () => Date.now(), onGiveUp = () => {} } = {}) {
    let step = 0; // the last step taken: 0 none, 1 recovered, 2 swapped, 3 given up
    let at = 0; // when it was taken
    const giveUp = (data) => {
        step = 3;
        try { hls.stopLoad(); } catch (e) { /* already stopped */ }
        try { onGiveUp(data); } catch (e) { /* the page goes on */ }
    };
    return {
        get gaveUp() { return step === 3; },
        reset() { step = 0; at = 0; },
        handle(data) {
            const t = now();
            if (step === 3) {
                giveUp(data);
                return;
            }
            if (step === 0 || t - at > MEDIA_RETRY_WINDOW_MS) {
                step = 1;
                at = t;
                hls.recoverMediaError();
                return;
            }
            if (step === 1) {
                step = 2;
                at = t;
                try { hls.swapAudioCodec(); } catch (e) { /* the recovery still goes */ }
                hls.recoverMediaError();
                return;
            }
            giveUp(data);
        },
    };
}
