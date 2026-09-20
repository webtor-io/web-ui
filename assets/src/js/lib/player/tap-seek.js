// Double-tap to seek, for the 43% of viewers on a phone (2026-09-19) who have
// no arrow keys and only a hair-thin progress bar.
//
// A tap on the picture toggles play. Two quick taps on the same HALF of it
// seek instead, and every further tap in the streak seeks again (the habit
// every phone player has taught). The catch is the first tap: it cannot know
// it is the first of two, and letting it pause the film only for the second
// to resume it makes a stutter -- and a pair of play/pause events for
// everything listening. So a tap WAITS for the window to pass before it
// counts as a single.
//
// Halves, not "outer thirds with a centre that toggles at once": that was the
// first version, and a double tap lands near the middle -- on a narrow phone
// almost always -- so both taps went to play/pause and nothing seeked (owner,
// 2026-09-20). The only tap with nothing to wait for is one on a player whose
// width is unknown.
//
// Pure of the DOM and of time: the caller passes the tap's x, the width, and
// a clock; timers are injected. See tap-seek.test.js.

export const TAP_WINDOW_MS = 300;

export function zoneOf(x, width) {
    if (!(width > 0)) return 'centre';
    return x < width / 2 ? 'left' : 'right';
}

export function createTapSeek({ onSingle, onSeek, windowMs = TAP_WINDOW_MS,
    setTimer = setTimeout, clearTimer = clearTimeout } = {}) {
    let pending = null;   // timer for a side tap that may yet be a single
    let lastZone = null;
    let lastAt = -Infinity;
    let streak = 0;       // seeks made in the current run of taps

    const cancelPending = () => {
        if (pending !== null) { clearTimer(pending); pending = null; }
    };
    // A waiting single that turned out NOT to be half of a double was a real
    // tap: it is delivered now, late, rather than dropped -- two taps are two
    // toggles whichever sides they landed on.
    const flushPending = () => {
        if (pending === null) return;
        cancelPending();
        onSingle();
    };

    return {
        tap(x, width, now) {
            const zone = zoneOf(x, width);
            const quick = now - lastAt <= windowMs && zone === lastZone;
            lastAt = now;
            lastZone = zone;
            if (zone === 'centre') {
                flushPending();
                streak = 0;
                onSingle();
                return 'single';
            }
            if (quick) {
                cancelPending();
                streak += 1;
                onSeek(zone === 'left' ? -1 : 1, streak);
                return 'seek';
            }
            // First tap on a side: a single, unless a second one arrives.
            flushPending();
            streak = 0;
            pending = setTimer(() => { pending = null; onSingle(); }, windowMs);
            return 'wait';
        },
        cancel() {
            cancelPending();
            lastZone = null;
            lastAt = -Infinity;
            streak = 0;
        },
    };
}
