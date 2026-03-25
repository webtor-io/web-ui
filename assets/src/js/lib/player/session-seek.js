/**
 * Session-based transcoder seeking.
 * Extracted from mediaelement.js lines 191-250.
 *
 * For transcoded streams, seeking POSTs to the transcoder session API,
 * reloads the HLS manifest, and restores audio/subtitle track selections.
 * Seek is quantized to 30-second boundaries.
 */
import { Hls } from './hls-manager';

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

export function createSessionSeeker({ hls, videoEl, sessionSeekUrl, sourceUrl, onSeekOffsetChange, onSeekingChange }) {
    let isSeeking = false;
    let seekOffset = 0;
    const isNative = !hls; // native HLS (iOS) — no HLS.js instance

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
            const savedSubtitleTrack = hls ? hls.subtitleTrack : -1;
            const savedSubtitleDisplay = hls ? hls.subtitleDisplay : false;

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
                // HLS.js: reload manifest
                hls.stopLoad();
                hls.loadSource(sourceUrl);

                // Restore audio track
                if (savedAudioTrack >= 0) {
                    hls.once(Hls.Events.AUDIO_TRACKS_UPDATED, () => {
                        hls.audioTrack = savedAudioTrack;
                    });
                }

                // Restore subtitle track
                if (savedSubtitleDisplay && savedSubtitleTrack >= 0) {
                    hls.once(Hls.Events.SUBTITLE_TRACKS_UPDATED, () => {
                        hls.subtitleDisplay = true;
                        hls.subtitleTrack = savedSubtitleTrack;
                    });
                }
            }

            // Unlock seeking and remove freeze frame when playback resumes
            return new Promise((resolve) => {
                function onPlaying() {
                    videoEl.removeEventListener('playing', onPlaying);
                    if (freezeFrame) freezeFrame.remove();
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
