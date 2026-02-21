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
    let features = [
        'playpause',
        'current',
        'progress',
        'duration',
        'advancedtracks',
        'volume',
        'fullscreen',
        'chromecast',
        'embed',
    ];
    if (duration > 0) {
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
            path: '/assets/lib/hls.min.js',
            maxBufferSize: 50 * 1000 * 1000,
            maxMaxBufferLength: 180,
        },
        error: function(e) {
            console.log(e);
            destroyPlayer();
            initPlayer(target);
        },
        async success(media, node, player) {
            if (duration > 0) {
                const oldGetDuration = media.getDuration;
                media.oldGetDuration = function() {
                    return oldGetDuration.call(media);
                }
                media.getDuration = function() {
                    if (duration > 0) return duration;
                    return this.oldGetDuration();
                }
                const oldSetCurrentTime = player.setCurrentTime;
                player.setCurrentTime = function(time, userInteraction = false) {
                    if (time > media.oldGetDuration()) {
                        return;
                    }
                    return oldSetCurrentTime.call(player, time, userInteraction);
                }
            }
            let paused = false;
            window.addEventListener('player_paused', function() {
                player.pause();
                paused = true;
            });
            media.addEventListener('playing', () => {
                if (paused) player.pause();
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
                if (player) {
                    player.controlsEnabled = controls;
                }
                if (!controls) {
                    document.querySelector('.mejs__controls').style.display = 'none';
                }
                window.addEventListener('player_play', function() {
                    paused = false;
                    player.play();
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
                media.hlsPlayer.on(Hls.Events.MANIFEST_PARSED, function (event, data) {
                    if (media.hlsPlayer.levels.length > 1) {
                        media.hlsPlayer.startLevel = 1;
                    }
                    remapTrackIds(media.hlsPlayer);
                });
                media.hlsPlayer.on(Hls.Events.ERROR, function (event, data) {
                    if (data.fatal) {
                        switch (data.type) {
                            case Hls.ErrorTypes.NETWORK_ERROR:
                                if (data.details === 'levelParsingError') {
                                    // Server returned empty manifest (stream not ready yet), retry after delay
                                    setTimeout(() => {
                                        media.hlsPlayer.startLoad();
                                    }, 3000);
                                } else {
                                    // try to recover network error
                                    media.hlsPlayer.startLoad();
                                }
                                break;
                            case Hls.ErrorTypes.MEDIA_ERROR:
                                media.hlsPlayer.recoverMediaError();
                                break;
                            default:
                                // cannot recover
                                media.hlsPlayer.destroy();
                                break;
                        }
                    } else {
                        console.log(data);
                        if (data.type === Hls.ErrorTypes.MEDIA_ERROR && data.details === 'bufferStalledError') {
                            setTimeout(() => {
                                media.hlsPlayer.startLoad();
                            }, 5000);
                            // media.hlsPlayer.recoverMediaError();
                        }
                    }
                });
            }
        },
    });
}

export function destroyPlayer() {
    console.log(player, hlsPlayer, video);
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
