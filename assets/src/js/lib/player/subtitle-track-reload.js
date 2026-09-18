// Revision swapping for a <track> that is still being written.
//
// An AI translation is produced cue by cue, so the player refetches the
// partial .vtt as lines arrive (withRev bumps ?rev=). Re-parsing drops the
// cue list, so the cues are snapshotted before the swap and put back if the
// new revision comes up empty or fails to load at all. The session
// cue-offset (mid-movie resume) re-applies through the capture 'load'
// listener the player installs separately.
//
// The track MODE is deliberately not part of that snapshot any more
// (2026-09-18). It used to be, and the snapshot could be taken at the worst
// moment: a session seek's hls.loadSource() disables every element-backed
// track, a progress tick inside that window swapped the revision with
// mode = 'disabled' on record, the seek settled and the picker's selection
// switched the track back on -- and then the revision's `load` "restored"
// the snapshot, switching the track off with its fresh cues in it. Nothing
// re-asserts after a <track> load (the player's re-assertion listens to
// hls.js events only), so the viewer had a running translation, a banner
// saying it had caught up, and no subtitles until something re-applied the
// selection -- pause -> play, as it happened. The mode that is right after
// a load is the picker's answer NOW, which this module does not know:
// `onSettled` hands the moment to the caller, who does.
//
// Extracted from Player.jsx so the listener lifetime below is testable:
// Player.jsx is JSX and `node --test` cannot parse it.
import { captureTrackState } from './cue-offset.js';

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

// restoreCues puts the snapshot's cues back where the new revision left the
// list empty: restoreTrackState without the mode -- see the header.
function restoreCues(saved) {
    for (const { track, cues } of saved) {
        if (!track || !cues.length) continue;
        if (track.cues && track.cues.length > 0) continue;
        for (const cue of cues) track.addCue(cue);
    }
}

// reloadSubtitleTrack swaps in a newer revision of a partially written
// VTT and reports whether it actually did anything — the caller throttles
// reloads, and a no-op must not spend the throttle window.
export function reloadSubtitleTrack(video, id, nextSrc, onError, onSettled) {
    if (!video || !id || !nextSrc) return false;
    let el = null;
    for (const t of video.querySelectorAll('track')) {
        if (t.id === id) { el = t; break; }
    }
    // Compared without the refresh parameter: a track refetched after a seek
    // (refreshStaleTrack) is the same revision, and swapping it again would
    // download the file twice and spend the caller's reload throttle.
    if (!el || withoutRefresh(el.getAttribute('src')) === withoutRefresh(nextSrc)) return false;

    clearPending(el);
    const saved = captureTrackState([el.track]);
    const handlers = {
        onLoad: () => {
            restoreCues(saved);
            clearPending(el);
            if (onSettled) onSettled();
        },
        onError: () => {
            // Put back the cues the viewer already had before saying
            // anything: a broken revision must not leave a blank screen.
            restoreCues(saved);
            clearPending(el);
            if (onError) onError();
            if (onSettled) onSettled();
        },
    };
    pending.set(el, handlers);
    el.addEventListener('load', handlers.onLoad);
    el.addEventListener('error', handlers.onError);
    el.setAttribute('src', nextSrc);
    return true;
}

// dropDeletedTracks removes the <track> elements the picker has no chip for
// any more and reports whether the one that was showing was among them.
//
// Deleting an upload replaces the contents of #my-subtitles, so its chip
// goes; the <track> does not — it lives in <video>, which the async swap
// never touches. The subtitles of a file the viewer just deleted therefore
// kept playing until a reload, with no chip marked and a blank "Now:".
//
// `chipIDs` must be every chip in the dialog, not just the uploads: the
// preloaded OpenSubtitles, sidecar and embedded tracks are <track> elements
// too, and measuring orphanhood against the uploads alone would delete the
// track the viewer is actually watching. An id is the whole test — chips
// and tracks are rendered from one list (getSubtitles), and "Off" has no
// <track> to orphan.
export function dropDeletedTracks(video, chipIDs) {
    if (!video || !video.querySelectorAll) return '';
    const chips = new Set(chipIDs || []);
    let showing = '';
    for (const el of Array.from(video.querySelectorAll('track'))) {
        if (!el.id || chips.has(el.id)) continue;
        if (el.track && el.track.mode === 'showing') showing = el.id;
        clearPending(el);
        if (el.remove) el.remove();
    }
    return showing;
}

