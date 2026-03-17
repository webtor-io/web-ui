import '../../styles/mediaelement.css';
import 'mediaelement/full';
import './mediaelement-plugins/availableprogress';
import './mediaelement-plugins/advancedtracks';
import './mediaelement-plugins/chromecast';
import './mediaelement-plugins/embed';
import './mediaelement-plugins/logo';

const {MediaElementPlayer} = global;

let player;
let hlsPlayer;
let video;

function remapTrackGroup(elements, hlsTracks) {
    if (!elements.length || !hlsTracks.length) return;

    // If counts match, assume order is preserved (common case)
    if (elements.length === hlsTracks.length) {
        for (let i = 0; i < elements.length; i++) {
            elements[i].setAttribute('data-mp-id', String(i));
        }
        return;
    }

    // Counts differ (some codecs skipped by transcoder) — match by lang+name
    const used = new Set();
    for (const el of elements) {
        const lang = (el.getAttribute('data-srclang') || '').toLowerCase();
        const label = el.textContent.trim();

        // Try exact lang+name match first
        let matched = false;
        for (let i = 0; i < hlsTracks.length; i++) {
            if (used.has(i)) continue;
            const hlsLang = (hlsTracks[i].lang || '').toLowerCase();
            const hlsName = (hlsTracks[i].name || '').trim();
            if (lang && hlsLang && lang === hlsLang && label && hlsName && label === hlsName) {
                el.setAttribute('data-mp-id', String(i));
                used.add(i);
                matched = true;
                break;
            }
        }
        if (matched) continue;

        // Fallback: match by lang only (first unused)
        for (let i = 0; i < hlsTracks.length; i++) {
            if (used.has(i)) continue;
            const hlsLang = (hlsTracks[i].lang || '').toLowerCase();
            if (lang && hlsLang && lang === hlsLang) {
                el.setAttribute('data-mp-id', String(i));
                used.add(i);
                matched = true;
                break;
            }
        }
        if (matched) continue;

        // Fallback: match by name only (first unused)
        for (let i = 0; i < hlsTracks.length; i++) {
            if (used.has(i)) continue;
            const hlsName = (hlsTracks[i].name || '').trim();
            if (label && hlsName && label === hlsName) {
                el.setAttribute('data-mp-id', String(i));
                used.add(i);
                break;
            }
        }
    }
}

function remapTrackIds(hlsPlayer) {
    // Remap both the original and cloned #subtitles containers
    const containers = document.querySelectorAll('#subtitles');
    if (!containers.length) return;

    const hlsSubTracks = hlsPlayer.subtitleTracks || [];
    const hlsAudioTracks = hlsPlayer.audioTracks || [];

    for (const container of containers) {
        const subEls = Array.from(container.querySelectorAll('.subtitle[data-provider="MediaProbe"]'));
        remapTrackGroup(subEls, hlsSubTracks);

        const audioEls = Array.from(container.querySelectorAll('.audio[data-provider="MediaProbe"]'));
        remapTrackGroup(audioEls, hlsAudioTracks);
    }
}

function setupHlsEvents(hls, media) {
    hls.on(Hls.Events.MANIFEST_PARSED, function (event, data) {
        if (hls.levels.length > 1) {
            hls.startLevel = 1;
        }
        remapTrackIds(hls);
    });
    hls.on(Hls.Events.ERROR, function (event, data) {
        if (data.fatal) {
            switch (data.type) {
                case Hls.ErrorTypes.NETWORK_ERROR:
                    if (data.details === 'levelParsingError') {
                        // Server returned empty manifest (stream not ready yet), retry after delay
                        setTimeout(() => {
                            hls.startLoad();
                        }, 3000);
                    } else {
                        // try to recover network error
                        hls.startLoad();
                    }
                    break;
                case Hls.ErrorTypes.MEDIA_ERROR:
                    hls.recoverMediaError();
                    break;
                default:
                    // cannot recover
                    hls.destroy();
                    break;
            }
        } else {
            console.log(data);
            if (data.type === Hls.ErrorTypes.MEDIA_ERROR && data.details === 'bufferStalledError') {
                setTimeout(() => {
                    hls.startLoad();
                }, 5000);
            }
        }
    });
}

