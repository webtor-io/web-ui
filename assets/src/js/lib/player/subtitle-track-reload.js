// Revision swapping for a <track> that is still being written.
//
// An AI translation is produced cue by cue, so the player refetches the
// partial .vtt as lines arrive (withRev bumps ?rev=). Re-parsing drops the
// cue list and flips the track mode, so both are snapshotted before the
// swap and put back once the new revision has loaded, or if it fails to
// load at all (cue-offset.js). The session cue-offset (mid-movie resume)
// re-applies through the capture 'load' listener the player installs
// separately.
//
// Extracted from Player.jsx so the listener lifetime below is testable:
// Player.jsx is JSX and `node --test` cannot parse it.
import { captureTrackState, restoreTrackState } from './cue-offset.js';

// The reload waiting to settle on each <track>, so its listeners can be
// taken off before the next one goes on. A pair per revision, left to
// accumulate, is both a leak and a correctness bug: on a failure the
// oldest handler still runs and restores a snapshot from several
// revisions ago, so the viewer's subtitles jump backwards.
const pending = new WeakMap();

function clearPending(el) {
    const prev = pending.get(el);
    if (!prev) return;
    el.removeEventListener('load', prev.onLoad);
    el.removeEventListener('error', prev.onError);
    pending.delete(el);
}

// reloadSubtitleTrack swaps in a newer revision of a partially written
// VTT and reports whether it actually did anything — the caller throttles
// reloads, and a no-op must not spend the throttle window.
export function reloadSubtitleTrack(video, id, nextSrc, onError) {
    if (!video || !id || !nextSrc) return false;
    let el = null;
    for (const t of video.querySelectorAll('track')) {
        if (t.id === id) { el = t; break; }
    }
    if (!el || el.getAttribute('src') === nextSrc) return false;

    clearPending(el);
    const saved = captureTrackState([el.track]);
    const handlers = {
        onLoad: () => {
            restoreTrackState(saved);
            clearPending(el);
        },
        onError: () => {
            // Put back the cues the viewer already had before saying
            // anything: a broken revision must not leave a blank screen.
            restoreTrackState(saved);
            clearPending(el);
            if (onError) onError();
        },
    };
    pending.set(el, handlers);
    el.addEventListener('load', handlers.onLoad);
    el.addEventListener('error', handlers.onError);
    el.setAttribute('src', nextSrc);
    return true;
}
