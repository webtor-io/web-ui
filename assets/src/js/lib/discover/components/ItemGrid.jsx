import { useCallback, useState } from 'preact/hooks';

export function ItemGrid({ items, showBadges, userStatuses, onClick }) {
    if (!items.length) return null;
    return (
        <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
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

function ItemCard({ item, showBadge, watched, rating, onClick }) {
    const handleClick = useCallback(() => onClick(item), [item, onClick]);
    const [imgError, setImgError] = useState(false);
    const onImgError = useCallback(() => setImgError(true), []);

    return (
        <div class="group cursor-pointer" onClick={handleClick}>
            <div class="bg-w-card border border-w-line rounded-xl overflow-hidden hover:border-w-cyan/30 transition-all duration-300 flex flex-col w-full">
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
                    {rating > 0 && (
                        <div class="absolute top-2 left-2 flex items-center gap-1 px-2 py-1 rounded-full bg-black/70 backdrop-blur-sm text-yellow-400 text-[10px] font-semibold shadow-lg" title="Your rating">
                            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="currentColor"><path fill-rule="evenodd" d="M10.788 3.21c.448-1.077 1.976-1.077 2.424 0l2.082 5.006 5.404.434c1.164.093 1.636 1.545.749 2.305l-4.117 3.527 1.257 5.273c.271 1.136-.964 2.033-1.96 1.425L12 18.354 7.373 21.18c-.996.608-2.231-.29-1.96-1.425l1.257-5.273-4.117-3.527c-.887-.76-.415-2.212.749-2.305l5.404-.434 2.082-5.005Z" clip-rule="evenodd" /></svg>
                            {rating}
                        </div>
                    )}
                    {watched && (
                        <div class="absolute top-2 right-2 flex items-center gap-1 px-2 py-1 rounded-full bg-black/70 backdrop-blur-sm text-green-400 text-[10px] font-semibold uppercase tracking-wider shadow-lg" title="Watched">
                            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                            Watched
                        </div>
                    )}
                </figure>
                <div class="p-3">
                    <h3 class="font-semibold text-sm line-clamp-1 group-hover:text-w-cyan transition-colors">
                        {item.name || 'Unknown'}
                    </h3>
                    <div class="flex items-center gap-1.5 mt-1">
                        {(item.releaseInfo || item.year) && (
                            <span class="text-xs text-w-muted">
                                {item.releaseInfo || item.year}
                            </span>
                        )}
                        {showBadge && item.type && (
                            <span class="text-[10px] text-w-muted/60 uppercase tracking-wider">
                                {item.type === 'series' ? 'series' : item.type}
                            </span>
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
