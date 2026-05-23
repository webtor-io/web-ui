import { render } from 'preact';
import { useRef, useState, useEffect, useCallback } from 'preact/hooks';
import { usePlayerState } from './hooks/usePlayerState';
import { useHls } from './hooks/useHls';
import { useWatchHistory } from './hooks/useWatchHistory';
import { createSessionSeeker } from './session-seek';
import { Controls } from './Controls';
import { LoadingSpinner } from './icons';
import { init as initI18n, t, tf } from './i18n';
import { shareResource } from '../share/share';
import '../../../styles/player.css';

let _currentPlayer = null;

/**
 * Main Player Preact component.
 * Wraps <video>/<audio>, renders custom controls, manages HLS + session seeking.
 */
function PlayerComponent({ videoEl, settings, containerEl, showControls, fixedSize }) {
    const containerRef = useRef(containerEl);
    const videoRef = useRef(videoEl);
    const [seekOffset, setSeekOffset] = useState(0);
    const [sessionSeeking, setSessionSeeking] = useState(false);
    const sessionSeekingRef = useRef(false);
    const [controlsVisible, setControlsVisible] = useState(true);
    const hideTimerRef = useRef(null);
    const sessionSeekerRef = useRef(null);

    const isVideo = videoEl.tagName === 'VIDEO';
    const duration = videoEl.getAttribute('data-duration') ? parseFloat(videoEl.getAttribute('data-duration')) : -1;
    const sessionId = videoEl.dataset.sessionId;
    const sessionSeekUrl = videoEl.dataset.sessionSeekUrl;
    const sessionDeletePath = videoEl.dataset.sessionDeletePath;
    const isSession = !!sessionId;
    const graceDurationSec = videoEl.dataset.graceDurationSec ? parseInt(videoEl.dataset.graceDurationSec, 10) : 0;
    const graceShownRef = useRef(false);
    const [streamStarted, setStreamStarted] = useState(false);
    const streamStartFiredRef = useRef(false);
    const poster = videoEl.getAttribute('poster');
    const resourceID = videoEl.dataset.resourceId;
    const path = videoEl.dataset.path;

    // Source URL from first <source> element
    const sourceEl = videoEl.querySelector('source');
    const sourceUrl = sourceEl ? sourceEl.getAttribute('src') : videoEl.src;

    // Parse features from settings
    const features = parseFeatures(settings, isVideo, duration, isSession);

    // Fetch initial seek offset from transcoder session
    useEffect(() => {
        if (!isSession || !sessionSeekUrl) return;
        fetch(sessionSeekUrl)
            .then(r => r.json())
            .then(data => {
                if (data.offset > 0) setSeekOffset(data.offset);
            })
            .catch(() => {}); // ignore — offset stays 0
    }, []);

    // Sync ref with state for use in closures that don't re-bind
    const setSessionSeekingWithRef = useCallback((val) => {
        sessionSeekingRef.current = val;
        setSessionSeeking(val);
    }, []);

    // Player state hook
    const state = usePlayerState(videoRef, containerRef, { duration, seekOffset, seeking: sessionSeeking });

    // HLS hook
    const hlsRef = useHls(videoRef, sourceUrl);

    // Resume prompt state — must be declared before useWatchHistory which reads it.
    const [showResumePrompt, setShowResumePrompt] = useState(false);

    // Watch history hook (position tracking + resume).
    // `paused` prevents overwriting saved position while resume prompt is open.
    const { resumePosition, resumeReady, forceSendPosition } = useWatchHistory(videoRef, {
        resourceID, path,
        currentTime: state.currentTime,
        duration: state.duration,
        playing: state.playing,
        paused: showResumePrompt,
    });

    // Seek handler (session or direct)
    const handleSeek = useCallback((time) => {
        if (sessionSeekingRef.current) return;
        if (isSession && sessionSeekUrl) {
            // Immediately show target position on timeline
            state.setCurrentTime(time);
            // Lazily create session seeker (works with HLS.js or native HLS)
            if (!sessionSeekerRef.current) {
                sessionSeekerRef.current = createSessionSeeker({
                    hls: hlsRef.current, // null for native HLS (iOS)
                    videoEl,
                    sessionSeekUrl,
                    sourceUrl,
                    onSeekOffsetChange: setSeekOffset,
                    onSeekingChange: setSessionSeekingWithRef,
                });
            }
            if (sessionSeekerRef.current) {
                sessionSeekerRef.current.seek(time);
            }
        } else {
            const video = videoRef.current;
            if (video) {
                const maxTime = video.duration && isFinite(video.duration) ? video.duration : time;
                video.currentTime = Math.min(time, maxTime);
            }
        }
    }, [isSession, sessionSeekUrl, sourceUrl]);

    // Auto-hide controls
    const resetHideTimer = useCallback(() => {
        setControlsVisible(true);
        if (hideTimerRef.current) clearTimeout(hideTimerRef.current);
        if (isVideo) {
            hideTimerRef.current = setTimeout(() => {
                if (!videoRef.current?.paused) setControlsVisible(false);
            }, 3000);
        }
    }, [isVideo]);

    useEffect(() => {
        resetHideTimer();
        return () => { if (hideTimerRef.current) clearTimeout(hideTimerRef.current); };
    }, []);

    // Show controls on pause
    useEffect(() => {
        if (!state.playing) setControlsVisible(true);
    }, [state.playing]);

    // Stream-start Umami event — fires once per player session when playback
    // actually advances past 5s (real engagement, not just press-play-and-bounce).
    // Gates the in-player share button (Controls reads `streamStarted` prop) and
    // is the canonical denominator for share-rate analysis (share-resource events
    // divided by stream-start sessions).
    useEffect(() => {
        if (streamStartFiredRef.current) return;
        if (state.currentTime < 5) return;
        streamStartFiredRef.current = true;
        setStreamStarted(true);
        if (window.umami) window.umami.track('stream-start', {
            isVideo,
            isSession,
            resourceID: resourceID || '',
        });
    }, [state.currentTime, isVideo, isSession, resourceID]);

    // Grace soft CTA — fires once when movie-time crosses the grace window.
    // The popup is server-rendered by the action template (stream_video.html)
    // as a sibling of the player container, NOT inside containerRef — so we
    // search globally. CTA is a per-page singleton (one player per action page).
    // If the user is in fullscreen, exit first — the CTA lives outside the
    // fullscreen element and would be invisible otherwise.
    useEffect(() => {
        if (!graceDurationSec || graceShownRef.current) return;
        if (state.currentTime < graceDurationSec) return;
        const el = document.querySelector('#grace-cta');
        if (!el) return;
        graceShownRef.current = true;
        if (document.fullscreenElement) {
            document.exitFullscreen().catch(() => {});
        }
        el.classList.remove('hidden');
        if (window.umami) window.umami.track('grace-soft-cta-shown');
        const hide = (action) => {
            el.classList.add('hidden');
            if (window.umami) window.umami.track('grace-soft-cta-click', { action });
        };
        const closeBtn = el.querySelector('.grace-cta-close');
        if (closeBtn) closeBtn.addEventListener('click', () => hide('dismiss'), { once: true });
        const contBtn = el.querySelector('.grace-cta-continue');
        if (contBtn) contBtn.addEventListener('click', () => hide('continue'), { once: true });
    }, [state.currentTime, graceDurationSec]);

    // Keyboard shortcuts
    useEffect(() => {
        function onKeyDown(e) {
            if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
            if (sessionSeeking) return;
            switch (e.key) {
                case ' ':
                case 'k':
                    e.preventDefault();
                    state.togglePlay();
                    resetHideTimer();
                    break;
                case 'ArrowLeft':
                    e.preventDefault();
                    handleSeek(Math.max(0, state.currentTime - 15));
                    resetHideTimer();
                    break;
                case 'ArrowRight':
                    e.preventDefault();
                    handleSeek(Math.min(state.duration, state.currentTime + 15));
                    resetHideTimer();
                    break;
                case 'ArrowUp':
                    e.preventDefault();
                    state.setVolume(Math.min(1, state.volume + 0.1));
                    resetHideTimer();
                    break;
                case 'ArrowDown':
                    e.preventDefault();
                    state.setVolume(Math.max(0, state.volume - 0.1));
                    resetHideTimer();
                    break;
                case 'f':
                    e.preventDefault();
                    state.toggleFullscreen();
                    resetHideTimer();
                    break;
                case 'm':
                    e.preventDefault();
                    state.toggleMute();
                    resetHideTimer();
                    break;
            }
        }
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, [state.currentTime, state.duration, state.volume, state.playing, sessionSeeking]);

    // player_play / player_paused custom events
    useEffect(() => {
        let forcePaused = false;
        function onPlayerPaused() {
            videoRef.current?.pause();
            forcePaused = true;
        }
        function onPlayerPlay() {
            forcePaused = false;
            videoRef.current?.play().catch(() => {});
        }
        function onPlaying() {
            if (forcePaused) videoRef.current?.pause();
        }
        window.addEventListener('player_paused', onPlayerPaused);
        window.addEventListener('player_play', onPlayerPlay);
        videoEl.addEventListener('playing', onPlaying);
        return () => {
            window.removeEventListener('player_paused', onPlayerPaused);
            window.removeEventListener('player_play', onPlayerPlay);
            videoEl.removeEventListener('playing', onPlaying);
        };
    }, []);

    // Dispatch player_ready on canplay + set aspect-ratio from video
    useEffect(() => {
        let dispatched = false;
        function onCanPlay() {
            if (!dispatched) {
                dispatched = true;
                // Native HLS (iOS) starts at live edge — force start from beginning
                if (!hlsRef.current && videoEl.currentTime > 1) {
                    videoEl.currentTime = 0;
                }
                // Set container aspect-ratio from actual video dimensions
                if (videoEl.videoWidth && videoEl.videoHeight) {
                    containerEl.style.aspectRatio = `${videoEl.videoWidth} / ${videoEl.videoHeight}`;
                }
                window.dispatchEvent(new CustomEvent('player_ready'));
            }
        }
        videoEl.addEventListener('canplay', onCanPlay);
        return () => videoEl.removeEventListener('canplay', onCanPlay);
    }, []);

    // Once resume check completes: show resume prompt if there's a saved position
    useEffect(() => {
        if (!resumeReady) return;
        if (resumePosition && resumePosition > 0) {
            setShowResumePrompt(true);
        }
    }, [resumeReady]);

    // Handle resume choice
    const handleResume = useCallback(() => {
        setShowResumePrompt(false);
        const video = videoRef.current;
        if (!video) return;
        if (isSession && sessionSeekUrl) {
            handleSeek(resumePosition);
        } else {
            video.currentTime = resumePosition;
        }
        // Save resumed position immediately
        const dur = duration > 0 ? duration : (video.duration || 0);
        if (dur > 0) forceSendPosition(resumePosition, dur);
    }, [resumePosition, isSession, sessionSeekUrl, handleSeek, duration, forceSendPosition]);

    const handleStartOver = useCallback(() => {
        setShowResumePrompt(false);
        // Save position 0 immediately
        const video = videoRef.current;
        const dur = duration > 0 ? duration : (video?.duration || 0);
        if (dur > 0) forceSendPosition(0, dur);
    }, [duration, forceSendPosition]);

    // Chromecast integration
    useEffect(() => {
        if (!features.chromecast) return;
        let castBtn = null;
        function initCast() {
            if (!window.cast || !window.chrome?.cast) return;
            const ctx = cast.framework.CastContext.getInstance();
            ctx.setOptions({
                receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
                autoJoinPolicy: chrome.cast.AutoJoinPolicy.PAGE_SCOPED,
                androidReceiverCompatible: true,
            });
            // Show cast button
            castBtn = document.createElement('div');
            castBtn.className = 'wt-player-cast-button';
            castBtn.innerHTML = '<google-cast-launcher></google-cast-launcher>';
            containerEl.appendChild(castBtn);
        }
        if (window.cast) {
            initCast();
        } else {
            window.__onGCastApiAvailable = (available) => { if (available) initCast(); };
            const s = document.createElement('script');
            s.src = 'https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1';
            document.body.appendChild(s);
        }
        return () => { if (castBtn) castBtn.remove(); };
    }, [features.chromecast, isVideo]);

    // Session cleanup removed — sessions have server-side TTL

    // Captions modal toggle — use original (non-cloned) checkbox outside player
    const handleCaptionsClick = useCallback(() => {
        const checkbox = document.getElementById('subtitles-checkbox');
        if (checkbox) checkbox.checked = !checkbox.checked;
    }, []);

    // Embed modal toggle
    const handleEmbedClick = useCallback(() => {
        const checkbox = document.getElementById('embed-checkbox');
        if (checkbox) checkbox.checked = !checkbox.checked;
    }, []);

    // In-player share click — same handler as the header button (see
    // assets/src/js/lib/share/share.js), tagged `location:'player'` so we
    // can A/B which placement converts better.
    const handleShareClick = useCallback(() => {
        shareResource({ location: 'player' });
    }, []);

    // Click on video to toggle play (video only).
    // Use ref for showResumePrompt to avoid re-registering native DOM listeners.
    const showResumePromptRef = useRef(false);
    showResumePromptRef.current = showResumePrompt;

    const handleVideoClick = useCallback((e) => {
        if (!isVideo || sessionSeekingRef.current || showResumePromptRef.current) return;
        if (e.target.closest('.wt-player-controls')) return;
        if (e.target.closest('.wt-resume-prompt')) return;
        state.togglePlay();
        resetHideTimer();
    }, [isVideo, state.togglePlay]);

    // Double-click for fullscreen
    const handleDoubleClick = useCallback((e) => {
        if (!isVideo) return;
        if (e.target.closest('.wt-player-controls')) return;
        state.toggleFullscreen();
    }, [isVideo, state.toggleFullscreen]);

    // Apply classes to the container element (managed outside Preact)
    useEffect(() => {
        const el = containerEl;
        if (!el) return;
        el.className = `wt-player ${isVideo ? 'wt-player--video' : 'wt-player--audio'}${fixedSize ? ' wt-player--fixed' : ''}`;

        const onMove = () => resetHideTimer();
        const onTouch = () => resetHideTimer();
        const onClick = (e) => handleVideoClick(e);
        const onDblClick = (e) => handleDoubleClick(e);
        el.addEventListener('mousemove', onMove);
        el.addEventListener('touchstart', onTouch);
        el.addEventListener('click', onClick);
        el.addEventListener('dblclick', onDblClick);

        return () => {
            el.removeEventListener('mousemove', onMove);
            el.removeEventListener('touchstart', onTouch);
            el.removeEventListener('click', onClick);
            el.removeEventListener('dblclick', onDblClick);
        };
    }, [containerEl, isVideo]);

    // Update dynamic classes
    useEffect(() => {
        const el = containerEl;
        if (!el) return;
        el.classList.toggle('wt-player--fullscreen', state.fullscreen);
        el.classList.toggle('wt-player--controls-visible', controlsVisible);
    }, [state.fullscreen, controlsVisible, containerEl]);

    return (
        <>
            {/* Resume prompt — ask user to continue or start over */}
            {showResumePrompt && isVideo && (
                <div class="wt-player-overlay wt-resume-prompt" style="background:rgba(0,0,0,0.75);z-index:50;display:flex;align-items:center;justify-content:center"
                     onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
                    <div style="display:flex;flex-direction:column;gap:10px;align-items:center">
                        <button type="button" onClick={(e) => { e.stopPropagation(); handleResume(); }}
                            class="wt-resume-btn wt-resume-btn--primary">
                            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" width="16" height="16"><path fill-rule="evenodd" d="M4.5 5.653c0-1.427 1.529-2.33 2.779-1.643l11.54 6.347c1.295.712 1.295 2.573 0 3.286L7.28 19.99c-1.25.687-2.779-.217-2.779-1.643V5.653Z" clip-rule="evenodd" /></svg>
                            {tf('player.continueFrom', formatTime(resumePosition))}
                        </button>
                        <button type="button" onClick={(e) => { e.stopPropagation(); handleStartOver(); }}
                            class="wt-resume-btn wt-resume-btn--ghost">
                            {t('player.startOver')}
                        </button>
                    </div>
                </div>
            )}

            {/* Loading spinner (only when playing + buffering, or seeking) */}
            {showControls && isVideo && (sessionSeeking || (state.playing && state.loading)) && (
                <div class="wt-player-overlay wt-player-overlay--loading">
                    <LoadingSpinner />
                </div>
            )}

            {/* Big play button — shown when paused, regardless of loading state */}
            {showControls && isVideo && !state.playing && !sessionSeeking && !showResumePrompt && (
                <div class="wt-player-overlay wt-player-overlay--play" onDblClick={(e) => e.stopPropagation()}>
                    <button type="button" class="wt-player-big-play" onClick={(e) => { e.stopPropagation(); state.togglePlay(); }} aria-label={t('player.play')}>
                        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="w-16 h-16">
                            <path fill-rule="evenodd" d="M4.5 5.653c0-1.427 1.529-2.33 2.779-1.643l11.54 6.347c1.295.712 1.295 2.573 0 3.286L7.28 19.99c-1.25.687-2.779-.217-2.779-1.643V5.653Z" clip-rule="evenodd" />
                        </svg>
                    </button>
                </div>
            )}

            {/* Controls */}
            {showControls && (
                <Controls
                    playing={state.playing}
                    currentTime={state.currentTime}
                    duration={state.duration}
                    volume={state.volume}
                    muted={state.muted}
                    fullscreen={state.fullscreen}
                    buffered={state.buffered}
                    seeking={sessionSeeking}
                    onTogglePlay={state.togglePlay}
                    onSeek={handleSeek}
                    onVolumeChange={state.setVolume}
                    onToggleMute={state.toggleMute}
                    onToggleFullscreen={state.toggleFullscreen}
                    onCaptionsClick={handleCaptionsClick}
                    onEmbedClick={handleEmbedClick}
                    onShareClick={handleShareClick}
                    streamStarted={streamStarted}
                    isVideo={isVideo}
                    features={features}
                />
            )}
        </>
    );
}

function formatTime(seconds) {
    const s = Math.floor(seconds);
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`;
    return `${m}:${String(sec).padStart(2, '0')}`;
}

function parseFeatures(settings, isVideo, duration, isSession) {
    const defaults = {
        playpause: true,
        progress: true,
        duration: true,
        volume: true,
        advancedtracks: isVideo,
        fullscreen: isVideo,
        chromecast: isVideo,
        embed: isVideo,
        // In-player share button — same handler as the header button, fires
        // share-resource Umami with location:'player'. Visible only after
        // stream-start (gated in Controls). Embed contexts can opt out via
        // settings.features.share=false.
        share: true,
        availableprogress: duration > 0 && !isSession,
        logo: !!(window._domainSettings && window._domainSettings.ads === true),
    };
    if (settings && settings.features) {
        for (const name in settings.features) {
            defaults[name] = settings.features[name];
        }
    }
    return defaults;
}

/**
 * Initialize player on a target container that contains a <video> or <audio> with class="player".
 * Called from action/stream.js.
 */
export async function initPlayer(target) {
    const videoEl = target.querySelector('.player');
    if (!videoEl) return;

    // Load player translations before rendering (sync t() reads from cached instance)
    await initI18n();

    let settings = {};
    if (videoEl.dataset.settings) {
        settings = JSON.parse(videoEl.dataset.settings);
    }

    // Check if controls should be shown (from HTML controls attribute)
    const showControls = videoEl.hasAttribute('controls');

    // Remove native controls — we render our own (or none)
    videoEl.removeAttribute('controls');

    // Transfer fixed dimensions from video to container
    const fixedWidth = videoEl.getAttribute('width');
    const fixedHeight = videoEl.getAttribute('height');
    if (fixedWidth) videoEl.removeAttribute('width');
    if (fixedHeight) videoEl.removeAttribute('height');

    // Wrap the video in a player container div
    const mountEl = document.createElement('div');
    mountEl.className = 'wt-player-mount';
    if (fixedWidth) mountEl.style.width = fixedWidth;
    if (fixedHeight) mountEl.style.height = fixedHeight;
    videoEl.parentNode.insertBefore(mountEl, videoEl);

    // Build the player container with video inside
    const playerContainer = document.createElement('div');
    mountEl.appendChild(playerContainer);
    playerContainer.appendChild(videoEl);

    // Wire track handlers on original modals (stay outside player, no overflow issues)
    wireTrackHandlers(target);

    // Wire embed copy button
    wireEmbedCopy(target);

    // Wire logo (ads download overlay) — only when ads enabled
    let showLogo = !!(window._domainSettings && window._domainSettings.ads === true);
    if (settings.features && settings.features.logo !== undefined) {
        showLogo = settings.features.logo;
    }
    if (showLogo) wireLogo(target, mountEl);

    // Render Preact controls into the player container (after video)
    render(
        <PlayerComponent videoEl={videoEl} settings={settings} containerEl={playerContainer} showControls={showControls} fixedSize={!!(fixedWidth || fixedHeight)} />,
        playerContainer
    );

    _currentPlayer = { mountEl, playerContainer, videoEl };
}

// Ensure a <track> with id=<trackID> exists inside <video>. The server
// pre-wraps user-subtitle URLs through /ext/ with the correct auth
// (subdomain/path/query baked in by torrent-http-proxy) and returns
// them in data-src. For subs uploaded after the initial render, the
// <video> has no matching <track> yet — create one on first click.
function ensureUserSubtitleTrack(video, trackID, wrappedSrc, label) {
    for (const t of video.querySelectorAll('track')) {
        if (t.id === trackID) return true;
    }
    if (!wrappedSrc) return false;
    const track = document.createElement('track');
    track.id = trackID;
    track.kind = 'subtitles';
    track.src = wrappedSrc;
    track.label = label || 'Subtitle';
    video.appendChild(track);
    return true;
}

function wireTrackHandlers(container) {
    // Delegate subtitle clicks on #subtitles so items swapped into
    // #my-subtitles via async still work without re-binding.
    const subtitlesModal = container.querySelector('#subtitles');
    if (subtitlesModal) {
        subtitlesModal.addEventListener('click', (e) => {
            const target = e.target.closest('.subtitle');
            if (!target || !subtitlesModal.contains(target)) return;
            const provider = target.getAttribute('data-provider');
            const id = target.getAttribute('data-id');
            // User subs uploaded after the initial page render have no
            // matching <track> yet — create it on first click so the
            // textTracks mode='showing' loop below finds something to
            // activate.
            if (provider === 'UserSubtitle' && id && id !== 'none') {
                const video = container.querySelector('video.player');
                if (video) {
                    ensureUserSubtitleTrack(
                        video,
                        id,
                        target.getAttribute('data-src') || '',
                        target.getAttribute('data-label') || target.textContent.trim(),
                    );
                }
                if (window.umami) window.umami.track('user-subtitle-select');
            }
            markTrack(container, target, 'subtitle');
            const hls = window.hlsPlayer;
            const mpId = target.getAttribute('data-mp-id');

            if (hls && provider === 'MediaProbe') {
                hls.subtitleDisplay = true;
                hls.subtitleTrack = parseInt(mpId);
                for (const p of document.querySelectorAll('video.player')) {
                    for (const t of p.textTracks) {
                        if (t.id) t.mode = 'hidden';
                    }
                }
            } else {
                if (hls) {
                    hls.subtitleTrack = -1;
                    hls.subtitleDisplay = false;
                }
                for (const p of document.querySelectorAll('video.player, audio.player')) {
                    for (const t of p.textTracks) {
                        t.mode = (id && id !== 'none' && t.id === id) ? 'showing' : 'hidden';
                    }
                }
            }
        });
    }

    // Audio click handlers (no async swap — direct binding is enough)
    for (const audio of container.querySelectorAll('.audio')) {
        audio.addEventListener('click', (e) => {
            const target = e.target;
            markTrack(container, target, 'audio');
            if (window.hlsPlayer && target.getAttribute('data-provider') === 'MediaProbe') {
                window.hlsPlayer.audioTrack = parseInt(target.getAttribute('data-mp-id'));
            }
        });
    }

    // Sync the visual "active" marker on .subtitle items in #my-subtitles:
    // the <track default> in <video> already drives playback correctly on
    // reload, but the list items never go through the click path and so
    // lose their text-primary/underline marker. Re-derive it from the
    // currently-showing (or default) <track> and re-run on async swap.
    const syncMySubtitleMark = () => {
        const video = container.querySelector('video.player');
        const mySubs = container.querySelector('#my-subtitles');
        if (!video || !mySubs) return;
        let activeID = null;
        for (const t of video.textTracks) {
            if (t.mode === 'showing' && t.id) { activeID = t.id; break; }
        }
        if (!activeID) {
            const dt = video.querySelector('track[default]');
            if (dt && dt.id) activeID = dt.id;
        }
        if (!activeID) return;
        for (const item of mySubs.querySelectorAll('.subtitle')) {
            const isActive = item.getAttribute('data-id') === activeID;
            item.classList.toggle('text-primary', isActive);
            item.classList.toggle('underline', isActive);
            if (isActive) item.setAttribute('data-default', 'true');
            else item.removeAttribute('data-default');
        }
    };
    syncMySubtitleMark();
    // loadAsyncView dispatches an 'async' CustomEvent after swapping a
    // target's innerHTML; re-sync when #my-subtitles content is replaced.
    const mySubsContainer = container.querySelector('#my-subtitles');
    if (mySubsContainer) {
        window.addEventListener('async', (e) => {
            if (e.detail && e.detail.target === mySubsContainer) syncMySubtitleMark();
        });
    }

    // Subtitles modal view toggles — OpenSubtitles and My Subtitles are
    // alternate views inside the same modal alongside #embedded. Clicking a
    // toggle shows its view and hides every other; clicking the active one
    // returns to #embedded.
    const embedded = container.querySelector('#embedded');
    const viewIDs = ['opensubtitles', 'my-subtitles'];
    const views = viewIDs
        .map((id) => ({ id, el: container.querySelector('#' + id) }))
        .filter((v) => v.el);
    for (const v of views) {
        const toggle = container.querySelector(`label[for=${v.id}]`);
        if (!toggle) continue;
        toggle.addEventListener('click', (e) => {
            const isHidden = v.el.classList.contains('hidden');
            if (isHidden) {
                if (embedded) embedded.classList.add('hidden');
                for (const other of views) {
                    if (other.id === v.id) continue;
                    other.el.classList.add('hidden');
                }
                v.el.classList.remove('hidden');
                e.target.classList.remove('btn-outline');
            } else {
                v.el.classList.add('hidden');
                if (embedded) embedded.classList.remove('hidden');
                e.target.classList.add('btn-outline');
            }
        });
    }
}

function markTrack(container, el, type) {
    if (el.getAttribute('data-default') === 'true') return;
    el.classList.add('text-primary', 'underline');
    el.setAttribute('data-default', 'true');

    const s = container.querySelector('#subtitles');
    if (!s) return;
    const es = s.querySelectorAll(`.${type}`);
    for (const ee of es) {
        if (ee === el) continue;
        ee.classList.remove('text-primary', 'underline');
        ee.removeAttribute('data-default');
    }

    fetch(`/stream-video/${type}`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-TOKEN': window._CSRF,
        },
        body: JSON.stringify({
            id: el.getAttribute('data-id'),
            resourceID: s.getAttribute('data-resource-id'),
            itemID: s.getAttribute('data-item-id'),
        }),
    });
}

function wireEmbedCopy(container) {
    const copy = container.querySelector('#embed label.copy');
    if (!copy) return;
    copy.addEventListener('click', (e) => {
        e.preventDefault();
        const textarea = container.querySelector('#embed textarea');
        if (!textarea) return;
        navigator.clipboard.writeText(textarea.value).then(() => {
            if (window.toast) window.toast.success(t('player.copied'));
        });
    });
}

function wireLogo(container, playerContainer) {
    const logo = container.querySelector('#logo');
    if (!logo) return;
    // Replace template classes with player-managed class for CSS transitions
    logo.className = 'wt-player-logo';
    playerContainer.appendChild(logo);
}

/**
 * Destroy current player instance.
 */
export function destroyPlayer() {
    if (!_currentPlayer) return;
    const { mountEl, playerContainer, videoEl } = _currentPlayer;

    // Destroy HLS
    if (window.hlsPlayer) {
        window.hlsPlayer.stopLoad();
        window.hlsPlayer.destroy();
        window.hlsPlayer = null;
    }

    // Unmount Preact
    render(null, playerContainer);

    // Remove video
    videoEl.remove();

    // Remove mount point
    mountEl.remove();

    _currentPlayer = null;
}
