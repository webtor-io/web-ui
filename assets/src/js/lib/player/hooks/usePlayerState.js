import { useState, useEffect, useCallback, useRef } from 'preact/hooks';

/**
 * Core player state hook.
 * Manages play/pause, currentTime, duration, volume, muted, fullscreen, loading.
 */
export function usePlayerState(videoRef, containerRef, { duration: serverDuration, seekOffset, seeking }) {
    const [playing, setPlaying] = useState(false);
    const [currentTime, setCurrentTime] = useState(0);
    const [duration, setDuration] = useState(serverDuration > 0 ? serverDuration : 0);
    const [volume, setVolumeState] = useState(1);
    const [muted, setMutedState] = useState(false);
    const [fullscreen, setFullscreen] = useState(false);
    const [loading, setLoading] = useState(true);
    const [buffered, setBuffered] = useState(0);

    const rafRef = useRef(null);
    const seekingRef = useRef(seeking);
    seekingRef.current = seeking;

    // Time update loop via requestAnimationFrame for smooth progress
    // Skips updates when seekingRef is true so setCurrentTime(target) sticks.
    useEffect(() => {
        const video = videoRef.current;
        if (!video) return;

        function tick() {
            if (!seekingRef.current && video && !video.paused) {
                const rawTime = video.currentTime || 0;
                setCurrentTime(seekOffset + rawTime);
                if (video.buffered && video.buffered.length > 0) {
                    setBuffered(seekOffset + video.buffered.end(video.buffered.length - 1));
                }
            }
            rafRef.current = requestAnimationFrame(tick);
        }
        rafRef.current = requestAnimationFrame(tick);

        return () => {
            if (rafRef.current) cancelAnimationFrame(rafRef.current);
        };
    }, [videoRef, seekOffset]);

    // Media event listeners
    useEffect(() => {
        const video = videoRef.current;
        if (!video) return;

        const onPlay = () => setPlaying(true);
        const onPause = () => setPlaying(false);
        const onWaiting = () => setLoading(true);
        const onCanPlay = () => setLoading(false);
        const onPlaying = () => setLoading(false);
        const onLoadedMetadata = () => {
            if (serverDuration <= 0 && video.duration && isFinite(video.duration)) {
                setDuration(video.duration);
            }
        };
        const onVolumeChange = () => {
            setVolumeState(video.volume);
            setMutedState(video.muted);
        };
        const onEnded = () => setPlaying(false);

        video.addEventListener('play', onPlay);
        video.addEventListener('pause', onPause);
        video.addEventListener('waiting', onWaiting);
        video.addEventListener('canplay', onCanPlay);
        video.addEventListener('playing', onPlaying);
        video.addEventListener('loadedmetadata', onLoadedMetadata);
        video.addEventListener('volumechange', onVolumeChange);
        video.addEventListener('ended', onEnded);

        // Init from current state
        setVolumeState(video.volume);
        setMutedState(video.muted);

        return () => {
            video.removeEventListener('play', onPlay);
            video.removeEventListener('pause', onPause);
            video.removeEventListener('waiting', onWaiting);
            video.removeEventListener('canplay', onCanPlay);
            video.removeEventListener('playing', onPlaying);
            video.removeEventListener('loadedmetadata', onLoadedMetadata);
            video.removeEventListener('volumechange', onVolumeChange);
            video.removeEventListener('ended', onEnded);
        };
    }, [videoRef, serverDuration]);

    // Fullscreen change listener
    useEffect(() => {
        const onChange = () => {
            const el = document.fullscreenElement || document.webkitFullscreenElement;
            setFullscreen(!!el);
        };
        document.addEventListener('fullscreenchange', onChange);
        document.addEventListener('webkitfullscreenchange', onChange);
        return () => {
            document.removeEventListener('fullscreenchange', onChange);
            document.removeEventListener('webkitfullscreenchange', onChange);
        };
    }, []);

    const togglePlay = useCallback(() => {
        const video = videoRef.current;
        if (!video) return;
        if (video.paused) {
            video.play().catch(() => {});
        } else {
            video.pause();
        }
    }, [videoRef]);

    const seekTo = useCallback((time) => {
        const video = videoRef.current;
        if (!video) return;
        video.currentTime = time;
    }, [videoRef]);

    const setVolume = useCallback((val) => {
        const video = videoRef.current;
        if (!video) return;
        video.volume = Math.max(0, Math.min(1, val));
        if (val > 0) video.muted = false;
    }, [videoRef]);

    const toggleMute = useCallback(() => {
        const video = videoRef.current;
        if (!video) return;
        video.muted = !video.muted;
    }, [videoRef]);

    const toggleFullscreen = useCallback(() => {
        const container = containerRef.current;
        if (!container) return;
        if (document.fullscreenElement || document.webkitFullscreenElement) {
            (document.exitFullscreen || document.webkitExitFullscreen).call(document);
        } else {
            (container.requestFullscreen || container.webkitRequestFullscreen).call(container);
        }
    }, [containerRef]);

    return {
        playing, currentTime, duration, volume, muted, fullscreen, loading, buffered,
        togglePlay, seekTo, setVolume, toggleMute, toggleFullscreen,
        setDuration, setCurrentTime,
    };
}
