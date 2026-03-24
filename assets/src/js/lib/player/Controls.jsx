import { PlayIcon, PauseIcon, FullscreenIcon, ExitFullscreenIcon, CaptionsIcon, EmbedIcon } from './icons';
import { ProgressBar } from './ProgressBar';
import { TimeDisplay } from './TimeDisplay';
import { VolumeControl } from './VolumeControl';

/**
 * Control bar component.
 * Assembled from sub-components. Features are toggled via props.
 */
export function Controls({
    playing, currentTime, duration, volume, muted, fullscreen, buffered, seeking,
    onTogglePlay, onSeek, onVolumeChange, onToggleMute, onToggleFullscreen,
    onCaptionsClick, onEmbedClick,
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
                        <button type="button" class="wt-player-btn wt-player-btn--play" onClick={seeking ? undefined : onTogglePlay} aria-label={playing ? 'Pause' : 'Play'} disabled={seeking}>
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
                        <button type="button" class="wt-player-btn" onClick={onCaptionsClick} aria-label="Subtitles & Audio">
                            <CaptionsIcon />
                        </button>
                    )}

                    {features.embed && (
                        <button type="button" class="wt-player-btn" onClick={onEmbedClick} aria-label="Embed">
                            <EmbedIcon />
                        </button>
                    )}

                    {features.fullscreen && isVideo && (
                        <button type="button" class="wt-player-btn" onClick={onToggleFullscreen} aria-label={fullscreen ? 'Exit Fullscreen' : 'Fullscreen'}>
                            {fullscreen ? <ExitFullscreenIcon /> : <FullscreenIcon />}
                        </button>
                    )}
                </div>
            </div>
        </div>
    );
}
