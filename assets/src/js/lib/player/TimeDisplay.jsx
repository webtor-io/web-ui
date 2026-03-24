/**
 * Time display: "0:45 / 1:23:45"
 */
export function TimeDisplay({ currentTime, duration }) {
    return (
        <div class="wt-player-time">
            <span>{formatTime(currentTime)}</span>
            {duration > 0 && (
                <>
                    <span class="wt-player-time-sep"> / </span>
                    <span>{formatTime(duration)}</span>
                </>
            )}
        </div>
    );
}

function formatTime(seconds) {
    if (!seconds || seconds < 0 || !isFinite(seconds)) return '0:00';
    const s = Math.floor(seconds);
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    const pad = (n) => n < 10 ? '0' + n : '' + n;
    if (h > 0) return h + ':' + pad(m) + ':' + pad(sec);
    return m + ':' + pad(sec);
}
