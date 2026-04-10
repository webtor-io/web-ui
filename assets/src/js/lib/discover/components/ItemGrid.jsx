import { useCallback, useState, useMemo } from 'preact/hooks';

// 5-star half-step rating from 0-10 scale, matching library/stars.html exactly.
// DaisyUI fills stars via :has(~[aria-current=true]) — all siblings before the
// selected one get opacity:1. We must set aria-current="true" on the right element.
function StarRating({ rating }) {
    if (!rating || parseFloat(rating) <= 0) return null;
    const r = parseFloat(rating);
    const stars5 = r / 2; // 0-10 → 0-5

    const items = useMemo(() => {
        const result = [];
        const step = 0.5;
        for (let i = 0; i <= 5; i += step) {
            const isHalf = (i * 2) % 2 === 1;
            const selected = stars5 >= i && stars5 < i + step;
            if (i === 0) {
                result.push(<div key="h" class="rating-hidden" />);
            } else if (isHalf) {
                result.push(<div key={i} class="mask mask-star-2 mask-half-1" aria-current={selected ? 'true' : undefined} />);
            } else {
                result.push(<div key={i} class="mask mask-star-2 mask-half-2" aria-current={selected ? 'true' : undefined} />);
            }
        }
        return result;
    }, [stars5]);

    return (
        <div class="flex items-center gap-1">
            <div class="rating rating-xs rating-half flex items-center text-w-purpleL">
                {items}
            </div>
            <span class="text-xs text-w-muted">{r.toFixed(1)}</span>
        </div>
    );
}

export function ItemGrid({ items, showBadges, userStatuses, onClick, onToggleWatched, onRate }) {
    if (!items.length) return null;
    return (
        <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-4">
            {items.map(item => {
                const status = userStatuses && userStatuses[item.id];
                return (
                    <ItemCard
                        key={item.id}
                        item={item}
                        showBadge={showBadges}
                        watched={status ? status.watched : false}
                        rating={status ? status.rating : 0}
                        onClick={onClick}
                        onToggleWatched={onToggleWatched}
                        onRate={onRate}
                    />
                );
            })}
        </div>
    );
}

function PosterGradient({ name }) {
    return (
        <div class="w-full h-full bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 text-w-purpleL/60 flex items-center justify-center">
            <div class="text-center font-bold text-lg p-3 line-clamp-3 drop-shadow-sm">
                {name || 'Unknown'}
            </div>
        </div>
    );
}

// Card visual primitives (.w-card-frame, .w-card-title, .w-card-badge,
// .w-card-badge-ghost) are defined in assets/src/styles/style.css and shared
// with templates/partials/library/video_list.html. See docs/uikit.html
// section 7 "Video Card".

function WatchedBadge({ watched, onClick }) {
    if (watched) {
        return (
            <button
                type="button"
                onClick={onClick}
                class="w-card-badge top-2 right-2 text-green-400 uppercase tracking-wider"
                title="Unmark watched"
            >
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                Watched
            </button>
        );
    }
    return (
        <button
            type="button"
            onClick={onClick}
            class="w-card-badge-ghost top-2 right-2 hover:text-green-400"
            title="Mark as watched"
        >
            <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z" />
                <path d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
            </svg>
        </button>
    );
}

function RatingBadge({ rating, onClick }) {
    if (rating > 0) {
        return (
            <button
                type="button"
                onClick={onClick}
                class="w-card-badge top-2 left-2 text-yellow-400"
                title="Change rating"
            >
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor"><path fill-rule="evenodd" d="M10.788 3.21c.448-1.077 1.976-1.077 2.424 0l2.082 5.006 5.404.434c1.164.093 1.636 1.545.749 2.305l-4.117 3.527 1.257 5.273c.271 1.136-.964 2.033-1.96 1.425L12 18.354 7.373 21.18c-.996.608-2.231-.29-1.96-1.425l1.257-5.273-4.117-3.527c-.887-.76-.415-2.212.749-2.305l5.404-.434 2.082-5.005Z" clip-rule="evenodd" /></svg>
                {rating}
            </button>
        );
    }
    return (
        <button
            type="button"
            onClick={onClick}
            class="w-card-badge-ghost top-2 left-2 hover:text-yellow-400"
            title="Rate"
        >
            <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M11.48 3.499a.562.562 0 0 1 1.04 0l2.125 5.111a.563.563 0 0 0 .475.345l5.518.442c.499.04.701.663.321.988l-4.204 3.602a.563.563 0 0 0-.182.557l1.285 5.385a.562.562 0 0 1-.84.61l-4.725-2.885a.562.562 0 0 0-.586 0L6.982 20.54a.562.562 0 0 1-.84-.61l1.285-5.386a.562.562 0 0 0-.182-.557l-4.204-3.602a.562.562 0 0 1 .321-.988l5.518-.442a.563.563 0 0 0 .475-.345L11.48 3.5Z" />
            </svg>
        </button>
    );
}

function ItemCard({ item, showBadge, watched, rating, onClick, onToggleWatched, onRate }) {
    const handleClick = useCallback(() => onClick(item), [item, onClick]);
    const [imgError, setImgError] = useState(false);
    const onImgError = useCallback(() => setImgError(true), []);

    const handleWatchedClick = useCallback((e) => {
        e.stopPropagation();
        e.preventDefault();
        if (onToggleWatched) onToggleWatched(item);
    }, [item, onToggleWatched]);

    const handleRateClick = useCallback((e) => {
        e.stopPropagation();
        e.preventDefault();
        if (onRate) onRate(item);
    }, [item, onRate]);

    const isImdb = item.id && item.id.startsWith('tt');

    return (
        <div class="group cursor-pointer flex" onClick={handleClick}>
            <div class={`w-card-frame${watched ? ' is-watched' : ''}`}>
                <figure class="aspect-[2/3] overflow-hidden relative">
                    {item.poster && !imgError ? (
                        <img
                            class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
                            src={item.poster}
                            alt={item.name || ''}
                            loading="lazy"
                            onError={onImgError}
                        />
                    ) : (
                        <PosterGradient name={item.name} />
                    )}
                    {isImdb && (
                        <>
                            <WatchedBadge watched={watched} onClick={handleWatchedClick} />
                            <RatingBadge rating={rating} onClick={handleRateClick} />
                        </>
                    )}
                </figure>
                <div class="p-3">
                    <h3 class="w-card-title">{item.name || 'Unknown'}</h3>
                    <div class="flex justify-between items-center mt-1.5">
                        <div class="flex items-center gap-1.5">
                            {(item.releaseInfo || item.year) && (
                                <span class="text-xs text-w-muted">{item.releaseInfo || item.year}</span>
                            )}
                            {showBadge && item.type && (
                                <span class="text-[10px] text-w-muted/60 uppercase tracking-wider">
                                    {item.type === 'series' ? 'series' : item.type}
                                </span>
                            )}
                        </div>
                        {item.imdbRating && <StarRating rating={item.imdbRating} />}
                    </div>
                </div>
            </div>
        </div>
    );
}

export { StarRating };