// ---- tracks a session seek emptied ------------------------------------
//
// A session seek calls hls.loadSource(), and hls.js answers it by clearing
// the cues of EVERY text track on the media element
// (TimelineController._cleanTracks on MANIFEST_LOADING) — ours as well as
// its own. The seeker snapshots cues first and puts them back once playback
// resumes, but a disabled track reports `cues === null`, so the snapshot
// only ever holds the track that was on screen. Every other <track> stays
// loaded, empty, and is never fetched again: the browser does not reload a
// src it has already loaded. Picking one of them after a seek showed
// nothing at all (reproduced on stage 2026-09-17: an OpenSubtitles track at
// readyState 2, mode showing, 0 cues — 353 after a forced reload).
//
// Refetching on activation, rather than snapshotting the disabled tracks
// too, because reading a disabled track's cues means flipping it to
// 'hidden' and back: a textTracks `change` event per track for hls.js to
// react to, and a first download for every track that was never loaded.
// Only the track the viewer actually picks is worth a request.

// HTMLTrackElement.LOADING / LOADED, spelled out: the tests have no DOM.
const TRACK_LOADING = 1;
const TRACK_LOADED = 2;
const REFRESH_PARAM = 'wt-rf';

// element -> { loadingAtWipe }. A track that was still loading when
// loadSource emptied it lost the cues parsed so far and gets only the rest,
// so it can be short while not empty: it is refetched regardless of its
// cue count.
const stale = new WeakMap();
let refreshSeq = 0;

// markUnsnapshottedTracksStale marks the <track> elements whose cues the
// seek snapshot does not hold — the ones loadSource is about to empty for
// good — and returns them. `saved` is captureTrackState's output.
export function markUnsnapshottedTracksStale(elements, saved) {
    const covered = new Set();
    for (const s of saved || []) {
        if (s && s.track && s.cues && s.cues.length) covered.add(s.track);
    }
    const marked = [];
    for (const el of elements || []) {
        if (!el) continue;
        const loading = el.readyState === TRACK_LOADING;
        // A track still loading is marked even when the snapshot holds some
        // of its cues: the loader adds only the rest after the wipe, the
        // list is then not empty, and the snapshot is never put back.
        if (covered.has(el.track) && !loading) continue;
        stale.set(el, { loadingAtWipe: loading });
        marked.push(el);
    }
    return marked;
}

function withoutRefresh(src) {
    if (src === null || src === undefined) return src;
    const re = new RegExp(`([?&])${REFRESH_PARAM}=\\d+(&|$)`);
    return String(src).replace(re, (m, lead, tail) => (tail ? lead : '')).replace(/[?&]$/, '');
}

function withRefresh(src) {
    refreshSeq++;
    const base = withoutRefresh(src);
    return `${base}${base.includes('?') ? '&' : '?'}${REFRESH_PARAM}=${refreshSeq}`;
}

function refetch(el) {
    const src = el.getAttribute('src');
    if (!src) return false;
    el.setAttribute('src', withRefresh(src));
    return true;
}

// refreshStaleTrack refetches a track a seek emptied, and reports whether it
// did. Called when the track is switched on; the mark is spent on the first
// look either way, so a file that is legitimately empty costs one request,
// not one per re-assertion of the selection.
export function refreshStaleTrack(el) {
    const mark = el ? stale.get(el) : undefined;
    if (!mark) return false;
    // A revision swap in flight is already the refetch.
    if (pending.has(el)) {
        stale.delete(el);
        return false;
    }
    if (mark.loadingAtWipe && el.readyState === TRACK_LOADING) {
        // Still on the load the wipe cut into: refetch when it lands. The
        // mark stays until then, so a second activation does not add a
        // second listener's worth of work.
        if (!mark.waiting) {
            mark.waiting = true;
            const done = () => {
                el.removeEventListener('load', onLoad);
                el.removeEventListener('error', onError);
            };
            const onLoad = () => {
                done();
                if (stale.get(el) !== mark) return;
                stale.delete(el);
                refetch(el);
            };
            // A load that fails ends the wait too; left attached, the
            // listener would refetch on some later, unrelated load.
            const onError = () => {
                done();
                if (stale.get(el) === mark) stale.delete(el);
            };
            el.addEventListener('load', onLoad);
            el.addEventListener('error', onError);
        }
        return false;
    }
    stale.delete(el);
    // Never loaded: switching it on starts its first load, which brings the
    // cues by itself.
    if (el.readyState !== TRACK_LOADED) return false;
    const cues = el.track && el.track.cues;
    if (!mark.loadingAtWipe && cues && cues.length > 0) return false;
    return refetch(el);
}
