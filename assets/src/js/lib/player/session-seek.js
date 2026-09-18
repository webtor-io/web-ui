/**
 * Session-based transcoder seeking.
 * Extracted from mediaelement.js lines 191-250.
 *
 * For transcoded streams, seeking POSTs to the transcoder session API,
 * reloads the HLS manifest, and restores audio/subtitle track selections.
 * Seek is quantized to 30-second boundaries.
 */
import { Hls } from './hls-manager';
import { applyCueOffset, captureTrackState, restoreTrackState } from './cue-offset';
import { applySubtitleSelection, readSelection } from './subtitle-apply.js';
import { markUnsnapshottedTracksStale } from './subtitle-track-reload.js';

/**
 * Capture the current video frame onto a canvas positioned over the video.
 * Returns the canvas element (call .remove() to clean up), or null if capture fails.
 */
function captureFrame(videoEl) {
    try {
        const w = videoEl.videoWidth;
        const h = videoEl.videoHeight;
        if (!w || !h) return null;
        const canvas = document.createElement('canvas');
        canvas.width = w;
        canvas.height = h;
        canvas.getContext('2d').drawImage(videoEl, 0, 0, w, h);
        canvas.style.cssText =
            'position:absolute;top:0;left:0;width:100%;height:100%;' +
            'object-fit:contain;pointer-events:none;z-index:1;';
        videoEl.parentNode.insertBefore(canvas, videoEl.nextSibling);
        return canvas;
    } catch {
        return null;
    }
}

