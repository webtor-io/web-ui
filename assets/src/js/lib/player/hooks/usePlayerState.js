import { useState, useEffect, useCallback, useRef } from 'preact/hooks';
import { loadPrefs, savePrefs, RATES } from '../player-prefs';
import { createStallWatch } from '../stall-watch';


/**
 * Core player state hook.
 * Manages play/pause, currentTime, duration, volume, muted, fullscreen, loading.
 */
export function usePlayerState(videoRef, containerRef, { duration: serverDuration, seekOffset, seeking, initialTime = 0, mediaState }) {
    const [playing, setPlaying] = useState(false);
    const [currentTime, writeCurrentTime] = useState(initialTime);
    const restoringTo = useRef(initialTime > 0 ? initialTime : null);
    // Hold the old timeline through mounting and a quantized session seek.
    // A viewer's own seek takes over immediately; the resume seek keeps it.
    const setCurrentTime = useCallback((time, { restore = false } = {}) => {
        restoringTo.current = restore ? time : null;
        writeCurrentTime(time);
    }, []);
    const [duration, setDuration] = useState(serverDuration > 0 ? serverDuration : 0);
    const [volume, setVolumeState] = useState(1);
    const [muted, setMutedState] = useState(false);
    const [rate, setRateState] = useState(1);
    const [fullscreen, setFullscreen] = useState(() => !!(document.fullscreenElement || document.webkitFullscreenElement));
    const [loading, setLoading] = useState(true);
    const [buffered, setBuffered] = useState(0);

    const rafRef = useRef(null);
    const stalledRef = useRef(false);
    const offsetRef = useRef(seekOffset);
    offsetRef.current = seekOffset;
    const seekingRef = useRef(seeking);
    seekingRef.current = seeking;

    // Time update loop via requestAnimationFrame for smooth progress
    // Skips updates when seekingRef is true so setCurrentTime(target) sticks.
    useEffect(() => {
        const video = videoRef.current;
        if (!video) return;

        // The stall watchdog (stall-watch.js): the spinner follows the clock,
        // not the media events.
        const stallWatch = createStallWatch();
        function tick() {
            if (video) {
                const idle = video.paused || video.ended || seekingRef.current;
                // Presented frames where the browser counts them (video only).
                let frames;
                // videoWidth: a <video> playing an audio-only stream counts no
                // frames at all, and would never be seen to recover.
                if (video.videoWidth > 0 && typeof video.getVideoPlaybackQuality === 'function') {
                    const q = video.getVideoPlaybackQuality();
                    if (q && typeof q.totalVideoFrames === 'number') frames = q.totalVideoFrames - (q.droppedVideoFrames || 0);
                }
                const verdict = stallWatch.sample(video.currentTime || 0, performance.now(), { idle, frames });
                stalledRef.current = stallWatch.isStalled();
                if (verdict === 'stalled') setLoading(true);
                else if (verdict === 'moving') setLoading(false);
            }
            if (!seekingRef.current && video && !video.paused) {
                const rawTime = video.currentTime || 0;
                const time = seekOffset + rawTime;
                if (restoringTo.current === null || time >= restoringTo.current - 0.5) {
                    restoringTo.current = null;
                    writeCurrentTime(time);
                }
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

        const onSeeked = () => {
            const time = offsetRef.current + (video.currentTime || 0);
            if (restoringTo.current !== null && Math.abs(time - restoringTo.current) < 0.5) {
                restoringTo.current = null;
                writeCurrentTime(time);
            }
        };
        const onPlay = () => setPlaying(true);
        const onPause = () => setPlaying(false);
        // `stalledRef` is the watchdog's verdict (see the tick above): while
        // the clock is not moving, an optimistic event must not take the
        // spinner down -- `canplay` and `seeked` arrive when the first
        // fragment is in, which is well before the picture moves again.
        const onWaiting = () => setLoading(true);
        const onCanPlay = () => { if (!stalledRef.current) setLoading(false); };
        const onPlaying = () => { if (!stalledRef.current) setLoading(false); };
        const onLoadedMetadata = () => {
            if (serverDuration <= 0 && video.duration && isFinite(video.duration)) {
                setDuration(video.duration);
            }
        };
        // Restored before the listeners go on, so the restore itself is not
        // written straight back. defaultPlaybackRate as well: a session seek
        // reloads the source, and load() resets playbackRate to the default.
        // `muted` is restored only towards silence: a stream the page started
        // muted (autoplay policy) must not be unmuted by a stored "false".
        const prefs = mediaState || loadPrefs();
        video.volume = prefs.volume;
        if (mediaState) video.muted = prefs.muted;
        else if (prefs.muted) video.muted = true;
        video.defaultPlaybackRate = prefs.rate;
        video.playbackRate = prefs.rate;

        const onVolumeChange = () => {
            setVolumeState(video.volume);
            setMutedState(video.muted);
            savePrefs({ volume: video.volume, muted: video.muted });
        };
        const onRateChange = () => {
            setRateState(video.playbackRate);
            // Only the viewer's own choices: a rate off the scale is somebody
            // else's (a browser extension, the reset on load()).
            if (RATES.includes(video.playbackRate)) savePrefs({ rate: video.playbackRate });
        };
        const onEnded = () => setPlaying(false);

        video.addEventListener('seeked', onSeeked);
        video.addEventListener('play', onPlay);
        video.addEventListener('pause', onPause);
        video.addEventListener('waiting', onWaiting);
        video.addEventListener('canplay', onCanPlay);
        video.addEventListener('playing', onPlaying);
        video.addEventListener('loadedmetadata', onLoadedMetadata);
        video.addEventListener('volumechange', onVolumeChange);
        video.addEventListener('ratechange', onRateChange);
        video.addEventListener('ended', onEnded);

        // Init from current state
        setVolumeState(video.volume);
        setMutedState(video.muted);
        setRateState(video.playbackRate);

        return () => {
            video.removeEventListener('seeked', onSeeked);
            video.removeEventListener('play', onPlay);
            video.removeEventListener('pause', onPause);
            video.removeEventListener('waiting', onWaiting);
            video.removeEventListener('canplay', onCanPlay);
            video.removeEventListener('playing', onPlaying);
            video.removeEventListener('loadedmetadata', onLoadedMetadata);
            video.removeEventListener('volumechange', onVolumeChange);
        video.removeEventListener('ratechange', onRateChange);
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
            // Native HLS on iOS may not be loaded yet — load first if needed.
            // Never under hls.js: load() drops the element's MediaSource (its
            // blob: source) and the player is dead -- the first tap before
            // any data, on an iPhone that plays through hls.js (`?mms=on`),
            // or in Chrome.
            const hls = window.hlsPlayer;
            if (video.readyState === 0 && !(hls && hls.media === video)) {
                video.load();
            }
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

    const setRate = useCallback((val) => {
        const video = videoRef.current;
        if (!video || !RATES.includes(val)) return;
        video.defaultPlaybackRate = val;
        video.playbackRate = val;
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
            const exit = document.exitFullscreen || document.webkitExitFullscreen;
            if (exit) exit.call(document);
        } else {
            // On the stage, not on the player's own container: the stage
            // survives a move to the next file, and fullscreen with it
            // (Player.jsx initPlayer).
            const host = (container.closest && container.closest('.wt-player-stage')) || container;
            const enter = host.requestFullscreen || host.webkitRequestFullscreen;
            if (enter) {
                enter.call(host);
            } else {
                // iOS: native video fullscreen (only option)
                const video = container.querySelector('video');
                if (video && video.webkitEnterFullscreen) video.webkitEnterFullscreen();
            }
        }
    }, [containerRef]);

    return {
        playing, currentTime, duration, volume, muted, rate, fullscreen, loading, buffered,
        togglePlay, seekTo, setVolume, setRate, toggleMute, toggleFullscreen,
        setDuration, setCurrentTime,
    };
}
