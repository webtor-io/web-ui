import { AIRecCard } from './AIRecCard';
import { tf } from '../../i18n';

// AIRecsGrid — chessboard layout for AI recommendations.
//
// Each item is a two-column row: one side holds the poster, the other side
// holds the title, metadata, and the Claude-generated "reason" block. Rows
// alternate which side the poster sits on, producing a zig-zag reading
// rhythm that feels editorial rather than catalog-like.
//
// On mobile (single column), alternation is dropped — poster sits on top,
// text below, full width. The `sm` breakpoint flips to the two-column
// chessboard.
//
// The grid is also the bridge between AI item shape ({video_id, title, ...})
// and the regular discover watched/rate callbacks which expect {id, type}.
// Per-item shim closures handle the translation so AIRecCard stays ignorant
// of the conversion.

function StarBadge({ rating }) {
    if (!rating || rating <= 0) return null;
    return (
        <span class="inline-flex items-center gap-1 text-xs text-yellow-400 font-semibold">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor">
                <path fill-rule="evenodd" d="M10.788 3.21c.448-1.077 1.976-1.077 2.424 0l2.082 5.006 5.404.434c1.164.093 1.636 1.545.749 2.305l-4.117 3.527 1.257 5.273c.271 1.136-.964 2.033-1.96 1.425L12 18.354 7.373 21.18c-.996.608-2.231-.29-1.96-1.425l1.257-5.273-4.117-3.527c-.887-.76-.415-2.212.749-2.305l5.404-.434 2.082-5.005Z" clip-rule="evenodd" />
            </svg>
            {rating.toFixed(1)}
        </span>
    );
}

function ReasonBlock({ item }) {
    return (
        <div class="flex flex-col gap-2 min-w-0">
            <div class="flex items-baseline gap-3 flex-wrap">
                <h3 class="text-lg sm:text-xl font-semibold text-w-text leading-tight">
                    {item.title || 'Unknown'}
                </h3>
                {item.year && <span class="text-sm text-w-muted tabular-nums">{item.year}</span>}
                <StarBadge rating={item.rating} />
            </div>
            {item.reason && (
                <p class="text-sm sm:text-base text-w-cyan/90 italic leading-relaxed border-l-2 border-w-cyan/50 pl-3 py-0.5">
                    {item.reason}
                </p>
            )}
            {item.plot && (
                <p class="text-xs sm:text-sm text-w-muted/90 line-clamp-3 leading-relaxed">
                    {item.plot}
                </p>
            )}
        </div>
    );
}

export function AIRecsGrid({
    items,
    onCardClick,
    userStatuses,
    onToggleWatched,
    onRate,
    initialVisible = 4,
    expanded = false,
    onExpand,
}) {
    if (!items || items.length === 0) return null;

    // Slice the visible window. When `expanded` is true (or there are
    // fewer items than the initial cap), show everything; otherwise show
    // only the first `initialVisible` and offer a "Show more" button.
    const visibleCount = expanded ? items.length : Math.min(initialVisible, items.length);
    const visible = items.slice(0, visibleCount);
    const hidden = items.length - visibleCount;

    return (
        <div class="flex flex-col gap-8 sm:gap-10 mt-6">
            {visible.map((item, i) => {
                // Even index → poster on the left; odd → poster on the
                // right. Below the `sm` breakpoint both columns stack, so
                // this ordering only matters on tablet+.
                const posterFirst = i % 2 === 0;

                const status = (userStatuses && userStatuses[item.video_id]) || {};
                // Shim the AI item shape into the {id, type} shape the
                // parent handlers were designed for. The closures capture
                // the item so AIRecCard doesn't need to know about the
                // translation — it just calls onToggleWatched()/onRate().
                const toggleWatched = onToggleWatched ? () => onToggleWatched(item) : undefined;
                const rate = onRate ? () => onRate(item) : undefined;

                return (
                    <article
                        key={item.video_id}
                        class="grid grid-cols-1 sm:grid-cols-[180px_1fr] md:grid-cols-[220px_1fr] gap-4 sm:gap-6 items-center"
                    >
                        <div class={posterFirst ? '' : 'sm:col-start-2 sm:row-start-1'}>
                            <AIRecCard
                                item={item}
                                onClick={onCardClick}
                                watched={!!status.watched}
                                rating={status.rating || 0}
                                onToggleWatched={toggleWatched}
                                onRate={rate}
                            />
                        </div>
                        <div class={posterFirst ? '' : 'sm:col-start-1 sm:row-start-1'}>
                            <ReasonBlock item={item} />
                        </div>
                    </article>
                );
            })}

            {hidden > 0 && (
                <div class="flex justify-center">
                    <button
                        type="button"
                        onClick={onExpand}
                        class="inline-flex items-center gap-2 rounded-full bg-w-cyan/10 text-w-cyan border border-w-cyan/30 hover:bg-w-cyan/20 hover:border-w-cyan/60 transition-colors px-5 py-2 text-sm font-medium cursor-pointer"
                    >
                        <span>{tf('discover.ai.showMore', hidden)}</span>
                        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <polyline points="6 9 12 15 18 9" />
                        </svg>
                    </button>
                </div>
            )}
        </div>
    );
}
