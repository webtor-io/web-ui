import { PlayIcon, PauseIcon, FullscreenIcon, ExitFullscreenIcon, CaptionsIcon, EmbedIcon, ShareIcon } from './icons';
import { ProgressBar } from './ProgressBar';
import { TimeDisplay } from './TimeDisplay';
import { VolumeControl } from './VolumeControl';
import { t } from './i18n';

/**
 * Control bar component.
 * Assembled from sub-components. Features are toggled via props.
 */
export function Controls({
    playing, currentTime, duration, volume, muted, fullscreen, buffered, seeking,
    onTogglePlay, onSeek, onVolumeChange, onToggleMute, onToggleFullscreen,
    onCaptionsClick, onEmbedClick, onShareClick, streamStarted,
    isVideo, features,
}) {
    return (
        <div class="wt-player-controls" onClick={(e) => e.stopPropagation()} onDblClick={(e) => e.stopPropagation()}>
            {/* Progress bar full-width on top */}
            {features.progress && (
                <ProgressBar
                    currentTime={currentTime}
                    duration={duration}
                    buffered={buffered}
                    onSeek={onSeek}
                    disabled={seeking}
                />
            )}

            {/* Controls row */}
            <div class="wt-player-controls-row">
                {/* Left group: play + time */}
                <div class="wt-player-controls-left">
                    {features.playpause && (
                        <button type="button" class="wt-player-btn wt-player-btn--play" onClick={seeking ? undefined : onTogglePlay} aria-label={playing ? t('player.pause') : t('player.play')} disabled={seeking}>
                            {playing ? <PauseIcon /> : <PlayIcon />}
                        </button>
                    )}

                    {features.duration && (
                        <TimeDisplay currentTime={currentTime} duration={duration} />
                    )}
                </div>

                {/* Right group: volume, captions, embed, fullscreen */}
                <div class="wt-player-controls-right">
                    {features.volume && (
                        <VolumeControl
                            volume={volume}
                            muted={muted}
                            onVolumeChange={onVolumeChange}
                            onToggleMute={onToggleMute}
                        />
                    )}

                    {features.advancedtracks && (
                        <button type="button" class="wt-player-btn" onClick={onCaptionsClick} aria-label={t('player.subtitlesAndAudio')}>
                            <CaptionsIcon />
                        </button>
                    )}

                    {features.embed && (
                        <button type="button" class="wt-player-btn" onClick={onEmbedClick} aria-label={t('player.embed')}>
                            <EmbedIcon />
                        </button>
                    )}

                    {features.share && streamStarted && onShareClick && (
                        <button type="button" class="wt-player-btn" onClick={onShareClick} aria-label={t('player.share')}>
                            <ShareIcon />
                        </button>
                    )}

                    {features.fullscreen && isVideo && (
                        <button type="button" class="wt-player-btn" onClick={onToggleFullscreen} aria-label={fullscreen ? t('player.exitFullscreen') : t('player.fullscreen')}>
                            {fullscreen ? <ExitFullscreenIcon /> : <FullscreenIcon />}
                        </button>
                    )}
                </div>
            </div>
        </div>
    );
}
