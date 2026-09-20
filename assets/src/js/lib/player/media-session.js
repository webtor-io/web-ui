// Media Session: what the lock screen, the notification shade, a headset and
// the keyboard's media keys see of the film. Without it they show the page
// title and a play button that does nothing useful, and on a phone -- 43% of
// viewers -- that is the only control left once the screen is off.
//
// Two things are ours to get right and not the browser's:
//   - the timeline. A transcoder session plays a RUN that starts at
//     seekOffset; the element's own currentTime/duration describe the run,
//     not the film. Position state is reported in film time.
//   - seeking. It goes through the player's handleSeek (a session seek is a
//     POST, not a currentTime write), never to the element directly.
//
// navigator.mediaSession and MediaMetadata are injected so this is testable
// and so a browser without them is a no-op rather than a throw.

export const MS_SEEK_STEP = 10;

export function bindMediaSession({ session, Metadata, title, artwork, onPlay, onPause, onSeekTo, getPosition }) {
    if (!session) return { update() {}, destroy() {} };

    try {
        if (Metadata) {
            session.metadata = new Metadata({
                title: title || '',
                artist: 'Webtor',
                artwork: artwork ? [{ src: artwork }] : [],
            });
        }
    } catch (e) { /* a malformed artwork URL must not cost the handlers */ }

    const clampTo = (t) => {
        const { duration } = getPosition();
        return Math.max(0, duration > 0 ? Math.min(duration, t) : t);
    };
    const handlers = {
        play: () => onPlay(),
        pause: () => onPause(),
        seekbackward: (d) => onSeekTo(clampTo(getPosition().currentTime - ((d && d.seekOffset) || MS_SEEK_STEP))),
        seekforward: (d) => onSeekTo(clampTo(getPosition().currentTime + ((d && d.seekOffset) || MS_SEEK_STEP))),
        seekto: (d) => { if (d && typeof d.seekTime === 'number') onSeekTo(clampTo(d.seekTime)); },
    };
    const bound = [];
    for (const [action, fn] of Object.entries(handlers)) {
        // Each on its own: a browser that does not know an action throws for
        // that one and must keep the rest.
        try { session.setActionHandler(action, fn); bound.push(action); } catch (e) { /* unsupported action */ }
    }

    return {
        // update reports the film-time position. Called on the events that
        // change the picture of the timeline (play, pause, seek, rate), not
        // per frame: the browser extrapolates from rate in between.
        update() {
            if (typeof session.setPositionState !== 'function') return;
            const { currentTime, duration, rate } = getPosition();
            if (!(duration > 0) || !isFinite(duration)) return;
            try {
                session.setPositionState({
                    duration,
                    playbackRate: rate > 0 ? rate : 1,
                    position: Math.max(0, Math.min(duration, currentTime)),
                });
            } catch (e) { /* position > duration for a frame, etc. */ }
        },
        destroy() {
            for (const action of bound) {
                try { session.setActionHandler(action, null); } catch (e) { /* gone */ }
            }
            try { session.metadata = null; } catch (e) { /* gone */ }
        },
    };
}
