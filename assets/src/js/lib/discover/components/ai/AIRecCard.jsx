import { useCallback, useState } from 'preact/hooks';
import { WatchedBadge, RatingBadge } from '../ItemGrid';

// AIRecCard — the poster-only half of an AI recommendation. Title, year,
// rating, plot, and the Claude-generated reason all live in the sibling
// text column rendered by AIRecsGrid, because the AI feature uses a
// chessboard layout where each row alternates poster-left / poster-right.
//
// Clicking the poster triggers the parent onClick, which is wired to the
// existing StreamModal flow (see DiscoverApp.handleAICardClick).
//
// The watched / rating badges are the same primitives used by the regular
// Discover grid (re-exported from ItemGrid). They stop propagation so a
// click on them never cascades to the card-click → StreamModal flow.

function PosterGradient({ name }) {
    return (
        <div class="w-full h-full bg-gradient-to-br from-w-cyan/25 via-w-purple/15 to-w-pink/10 text-w-cyan/70 flex items-center justify-center">
            <div class="text-center font-bold text-lg p-3 line-clamp-3 drop-shadow-sm">
                {name || 'Unknown'}
            </div>
        </div>
    );
}

export function AIRecCard({ item, onClick, watched, rating, onToggleWatched, onRate }) {
    const [imgError, setImgError] = useState(false);
    const onImgError = useCallback(() => setImgError(true), []);
    const handleClick = useCallback(() => onClick(item), [item, onClick]);

    const handleWatchedClick = useCallback((e) => {
        e.stopPropagation();
        e.preventDefault();
        onToggleWatched?.();
    }, [onToggleWatched]);

    const handleRateClick = useCallback((e) => {
        e.stopPropagation();
        e.preventDefault();
        onRate?.();
    }, [onRate]);

    return (
        <div
            class={`group cursor-pointer relative overflow-hidden rounded-xl aspect-[2/3] ring-1 ring-w-line/40 hover:ring-w-cyan/60 transition-all shadow-lg hover:shadow-w-cyan/10${watched ? ' is-watched' : ''}`}
            onClick={handleClick}
            title={item.title}
        >
            {item.poster && !imgError ? (
                <img
                    class="w-full h-full object-cover group-hover:scale-[1.03] transition-transform duration-500"
                    src={item.poster}
                    alt={item.title || ''}
                    loading="lazy"
                    onError={onImgError}
                />
            ) : (
                <PosterGradient name={item.title} />
            )}
            {/* Subtle gradient overlay on hover to indicate interactivity. */}
            <div class="absolute inset-0 bg-gradient-to-t from-w-bg/60 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none" />

            {onToggleWatched && (
                <WatchedBadge watched={watched} onClick={handleWatchedClick} />
            )}
            {onRate && (
                <RatingBadge rating={rating} onClick={handleRateClick} />
            )}
        </div>
    );
}
