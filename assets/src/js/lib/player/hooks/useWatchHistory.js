import { useState, useEffect, useRef, useCallback } from 'preact/hooks';

const SAVE_INTERVAL = 15000; // 15 seconds
const MIN_POSITION_CHANGE = 5; // minimum seconds change before sending update
// A position in the first half-minute is not a place to come back to: it is a
// viewer who looked in and left, and "Continue from 0:12?" on their return is
// a question about nothing (owner, 2026-09-20). Not saved, and not offered if
// an older build saved one. Exactly 0 is different -- that is "Start over"
// resetting a real position (forceSendPosition) and always goes through.
export const MIN_SAVED_POSITION = 30;
export const worthSaving = (pos) => pos >= MIN_SAVED_POSITION;

/**
 * Hook for tracking watch position and fetching resume position.
 * - On mount: fetches saved position from server
 * - During playback: sends position updates every 15s
 * - On pause/visibilitychange/beforeunload: sends current position
 * - Returns resumePosition (null until fetched)
 */
// `creditsAtRef`: where the credits begin, once the player knows
// (credits.js). Sent along so the server's "watched" agrees with the moment
// the player offers the next episode -- models.IsWatched.
const withCredits = (payload, creditsAtRef) => {
    const at = creditsAtRef && creditsAtRef.current;
    return typeof at === 'number' && at > 0 ? { ...payload, credits_at: at } : payload;
};

export function useWatchHistory(videoRef, { resourceID, path, currentTime, duration, playing, paused, creditsAtRef }) {
    const [resumePosition, setResumePosition] = useState(null);
    const [resumeReady, setResumeReady] = useState(false);
    const lastSentPositionRef = useRef(0);
    const lastSentTimeRef = useRef(0);
    const currentTimeRef = useRef(0);
    const durationRef = useRef(0);
    const playingRef = useRef(false);
    const pausedRef = useRef(false);

    // Keep refs in sync
    currentTimeRef.current = currentTime;
    durationRef.current = duration;
    playingRef.current = playing;
    pausedRef.current = !!paused;

    // Fetch saved position on mount
    useEffect(() => {
        if (!resourceID || !path) {
            setResumeReady(true);
            return;
        }
        fetch(`/watch/position?resource-id=${encodeURIComponent(resourceID)}&path=${encodeURIComponent(path)}`)
            .then(r => {
                if (r.ok) return r.json();
                return null;
            })
            .then(data => {
                if (data && worthSaving(data.position) && data.duration > 0) {
                    // Don't resume a finished file: 90%, or past the credits
                    // as the server saw them (models.IsWatched).
                    if (!data.watched && data.position / data.duration < 0.9) {
                        setResumePosition(data.position);
                    }
                }
            })
            .catch(() => {})
            .finally(() => setResumeReady(true));
    }, [resourceID, path]);

    // Send position to server
    const sendPosition = useCallback((pos, dur) => {
        if (!resourceID || !path || dur <= 0 || !worthSaving(pos)) return;
        const now = Date.now();
        const posDelta = Math.abs(pos - lastSentPositionRef.current);
        const timeDelta = now - lastSentTimeRef.current;

        // Debounce: skip if position changed < 5s and last sent < 5s ago
        if (posDelta < MIN_POSITION_CHANGE && timeDelta < SAVE_INTERVAL) return;

        lastSentPositionRef.current = pos;
        lastSentTimeRef.current = now;

        const body = JSON.stringify(withCredits({
            resource_id: resourceID,
            path,
            position: pos,
            duration: dur,
        }, creditsAtRef));

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
        if (!resourceID || !path || durationRef.current <= 0 || !worthSaving(currentTimeRef.current)) return;
        const body = JSON.stringify(withCredits({
            resource_id: resourceID,
            path,
            position: currentTimeRef.current,
            duration: durationRef.current,
        }, creditsAtRef));
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
            if (playingRef.current && durationRef.current > 0 && !pausedRef.current) {
                sendPosition(currentTimeRef.current, durationRef.current);
            }
        }, SAVE_INTERVAL);

        return () => clearInterval(interval);
    }, [resourceID, path, sendPosition]);

    // Save on pause
    useEffect(() => {
        if (!playing && duration > 0 && currentTime > 0 && !paused) {
            sendPosition(currentTime, duration);
        }
    }, [playing]);

    // Save on visibility change and beforeunload
    useEffect(() => {
        if (!resourceID || !path) return;

        const onVisibilityChange = () => {
            if (document.hidden && playingRef.current && durationRef.current > 0 && !pausedRef.current) {
                sendPosition(currentTimeRef.current, durationRef.current);
            }
        };

        const onBeforeUnload = () => {
            if (!pausedRef.current) sendBeaconPosition();
        };

        document.addEventListener('visibilitychange', onVisibilityChange);
        window.addEventListener('beforeunload', onBeforeUnload);

        return () => {
            document.removeEventListener('visibilitychange', onVisibilityChange);
            window.removeEventListener('beforeunload', onBeforeUnload);
        };
    }, [resourceID, path, sendPosition, sendBeaconPosition]);

    // Force-send position (bypasses debounce and paused flag) — use after seek
    const forceSendPosition = useCallback((pos, dur) => {
        if (!resourceID || !path || dur <= 0) return;
        lastSentPositionRef.current = pos;
        lastSentTimeRef.current = Date.now();
        const body = JSON.stringify(withCredits({ resource_id: resourceID, path, position: pos, duration: dur }, creditsAtRef));
        fetch('/watch/position', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-TOKEN': window._CSRF || '' },
            body,
            keepalive: true,
        }).catch(() => {});
    }, [resourceID, path]);

    return { resumePosition, resumeReady, forceSendPosition };
}
