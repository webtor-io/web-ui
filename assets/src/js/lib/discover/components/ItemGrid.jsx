import { useCallback, useState } from 'preact/hooks';

export function ItemGrid({ items, showBadges, onClick }) {
    if (!items.length) return null;
    return (
        <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
            {items.map(item => (
                <ItemCard key={item.id} item={item} showBadge={showBadges} onClick={onClick} />
            ))}
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

function ItemCard({ item, showBadge, onClick }) {
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
                    {showBadge && item.type && (
                        <span class="absolute top-2 left-2 text-[10px] font-semibold uppercase px-1.5 py-0.5 rounded bg-black/60 text-white backdrop-blur-sm">
                            {item.type === 'series' ? 'Series' : item.type.charAt(0).toUpperCase() + item.type.slice(1)}
                        </span>
                    )}
                </figure>
                <div class="p-3">
                    <h3 class="font-semibold text-sm line-clamp-1 group-hover:text-w-cyan transition-colors">
                        {item.name || 'Unknown'}
                    </h3>
                    {(item.releaseInfo || item.year) && (
                        <span class="text-xs text-w-muted mt-1 block">
                            {item.releaseInfo || item.year || ''}
                        </span>
                    )}
                </div>
            </div>
        </div>
    );
}
