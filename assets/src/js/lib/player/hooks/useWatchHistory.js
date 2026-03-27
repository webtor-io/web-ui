import { useState, useEffect, useRef, useCallback } from 'preact/hooks';

const SAVE_INTERVAL = 15000; // 15 seconds
const MIN_POSITION_CHANGE = 5; // minimum seconds change before sending update

/**
 * Hook for tracking watch position and fetching resume position.
 * - On mount: fetches saved position from server
 * - During playback: sends position updates every 15s
 * - On pause/visibilitychange/beforeunload: sends current position
 * - Returns resumePosition (null until fetched)
 */
export function useWatchHistory(videoRef, { resourceID, path, currentTime, duration, playing }) {
    const [resumePosition, setResumePosition] = useState(null);
    const lastSentPositionRef = useRef(0);
    const lastSentTimeRef = useRef(0);
    const currentTimeRef = useRef(0);
    const durationRef = useRef(0);
    const playingRef = useRef(false);

    // Keep refs in sync
    currentTimeRef.current = currentTime;
    durationRef.current = duration;
    playingRef.current = playing;

    // Fetch saved position on mount
    useEffect(() => {
        if (!resourceID || !path) return;
        fetch(`/watch/position?resource-id=${encodeURIComponent(resourceID)}&path=${encodeURIComponent(path)}`)
            .then(r => {
                if (r.ok) return r.json();
                return null;
            })
            .then(data => {
                if (data && data.position > 0 && data.duration > 0) {
                    // Don't resume if nearly finished (>= 90%)
                    if (data.position / data.duration < 0.9) {
                        setResumePosition(data.position);
                    }
                }
            })
            .catch(() => {});
    }, [resourceID, path]);

    // Send position to server
    const sendPosition = useCallback((pos, dur) => {
        if (!resourceID || !path || dur <= 0) return;
        const now = Date.now();
        const posDelta = Math.abs(pos - lastSentPositionRef.current);
        const timeDelta = now - lastSentTimeRef.current;

        // Debounce: skip if position changed < 5s and last sent < 5s ago
        if (posDelta < MIN_POSITION_CHANGE && timeDelta < SAVE_INTERVAL) return;

        lastSentPositionRef.current = pos;
        lastSentTimeRef.current = now;

        const body = JSON.stringify({
            resource_id: resourceID,
            path,
            position: pos,
            duration: dur,
        });

        fetch('/watch/position', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-TOKEN': window._CSRF || '',
            },
            body,
            keepalive: true,
        }).catch(() => {});
    }, [resourceID, path]);

    // Send position via sendBeacon (for beforeunload)
    const sendBeaconPosition = useCallback(() => {
        if (!resourceID || !path || durationRef.current <= 0) return;
        const body = JSON.stringify({
            resource_id: resourceID,
            path,
            position: currentTimeRef.current,
            duration: durationRef.current,
        });
        try {
            navigator.sendBeacon('/watch/position', new Blob([body], { type: 'application/json' }));
        } catch (e) {
            // sendBeacon not available — ignore
        }
    }, [resourceID, path]);

    // Periodic save during playback
    useEffect(() => {
        if (!resourceID || !path) return;

        const interval = setInterval(() => {
            if (playingRef.current && durationRef.current > 0) {
                sendPosition(currentTimeRef.current, durationRef.current);
            }
        }, SAVE_INTERVAL);

        return () => clearInterval(interval);
    }, [resourceID, path, sendPosition]);

    // Save on pause
    useEffect(() => {
        if (!playing && duration > 0 && currentTime > 0) {
            sendPosition(currentTime, duration);
        }
    }, [playing]);

    // Save on visibility change and beforeunload
    useEffect(() => {
        if (!resourceID || !path) return;

        const onVisibilityChange = () => {
            if (document.hidden && playingRef.current && durationRef.current > 0) {
                sendPosition(currentTimeRef.current, durationRef.current);
            }
        };

        const onBeforeUnload = () => {
            sendBeaconPosition();
        };

        document.addEventListener('visibilitychange', onVisibilityChange);
        window.addEventListener('beforeunload', onBeforeUnload);

        return () => {
            document.removeEventListener('visibilitychange', onVisibilityChange);
            window.removeEventListener('beforeunload', onBeforeUnload);
        };
    }, [resourceID, path, sendPosition, sendBeaconPosition]);

    return resumePosition;
}
