import { PlayIcon, PauseIcon, NextIcon, FullscreenIcon, ExitFullscreenIcon, CaptionsIcon, EmbedIcon } from './icons';
import { ProgressBar } from './ProgressBar';
import { TimeDisplay } from './TimeDisplay';
import { VolumeControl } from './VolumeControl';
import { SpeedControl } from './SpeedControl';
import { SettingsControl } from './SettingsControl';
import { t } from './i18n';

/**
 * Control bar component.
 * Assembled from sub-components. Features are toggled via props.
 */
export function Controls({
    playing, currentTime, duration, volume, muted, rate, fullscreen, buffered, seeking,
    onTogglePlay, onSeek, onVolumeChange, onRateChange, onToggleMute, onToggleFullscreen,
    onCaptionsClick, onEmbedClick, onNext, nextLabel, nextBusy, autoplayNext, onToggleAutoplayNext,
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

                    {/* Next episode / track, right after Play (owner). Present
                        only when the server named a next file. */}
                    {onNext && (
                        <button type="button" class="wt-player-btn wt-player-btn--next" onClick={seeking || nextBusy ? undefined : onNext} disabled={seeking || nextBusy}
                            aria-label={nextLabel ? `${t('player.next')}: ${nextLabel}` : t('player.next')} title={nextLabel ? `${t('player.next')}: ${nextLabel}` : t('player.next')}>
                            {nextBusy ? <span class="wt-player-btn-spinner" aria-hidden="true" /> : <NextIcon />}
                        </button>
                    )}

                    {features.duration && (
                        <TimeDisplay currentTime={currentTime} duration={duration} />
                    )}
                </div>

                {/* Right group: volume, speed, captions, embed, fullscreen, more */}
                <div class="wt-player-controls-right">
                    {features.volume && (
                        <VolumeControl
                            volume={volume}
                            muted={muted}
                            onVolumeChange={onVolumeChange}
                            onToggleMute={onToggleMute}
                        />
                    )}

                    {features.speed && (
                        <SpeedControl rate={rate} onRateChange={onRateChange} menu={isVideo} />
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

                    {features.fullscreen && isVideo && (
                        <button type="button" class="wt-player-btn" onClick={onToggleFullscreen} aria-label={fullscreen ? t('player.exitFullscreen') : t('player.fullscreen')}>
                            {fullscreen ? <ExitFullscreenIcon /> : <FullscreenIcon />}
                        </button>
                    )}

                    {/* "More" (three dots): autoplay of the next file, at any time
                        and with its name on it. Always the LAST thing on the
                        right (owner) -- after fullscreen, where a menu is
                        looked for. Only where there is a next file. */}
                    {onNext && onToggleAutoplayNext && (
                        <SettingsControl autoplayNext={autoplayNext} onToggleAutoplayNext={onToggleAutoplayNext} />
                    )}
                </div>
            </div>
        </div>
    );
}
