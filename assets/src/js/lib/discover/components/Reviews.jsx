import { useEffect, useState } from 'preact/hooks';
import { fetchReviews } from '../reviewsClient';
import { ExpandableText } from './ExpandableText';
import { StarIcon } from './StarIcon';
import { t } from '../i18n';

// useReviews — fetches TMDB reviews for the modal's title id. Returns
// null while loading, then the (possibly empty) array. Non-IMDB ids
// resolve to [] without a network call; the per-id cache lives in
// reviewsClient.
export function useReviews(videoId, videoType) {
    const [reviews, setReviews] = useState(null);
    useEffect(() => {
        let alive = true;
        setReviews(null);
        if (!videoId) { setReviews([]); return; }
        fetchReviews(videoId, videoType).then(r => { if (alive) setReviews(r); });
        return () => { alive = false; };
    }, [videoId]);
    return reviews;
}

// ReviewsList — plain list of TMDB review cards, the content of the
// "Reviews (M)" tab in both the episodes and streams views of the
// stream modal.
export function ReviewsList({ reviews }) {
    if (!reviews || reviews.length === 0) return null;
    return (
        <div class="flex flex-col gap-3">
            {reviews.map((r, i) => (
                <div key={r.url || i} class="rounded-lg border border-w-line/50 p-3">
                    <div class="flex items-center gap-2 mb-1 min-w-0">
                        <span class="text-sm font-medium text-w-text truncate">{r.author || t('discover.unknown')}</span>
                        {r.rating > 0 && (
                            <span class="flex items-center gap-0.5 text-xs text-yellow-400 flex-shrink-0">
                                <StarIcon class="w-3 h-3" />
                                {Number(r.rating).toFixed(0)}
                            </span>
                        )}
                        {r.createdAt && (
                            <span class="text-xs text-w-muted ml-auto flex-shrink-0">
                                {new Date(r.createdAt).toLocaleDateString()}
                            </span>
                        )}
                    </div>
                    <ExpandableText
                        text={r.content}
                        lines={4}
                        textClass="text-xs text-w-sub leading-relaxed whitespace-pre-line"
                    />
                </div>
            ))}
        </div>
    );
}
