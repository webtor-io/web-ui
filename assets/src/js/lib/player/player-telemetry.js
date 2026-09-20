// Usage events for the viewer-facing controls. They shipped without any
// (speed, double-tap seek, subtitle delay, Media Session -- 2026-09-20), and a
// feature nobody can count is a feature nobody can defend or remove.
//
// One event per DECISION, not per press: a viewer walking the delay from 0 to
// +1.5 s presses six times and has made one correction; a streak of taps is
// one seek. settled() holds the last value until the presses stop, and
// flush() sends it at once -- on teardown, so a viewer who fixes the delay
// and leaves is still counted.

export function track(name, data) {
    try {
        if (typeof window !== 'undefined' && window.umami) window.umami.track(name, data);
    } catch (e) { /* analytics never breaks playback */ }
}

export function settled(send, ms, { setTimer = setTimeout, clearTimer = clearTimeout } = {}) {
    let timer = null;
    let pending = null;
    const fire = () => {
        timer = null;
        const v = pending;
        pending = null;
        if (v !== null) send(v);
    };
    return {
        push(value) {
            pending = value;
            if (timer !== null) clearTimer(timer);
            timer = setTimer(fire, ms);
        },
        flush() {
            if (timer !== null) { clearTimer(timer); fire(); }
        },
    };
}