export function createSessionSeeker({ hls, videoEl, sessionSeekUrl, sourceUrl, onSeekOffsetChange, onSeekingChange, trackContainer }) {
    let isSeeking = false;
    let seekOffset = 0;
    const isNative = !hls; // native HLS (iOS) — no HLS.js instance
    // What is playing is the picker's answer, read when it is needed rather
    // than snapshotted: the subtitles dialog stays open and clickable
    // during a seek, so a snapshot taken at seek start can name a track the
    // viewer has already moved off.
    const pickerScope = () => trackContainer || document;

    function getSeekOffset() {
        return seekOffset;
    }

    function setIsSeeking(val) {
        isSeeking = val;
        if (onSeekingChange) onSeekingChange(val);
    }

    // `play: true` is for a caller whose film is paused for a reason that
    // the seek itself ends (the resume prompt's hold): the new run has to be
    // started, because the hls.js path below waits for `playing`.
    async function seek(targetTime, { play = false } = {}) {
        if (isSeeking) return;
        setIsSeeking(true);
        let freezeFrame = null;
        // The old run is over the moment the viewer seeks. The frozen frame
        // hides its picture while the POST is out, but nothing hid its
        // sound: it kept playing the old position -- for as long as the
        // transcoder needs to start the new one (owner, 2026-09-18). Paused
        // here, played again once the new source is loading. setIsSeeking
        // comes first on purpose: the player's pause listener reads it and
        // does not take this pause for the viewer's.
        const playAfter = play || !videoEl.paused;

        try {
            // Freeze current frame as overlay to avoid black flash
            freezeFrame = captureFrame(videoEl);
            if (!isNative && !videoEl.paused && typeof videoEl.pause === 'function') videoEl.pause();

            const savedAudioTrack = hls ? hls.audioTrack : -1;
            // hls.js flips element-backed <track>s (user uploads,
            // OpenSubtitles, external) to 'disabled' and clears their cue
            // lists while reprocessing the media element on loadSource() —
            // snapshot mode and cues so the active selection survives.
            const trackEls = [...videoEl.querySelectorAll('track')];
            const savedElementTrackState = captureTrackState(trackEls.map((el) => el.track));
            // Tracks that will be fetched again rather than restored from the
            // snapshot: restoring into one whose refetch is still parsing
            // would leave every snapshot cue on screen twice.
            const refetchedTracks = new Set();

            const separator = sessionSeekUrl.includes('?') ? '&' : '?';
            const res = await fetch(sessionSeekUrl + separator + 't=' + targetTime, { method: 'POST' });
            // A refused seek leaves the transcoder on its old run: moving the
            // offset (and reloading) anyway would shift every side-loaded cue
            // and tell the translation service the player watches a run that
            // does not exist.
            if (res && res.ok === false) throw new Error(`seek POST answered ${res.status}`);

            // The transcoder answers with the run's real start: for a
            // copy-mode video that is the keyframe before the quantized
            // point, up to a GOP earlier than the local guess — the exact
            // difference every side-loaded cue used to run ahead of the
            // sound by after a seek. An old transcoder sends no offset,
            // and the quantized guess stands as before.
            let answered = null;
            if (res && typeof res.json === 'function') {
                // Under a timer: a body that never completes (a proxy
                // holding the connection open) would otherwise park the
                // seek inside this await with isSeeking latched, and every
                // later seek would return immediately — for the rest of
                // the session. The offset is a nicety; the seek is not.
                const body = await Promise.race([
                    res.json().catch(() => null),
                    new Promise((resolve) => setTimeout(() => resolve(null), 3000)),
                ]);
                if (body && typeof body.offset === 'number' && Number.isFinite(body.offset) && body.offset >= 0) {
                    answered = body.offset;
                }
            }
            // The seek is unlocked by `playing`, and `playing` needs a play()
            // that went through. One that did not -- the autoplay policy
            // after the awaits above, or the reload aborting it -- used to be
            // swallowed, and since the seek pauses the old run itself the
            // film then stood still with isSeeking latched: spinner up, every
            // later seek returning at once, for the rest of the session. A
            // rejected play() is tried once more when the new source can
            // play (an abort is cured by that; by then hls.js is also done
            // wiping the tracks, so settling is safe), and if that is
            // refused too the seek settles paused, with the play button.
            let onPlayRejected = () => {};
            const startPlayback = () => {
                if (typeof videoEl.play !== 'function') return;
                const r = videoEl.play();
                if (r && typeof r.catch === 'function') r.catch((err) => onPlayRejected(err));
            };

            seekOffset = answered !== null ? answered : (targetTime > 0 ? Math.floor(targetTime / 30) * 30 : 0);
            if (onSeekOffsetChange) onSeekOffsetChange(seekOffset);

            if (isNative) {
                // Native HLS (iOS): reload source by resetting src
                videoEl.src = sourceUrl;
                videoEl.load();
                startPlayback();
            } else {
                // The snapshot only holds cues of tracks that were on: a
                // disabled track reports none. loadSource empties the rest
                // for good (the browser never refetches a src it loaded),
                // so they are marked to be fetched again when the viewer
                // picks one — see refreshStaleTrack. Here, right before the
                // wipe, and not at snapshot time: a track picked while the
                // POST was in flight would otherwise be refetched first and
                // emptied second.
                // Queried again: a <track> added while the POST was in
                // flight is wiped too, and is in no snapshot.
                for (const el of markUnsnapshottedTracksStale([...videoEl.querySelectorAll('track')], savedElementTrackState)) {
                    refetchedTracks.add(el.track);
                }
                // HLS.js: reload manifest
                hls.stopLoad();
                hls.loadSource(sourceUrl);

                // Restore audio track
                if (savedAudioTrack >= 0) {
                    hls.once(Hls.Events.AUDIO_TRACKS_UPDATED, () => {
                        hls.audioTrack = savedAudioTrack;
                    });
                }

                // Re-apply the subtitle selection. loadSource makes hls.js
                // reprocess the media element: it drops its own track
                // selection and disables our element-backed ones, so
                // whatever the picker says has to be written again. From
                // the chip, not from a snapshot — see pickerScope above.
                hls.once(Hls.Events.SUBTITLE_TRACKS_UPDATED, () => {
                    applySubtitleSelection(videoEl, hls, readSelection(pickerScope()));
                });
                if (playAfter) startPlayback();
            }

            // Unlock seeking and remove freeze frame when playback resumes
            return new Promise((resolve) => {
                let settled = false;
                let retried = false;
                function onCanPlay() {
                    videoEl.removeEventListener('canplay', onCanPlay);
                    if (settled) return;
                    startPlayback();
                }
                onPlayRejected = () => {
                    if (settled) return;
                    if (retried) {
                        onPlaying();
                        return;
                    }
                    retried = true;
                    videoEl.addEventListener('canplay', onCanPlay);
                };
                function onPlaying() {
                    if (settled) return;
                    settled = true;
                    videoEl.removeEventListener('playing', onPlaying);
                    videoEl.removeEventListener('canplay', onCanPlay);
                    if (freezeFrame) freezeFrame.remove();
                    const restorable = savedElementTrackState.filter(({ track }) => !refetchedTracks.has(track));
                    restoreTrackState(restorable);
                    // The Player's seekOffset effect fired while the cue
                    // lists were still empty, so re-shift the restored cues
                    // onto the new session timeline here.
                    for (const { track } of restorable) {
                        applyCueOffset(track, seekOffset);
                    }
                    // restoreTrackState puts back the modes captured at
                    // seek start, which are as stale as the hls.js
                    // selection was: a track chosen mid-seek would be
                    // switched straight back off. The cues are what the
                    // snapshot is for; the modes are the picker's answer,
                    // so they are written last and from the chip.
                    applySubtitleSelection(videoEl, hls, readSelection(pickerScope()));
                    setIsSeeking(false);
                    resolve();
                }
                videoEl.addEventListener('playing', onPlaying);
            });
        } catch (e) {
            console.error('Session seek failed:', e);
            if (freezeFrame) freezeFrame.remove();
            setIsSeeking(false);
            // A refused seek moves nothing, the pause above included.
            if (playAfter && !isNative && videoEl.paused && typeof videoEl.play === 'function') {
                const r = videoEl.play();
                if (r && typeof r.catch === 'function') r.catch(() => {});
            }
        }
    }

    return { seek, getSeekOffset, isSeeking: () => isSeeking };
}
