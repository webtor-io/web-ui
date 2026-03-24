import { useRef, useCallback, useState } from 'preact/hooks';
import { VolumeUpIcon, VolumeOffIcon } from './icons';

/**
 * Volume button + horizontal slider.
 */
export function VolumeControl({ volume, muted, onVolumeChange, onToggleMute }) {
    const barRef = useRef(null);
    const [hovering, setHovering] = useState(false);
    const [dragging, setDragging] = useState(false);
    const [dragX, setDragX] = useState(0);
    const [hoverX, setHoverX] = useState(0);

    const effectiveVolume = muted ? 0 : volume;
    const displayVolume = dragging ? dragX : effectiveVolume;

    const getPos = useCallback((e) => {
        const bar = barRef.current;
        if (!bar) return 0;
        const rect = bar.getBoundingClientRect();
        const touch = e.touches ? e.touches[0] : e;
        return Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width));
    }, []);

    const handleSliderDown = useCallback((e) => {
        e.preventDefault();
        e.stopPropagation();
        const pos = getPos(e);
        setDragging(true);
        setDragX(pos);

        function handleMove(e) {
            const pos = getPos(e);
            setDragX(pos);
            setHoverX(pos);
        }
        function handleUp(e) {
            const pos = getPos(e);
            setDragging(false);
            onVolumeChange(pos);
            document.removeEventListener('pointermove', handleMove);
            document.removeEventListener('pointerup', handleUp);
        }
        document.addEventListener('pointermove', handleMove);
        document.addEventListener('pointerup', handleUp);
    }, [onVolumeChange, getPos]);

    const handleMouseMove = useCallback((e) => {
        setHoverX(getPos(e));
    }, [getPos]);

    return (
        <div
            class={`wt-player-volume ${hovering || dragging ? 'wt-player-volume--expanded' : ''}`}
            onMouseEnter={() => setHovering(true)}
            onMouseLeave={() => { if (!dragging) setHovering(false); }}
        >
            <button
                type="button"
                class="wt-player-btn"
                onClick={onToggleMute}
                aria-label={muted ? 'Unmute' : 'Mute'}
            >
                {muted || volume === 0 ? <VolumeOffIcon /> : <VolumeUpIcon />}
            </button>
            <div class="wt-player-volume-slider-wrap">
                <div
                    ref={barRef}
                    class={`wt-player-volume-slider ${dragging ? 'wt-player-volume-slider--active' : ''}`}
                    onPointerDown={handleSliderDown}
                    onMouseMove={handleMouseMove}
                    role="slider"
                    aria-label="Volume"
                    aria-valuenow={Math.round(displayVolume * 100)}
                    aria-valuemin={0}
                    aria-valuemax={100}
                >
                    <div class="wt-player-volume-track">
                        <div class="wt-player-volume-level" style={{ transform: `scaleX(${displayVolume})` }} />
                        <div class="wt-player-volume-thumb" style={{ left: `${displayVolume * 100}%` }} />
                        {/* Ghost thumb follows mouse on hover */}
                        {hovering && !dragging && (
                            <div class="wt-player-volume-thumb wt-player-volume-thumb--ghost" style={{ left: `${hoverX * 100}%` }} />
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
