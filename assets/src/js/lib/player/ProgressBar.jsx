import { useRef, useCallback, useState } from 'preact/hooks';

/**
 * Full-width progress/seek bar with buffered and available indicators.
 */
export function ProgressBar({ currentTime, duration, buffered, onSeek, disabled }) {
    const barRef = useRef(null);
    const [hovering, setHovering] = useState(false);
    const [hoverX, setHoverX] = useState(0);
    const [dragging, setDragging] = useState(false);
    const [dragX, setDragX] = useState(0);

    const displayProgress = dragging ? dragX : (duration > 0 ? Math.min(1, Math.max(0, currentTime / duration)) : 0);
    const bufferedProgress = duration > 0 ? Math.min(1, Math.max(0, buffered / duration)) : 0;
    const tooltipX = dragging ? dragX : hoverX;
    const tooltipTime = duration > 0 ? tooltipX * duration : 0;

    const getPositionFromEvent = useCallback((e) => {
        const bar = barRef.current;
        if (!bar) return 0;
        const rect = bar.getBoundingClientRect();
        const touch = e.touches ? e.touches[0] : e;
        return Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width));
    }, []);

    const handlePointerDown = useCallback((e) => {
        if (disabled) return;
        e.preventDefault();
        const pos = getPositionFromEvent(e);
        setDragging(true);
        setDragX(pos);

        function handleMove(e) {
            const pos = getPositionFromEvent(e);
            setDragX(pos);
            setHoverX(pos);
        }
        function handleUp(e) {
            const pos = getPositionFromEvent(e);
            setDragging(false);
            if (onSeek && duration > 0) onSeek(pos * duration);
            document.removeEventListener('pointermove', handleMove);
            document.removeEventListener('pointerup', handleUp);
        }
        document.addEventListener('pointermove', handleMove);
        document.addEventListener('pointerup', handleUp);
    }, [duration, onSeek, getPositionFromEvent, disabled]);

    const handleMouseMove = useCallback((e) => {
        const pos = getPositionFromEvent(e);
        setHoverX(pos);
    }, [getPositionFromEvent]);

    return (
        <div
            ref={barRef}
            class={`wt-player-progress ${hovering || dragging ? 'wt-player-progress--active' : ''} ${disabled ? 'wt-player-progress--disabled' : ''}`}
            onPointerDown={handlePointerDown}
            onMouseEnter={() => setHovering(true)}
            onMouseLeave={() => { if (!dragging) setHovering(false); }}
            onMouseMove={handleMouseMove}
            role="slider"
            aria-label="Seek"
            aria-valuenow={Math.floor(currentTime)}
            aria-valuemin={0}
            aria-valuemax={Math.floor(duration)}
            tabIndex={0}
        >
            {/* Track background */}
            <div class="wt-player-progress-track">
                {/* Buffered */}
                <div class="wt-player-progress-buffered" style={{ transform: `scaleX(${bufferedProgress})` }} />
                {/* Played */}
                <div class="wt-player-progress-played" style={{ transform: `scaleX(${displayProgress})` }} />
                {/* Thumb — inside track for correct positioning */}
                <div class="wt-player-progress-thumb" style={{ left: `${displayProgress * 100}%` }} />
                {/* Ghost thumb follows mouse on hover (not during drag — thumb already follows) */}
                {hovering && !dragging && !disabled && (
                    <div class="wt-player-progress-thumb wt-player-progress-thumb--ghost" style={{ left: `${hoverX * 100}%` }} />
                )}
            </div>
            {/* Hover/drag tooltip — clamped to stay within bounds */}
            {(hovering || dragging) && duration > 0 && (
                <div class="wt-player-progress-tooltip" style={{ left: `clamp(1.5rem, ${tooltipX * 100}%, calc(100% - 1.5rem))` }}>
                    {formatTimeShort(tooltipTime)}
                </div>
            )}
        </div>
    );
}

function formatTimeShort(seconds) {
    if (!seconds || seconds < 0 || !isFinite(seconds)) return '0:00';
    const s = Math.floor(seconds);
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    const pad = (n) => n < 10 ? '0' + n : '' + n;
    if (h > 0) return h + ':' + pad(m) + ':' + pad(sec);
    return m + ':' + pad(sec);
}
