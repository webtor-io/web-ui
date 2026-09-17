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

    async function seek(targetTime) {
        if (isSeeking) return;
        setIsSeeking(true);

        try {
            // Freeze current frame as overlay to avoid black flash
            const freezeFrame = captureFrame(videoEl);

            const savedAudioTrack = hls ? hls.audioTrack : -1;
            // hls.js flips element-backed <track>s (user uploads,
            // OpenSubtitles, external) to 'disabled' and clears their cue
            // lists while reprocessing the media element on loadSource() —
            // snapshot mode and cues so the active selection survives.
            const trackEls = [...videoEl.querySelectorAll('track')];
            const savedElementTrackState = captureTrackState(trackEls.map((el) => el.track));

            const separator = sessionSeekUrl.includes('?') ? '&' : '?';
            await fetch(sessionSeekUrl + separator + 't=' + targetTime, { method: 'POST' });

            seekOffset = targetTime > 0 ? Math.floor(targetTime / 30) * 30 : 0;
            if (onSeekOffsetChange) onSeekOffsetChange(seekOffset);

            if (isNative) {
                // Native HLS (iOS): reload source by resetting src
                videoEl.src = sourceUrl;
                videoEl.load();
                videoEl.play().catch(() => {});
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
                markUnsnapshottedTracksStale([...videoEl.querySelectorAll('track')], savedElementTrackState);
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
            }

            // Unlock seeking and remove freeze frame when playback resumes
            return new Promise((resolve) => {
                function onPlaying() {
                    videoEl.removeEventListener('playing', onPlaying);
                    if (freezeFrame) freezeFrame.remove();
                    restoreTrackState(savedElementTrackState);
                    // The Player's seekOffset effect fired while the cue
                    // lists were still empty, so re-shift the restored cues
                    // onto the new session timeline here.
                    for (const { track } of savedElementTrackState) {
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
            setIsSeeking(false);
        }
    }

    return { seek, getSeekOffset, isSeeking: () => isSeeking };
}
