import { useEffect, useRef } from 'preact/hooks';
import { createHls, initDefaultTracks, Hls } from '../hls-manager';

/**
 * Hook that manages HLS.js lifecycle.
 * Attaches to videoRef, returns hlsRef for external access (track control, etc.).
 *
 * passthrough: { fragLoadMs, guard } for a stream the transcoder passes
 * through (Player.jsx; passthrough.js). audioGuard: the guard of a start on
 * any other route that declared multichannel audio (createAudioGuard). A
 * guard is told which path plays (setHls: the instance, or null for native
 * HLS) and sees hls.js's errors first; Player.jsx disposes of it.
 */
export function useHls(videoRef, sourceUrl, { onReady, passthrough = null, audioGuard = null } = {}) {
    const hlsRef = useRef(null);
    const tracksInitialized = useRef(false);

    useEffect(() => {
        const video = videoRef.current;
        if (!video || !sourceUrl) return;

        // Check if source is HLS
        const isHls = sourceUrl.includes('.m3u8') || sourceUrl.includes('mpegurl');

        if (isHls) {
            const guard = passthrough ? passthrough.guard : audioGuard;
            const hls = createHls(video, sourceUrl, () => {
                if (onReady) onReady(hls);
            }, passthrough ? { passthrough: true, fragLoadMs: passthrough.fragLoadMs, guard: passthrough.guard }
                : audioGuard ? { guard: audioGuard } : {});
            if (guard) guard.setHls(hls);

            if (hls) {
                hlsRef.current = hls;
                window.hlsPlayer = hls;

                // Init default tracks on first canplay
                const onCanPlay = () => {
                    if (!tracksInitialized.current && hls) {
                        tracksInitialized.current = true;
                        initDefaultTracks(hls, video);
                    }
                };
                video.addEventListener('canplay', onCanPlay);

                // Lower quality during seek
                const onSeeking = () => {
                    if (hls.loadLevel > 1) hls.loadLevel = 1;
                };
                const onSeeked = () => {
                    hls.loadLevel = -1;
                };
                video.addEventListener('seeking', onSeeking);
                video.addEventListener('seeked', onSeeked);

                return () => {
                    video.removeEventListener('canplay', onCanPlay);
                    video.removeEventListener('seeking', onSeeking);
                    video.removeEventListener('seeked', onSeeked);
                    hls.stopLoad();
                    hls.destroy();
                    hlsRef.current = null;
                    window.hlsPlayer = null;
                };
            } else {
                // Native HLS (Safari) — source already set in createHls
                return;
            }
        } else {
            // Non-HLS source (direct file)
            video.src = sourceUrl;
        }
    }, [videoRef, sourceUrl]);

    return hlsRef;
}
