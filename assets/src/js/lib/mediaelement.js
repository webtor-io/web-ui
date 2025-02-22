import '../../styles/mediaelement.css';
import 'mediaelement';
import './mediaelement-plugins/availableprogress';
import './mediaelement-plugins/advancedtracks';
import './mediaelement-plugins/chromecast';
import './mediaelement-plugins/embed';
import './mediaelement-plugins/logo';

const {MediaElementPlayer} = global;

let player;
let hlsPlayer;
let video;

export function initPlayer(target) {
    video = target.querySelector('.player');
    const settings = JSON.parse(video.dataset.settings);
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
        async success(media) {
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
            media.addEventListener('canplay', () => {
                if (hlsPlayer && document.getElementById('subtitles')) {
                    const audioId = document.querySelector('.audio[data-default=true]').getAttribute('data-mp-id');
                    const subId = document.querySelector('.subtitle[data-default=true]').getAttribute('data-mp-id');
                    if (audioId) hlsPlayer.audioTrack = audioId;
                    if (subId) hlsPlayer.subtitleTrack = subId;
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
                });
                media.hlsPlayer.on(Hls.Events.ERROR, function (event, data) {
                    if (data.fatal) {
                        switch (data.type) {
                            case Hls.ErrorTypes.NETWORK_ERROR:
                                // try to recover network error
                                media.hlsPlayer.startLoad();
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
    if (player) {
        player.options.stretching = 'none';
        player.remove();
        player = null;
    }
    if (hlsPlayer) {
        hlsPlayer.destroy();
        hlsPlayer = null;
    }
}