export function initPlayer(target) {
    video = target.querySelector('.player');
    let settings = {};
    if (video.dataset.settings) {
        settings = JSON.parse(video.dataset.settings);
    }
    const height = video.height;
    const controls = video.controls;
    const stretching = height ? 'auto' : 'responsive';
    const duration = video.getAttribute('data-duration') ? parseFloat(video.getAttribute('data-duration')) : -1;

    // Session-based transcoder state
    const sessionId = video.dataset.sessionId;
    const sessionSeekUrl = video.dataset.sessionSeekUrl;
    const sessionDeletePath = video.dataset.sessionDeletePath;
    const isSession = !!sessionId;
    let seekOffset = 0;
    let isSeeking = false;

    const isVideo = video.tagName === 'VIDEO';
    let features = [
        'playpause',
        'current',
        'progress',
        'duration',
        'volume',
    ];
    if (isVideo) {
        features.push('advancedtracks', 'fullscreen', 'chromecast', 'embed');
    }
    if (duration > 0 && !isSession) {
        features.push('availableprogress');
    }
    if (settings.features) {
        for (const name in settings.features) {
            if (features.includes(name) && settings.features[name] == false) {
                features = features.filter((e) => e != name);
            } else if (!features.includes(name) && settings.features[name] == true) {
                features.push(name);
            }
        }
    }
    if (window._domainSettings && window._domainSettings.ads === true) {
        features.push('logo');
    }

    async function doSessionSeek(targetTime) {
        if (isSeeking) return;
        isSeeking = true;
        try {
            // Show spinner via CSS class — survives MEJS's inline display:none on canplay
            const loadingOverlay = document.querySelector('.mejs__overlay-loading');
            const loadingLayer = loadingOverlay ? loadingOverlay.parentNode : null;
            if (loadingLayer) loadingLayer.classList.add('session-seeking-active');

            const hls = media.hlsPlayer;

            // Save current track selections
            const savedAudioTrack = hls.audioTrack;
            const savedSubtitleTrack = hls.subtitleTrack;
            const savedSubtitleDisplay = hls.subtitleDisplay;

            const separator = sessionSeekUrl.includes('?') ? '&' : '?';
            await fetch(sessionSeekUrl + separator + 't=' + targetTime, { method: 'POST' });

            seekOffset = targetTime > 0 ? Math.floor(targetTime / 30) * 30 : 0;

            const sourceUrl = video.querySelector('source').getAttribute('src');

            // Reload the manifest on the same HLS instance — keeps MEJS in sync
            hls.stopLoad();
            hls.loadSource(sourceUrl);

            // Restore audio track when tracks are actually ready
            if (savedAudioTrack >= 0) {
                hls.once(Hls.Events.AUDIO_TRACKS_UPDATED, () => {
                    hls.audioTrack = savedAudioTrack;
                });
            }

            // Restore subtitle track when tracks are actually ready
            if (savedSubtitleDisplay && savedSubtitleTrack >= 0) {
                hls.once(Hls.Events.SUBTITLE_TRACKS_UPDATED, () => {
                    hls.subtitleDisplay = true;
                    hls.subtitleTrack = savedSubtitleTrack;
                });
            }

            // Unlock seeking when manifest is parsed
            hls.once(Hls.Events.MANIFEST_PARSED, () => {
                isSeeking = false;
            });

            // Hide spinner when playback actually starts
            function onPlaying() {
                if (loadingLayer) loadingLayer.classList.remove('session-seeking-active');
                media.removeEventListener('playing', onPlaying);
            }
            media.addEventListener('playing', onPlaying);
        } catch (e) {
            console.error('Session seek failed:', e);
            const lo = document.querySelector('.mejs__overlay-loading');
            if (lo && lo.parentNode) lo.parentNode.classList.remove('session-seeking-active');
            isSeeking = false;
        }
    }

    // We need a reference to `media` available for doSessionSeek
    let media;

    player = new MediaElementPlayer(video, {
        renderers: ['native_hls', 'html5'],
        autoRewind: false,
        defaultSeekBackwardInterval: (media) => 15,
        defaultSeekForwardInterval: (media) => 15,
        iconSprite: 'assets/mejs-controls.svg',
        stretching,
        features,
        hls: {
            autoStartLoad: true,
            startPosition: 0,
            manifestLoadingTimeOut: 1000 * 60 * 10,
            manifestLoadingMaxRetry: 100,
            manifestLoadingMaxRetryTimeout: 1000 * 10,
            levelLoadingMaxRetry: 100,
            levelLoadingMaxRetryTimeout: 1000 * 10,
            fragLoadingMaxRetry: 100,
            fragLoadingMaxRetryTimeout: 1000 * 10,
            path: '/assets/lib/hls.min.js',
            maxBufferSize: 50 * 1000 * 1000,
            maxMaxBufferLength: 180,
        },
        error: function(e) {
            console.log(e);
            destroyPlayer();
            initPlayer(target);
        },
        async success(mediaEl, node, playerInst) {
            media = mediaEl;
            if (duration > 0) {
                const oldGetDuration = media.getDuration;
                media.oldGetDuration = function() {
                    return oldGetDuration.call(media);
                }
                media.getDuration = function() {
                    if (duration > 0) return duration;
                    return this.oldGetDuration();
                }
                if (isSession) {
                    // Override getCurrentTime to account for seekOffset
                    const origGetCurrentTime = playerInst.getCurrentTime.bind(playerInst);
                    playerInst.getCurrentTime = function() {
                        return seekOffset + origGetCurrentTime();
                    };

                    // Override oldGetDuration to account for seekOffset (for available progress)
                    const origOldGetDuration = media.oldGetDuration.bind(media);
                    media.oldGetDuration = function() {
                        return seekOffset + origOldGetDuration();
                    };

                    // Override setProgressRail to account for seekOffset in buffer indicator
                    const origSetProgressRail = playerInst.setProgressRail.bind(playerInst);
                    playerInst.setProgressRail = function(e) {
                        if (seekOffset > 0 && duration > 0 && playerInst.loaded) {
                            const target = (e !== undefined && e.detail) ? (e.detail.target || e.target) : media;
                            if (target && target.buffered && target.buffered.length > 0) {
                                const bufferedEnd = target.buffered.end(target.buffered.length - 1);
                                const percent = Math.min(1, Math.max(0, (seekOffset + bufferedEnd) / duration));
                                playerInst.setTransformStyle(playerInst.loaded, `scaleX(${percent})`);
                                return;
                            }
                        }
                        origSetProgressRail(e);
                    };

                    // Override setCurrentTime for session seek
                    playerInst.setCurrentTime = function(time) {
                        if (isSeeking) return;
                        doSessionSeek(time);
                    };
                } else {
                    const oldSetCurrentTime = playerInst.setCurrentTime;
                    playerInst.setCurrentTime = function(time, userInteraction = false) {
                        if (time > media.oldGetDuration()) {
                            return;
                        }
                        return oldSetCurrentTime.call(playerInst, time, userInteraction);
                    }
                }
            }
            let paused = false;
            window.addEventListener('player_paused', function() {
                playerInst.pause();
                paused = true;
            });
            media.addEventListener('playing', () => {
                if (paused) playerInst.pause();
            });
            let tracksInitialized = false;
            media.addEventListener('canplay', () => {
                if (hlsPlayer && document.getElementById('subtitles') && !tracksInitialized) {
                    tracksInitialized = true;
                    const defaultAudio = document.querySelector('.audio[data-default=true]');
                    const defaultSub = document.querySelector('.subtitle[data-default=true]');
                    const audioId = defaultAudio ? defaultAudio.getAttribute('data-mp-id') : null;
                    const subId = defaultSub ? defaultSub.getAttribute('data-mp-id') : null;
                    if (audioId) hlsPlayer.audioTrack = parseInt(audioId);
                    if (subId) {
                        hlsPlayer.subtitleDisplay = true;
                        hlsPlayer.subtitleTrack = parseInt(subId);
                    }
                }
                if (playerInst) {
                    playerInst.controlsEnabled = controls;
                }
                if (!controls) {
                    document.querySelector('.mejs__controls').style.display = 'none';
                }
                window.addEventListener('player_play', function() {
                    paused = false;
                    playerInst.play();
                });

                const event = new CustomEvent('player_ready');
                window.dispatchEvent(event);
            });
            if (media.hlsPlayer) {
                hlsPlayer = media.hlsPlayer;
                window.hlsPlayer = hlsPlayer;
                media.addEventListener('seeking', () => {
                    if (media.hlsPlayer.loadLevel > 1) {
                        media.hlsPlayer.loadLevel = 1;
                    }
                });
                media.addEventListener('seeked', () => {
                    media.hlsPlayer.loadLevel = -1;
                });
                setupHlsEvents(media.hlsPlayer, media);
            }
        },
    });

    // Session cleanup on page unload
    if (isSession && sessionDeletePath) {
        window.addEventListener('beforeunload', () => {
            navigator.sendBeacon(sessionDeletePath);
        });
    }
}

export function destroyPlayer() {
    console.log(player, hlsPlayer, video);
    if (video && video.dataset.sessionDeletePath) {
        navigator.sendBeacon(video.dataset.sessionDeletePath);
    }
    if (player) {
        player.options.stretching = 'none';
        player.pause();
        // this generates autoloading when switching to another location
        // player.remove();
        player = null;
    }
    if (hlsPlayer) {
        hlsPlayer.stopLoad();
        hlsPlayer.destroy();
        hlsPlayer = null;
    }
    if (video) {
        video.remove();
        video = null;
    }
}
