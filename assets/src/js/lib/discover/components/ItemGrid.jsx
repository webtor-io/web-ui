import { useCallback, useState, useMemo } from 'preact/hooks';
import { t } from '../i18n';

// 5-star half-step rating from 0-10 scale, matching library/stars.html exactly.
// DaisyUI fills stars via :has(~[aria-current=true]) — all siblings before the
// selected one get opacity:1. We must set aria-current="true" on the right element.
// Renders both compact (★ 7.6) and full (5-star row) versions.
// CSS container queries on .w-card-frame control which one is visible
// based on card width (threshold: 210px). See style.css and uikit section 15.
// This matches the Go template in library/video_list.html exactly.
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
        <>
            <div class="w-card-stars-compact flex items-center gap-0.5 text-w-purpleL">
                <svg class="w-3 h-3 -mt-px" viewBox="0 0 24 24" fill="currentColor"><path d="M10.788 3.21c.448-1.077 1.976-1.077 2.424 0l2.082 5.006 5.404.434c1.164.093 1.636 1.545.749 2.305l-4.117 3.527 1.257 5.273c.271 1.136-.964 2.033-1.96 1.425L12 18.354 7.373 21.18c-.996.608-2.231-.29-1.96-1.425l1.257-5.273-4.117-3.527c-.887-.76-.415-2.212.749-2.305l5.404-.434 2.082-5.005Z"/></svg>
                <span class="text-xs text-w-muted">{r.toFixed(1)}</span>
            </div>
            <div class="w-card-stars-full flex items-center gap-1">
                <div class="rating rating-xs rating-half flex items-center text-w-purpleL">
                    {items}
                </div>
                <span class="text-xs text-w-muted">{r.toFixed(1)}</span>
            </div>
        </>
    );
}

export function ItemGrid({ items, showBadges, userStatuses, watchlistIds, onClick, onToggleWatched, onRate, onToggleWatchlist }) {
    if (!items.length) return null;
    return (
        <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-4">
            {items.map(item => {
                const status = userStatuses && userStatuses[item.id];
                const inWatchlist = !!(watchlistIds && watchlistIds.has && watchlistIds.has(item.id));
                return (
                    <ItemCard
                        key={item.id}
                        item={item}
                        showBadge={showBadges}
                        watched={status ? status.watched : false}
                        rating={status ? status.rating : 0}
                        inWatchlist={inWatchlist}
                        onClick={onClick}
                        onToggleWatched={onToggleWatched}
                        onRate={onRate}
                        onToggleWatchlist={onToggleWatchlist}
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
                {name || t('discover.unknown')}
            </div>
        </div>
    );
}

// Card visual primitives (.w-card-frame, .w-card-title, .w-card-badge,
// .w-card-badge-ghost) are defined in assets/src/styles/style.css and shared
// with templates/partials/library/video_list.html. See docs/uikit.html
// section 7 "Video Card".

export function WatchedBadge({ watched, onClick }) {
    if (watched) {
        return (
            <button
                type="button"
                onClick={onClick}
                class="w-card-badge top-2 right-2 text-green-400 uppercase tracking-wider"
                title={t('discover.unmarkWatched')}
            >
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                <span class="w-card-badge-label">{t('discover.watched')}</span>
            </button>
        );
    }
    return (
        <button
            type="button"
            onClick={onClick}
            class="w-card-badge-ghost top-2 right-2 hover:text-green-400"
            title={t('discover.markWatched')}
        >
            <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z" />
                <path d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
            </svg>
        </button>
    );
}

// WatchlistBadge is the heart toggle in the bottom-right of every IMDB card.
// Heart was chosen over bookmark to signal a positive, low-friction
// "interested in this" gesture — closer in feel to "like" than to a stiff
// reading-list bookmark. Pink (w-pinkL) is the Watchlist family colour,
// shared with btn-soft and the Delete-style ghost buttons.
export function WatchlistBadge({ inWatchlist, onClick }) {
    if (inWatchlist) {
        return (
            <button
                type="button"
                onClick={onClick}
                class="w-card-badge bottom-2 right-2 text-w-pinkL"
                title={t('discover.removeFromWatchlist')}
            >
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor"><path d="M11.645 20.91l-.007-.003-.022-.012a15.247 15.247 0 0 1-.383-.218 25.18 25.18 0 0 1-4.244-3.17C4.688 15.36 2.25 12.174 2.25 8.25 2.25 5.322 4.714 3 7.688 3A5.5 5.5 0 0 1 12 5.052 5.5 5.5 0 0 1 16.313 3c2.973 0 5.437 2.322 5.437 5.25 0 3.925-2.438 7.111-4.739 9.256a25.175 25.175 0 0 1-4.244 3.17 15.247 15.247 0 0 1-.383.219l-.022.012-.007.004-.003.001a.752.752 0 0 1-.704 0l-.003-.001Z"/></svg>
            </button>
        );
    }
    return (
        <button
            type="button"
            onClick={onClick}
            // !opacity-100 keeps the heart's circular pill always visible —
            // the regular w-card-badge-ghost pattern (Watched / Rated) hides
            // until card-hover, but for Watchlist we want the affordance to
            // be a constant invitation. The black backdrop on the ghost
            // pseudo-element gives the icon enough contrast over light
            // posters even at full opacity. Bang prefix beats the
            // .w-card-badge-ghost { opacity: 0 } base rule.
            class="w-card-badge-ghost bottom-2 right-2 !opacity-100 hover:text-w-pinkL"
            title={t('discover.addToWatchlist')}
        >
            <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12Z" />
            </svg>
        </button>
    );
}

export function RatingBadge({ rating, onClick }) {
    if (rating > 0) {
        return (
            <button
                type="button"
                onClick={onClick}
                class="w-card-badge top-2 left-2 text-yellow-400"
                title={t('discover.changeRating')}
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
            title={t('discover.rate')}
        >
            <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M11.48 3.499a.562.562 0 0 1 1.04 0l2.125 5.111a.563.563 0 0 0 .475.345l5.518.442c.499.04.701.663.321.988l-4.204 3.602a.563.563 0 0 0-.182.557l1.285 5.385a.562.562 0 0 1-.84.61l-4.725-2.885a.562.562 0 0 0-.586 0L6.982 20.54a.562.562 0 0 1-.84-.61l1.285-5.386a.562.562 0 0 0-.182-.557l-4.204-3.602a.562.562 0 0 1 .321-.988l5.518-.442a.563.563 0 0 0 .475-.345L11.48 3.5Z" />
            </svg>
        </button>
    );
}

function ItemCard({ item, showBadge, watched, rating, inWatchlist, onClick, onToggleWatched, onRate, onToggleWatchlist }) {
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

    const handleWatchlistClick = useCallback((e) => {
        e.stopPropagation();
        e.preventDefault();
        if (onToggleWatchlist) onToggleWatchlist(item);
    }, [item, onToggleWatchlist]);

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
                            {onToggleWatchlist && (
                                <WatchlistBadge inWatchlist={inWatchlist} onClick={handleWatchlistClick} />
                            )}
                        </>
                    )}
                </figure>
                <div class="p-3">
                    <h3 class="w-card-title">{item.name || t('discover.unknown')}</h3>
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
