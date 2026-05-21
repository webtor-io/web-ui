import { useEffect, useMemo, useRef, useState, useCallback } from 'preact/hooks';
import { t, tf } from '../i18n';
import { getLang } from '../../i18n';

// Window for episode visibility, in days, relative to local midnight.
// Start at today: the calendar is a "what's coming" surface, not a
// recap log — already-aired episodes are noise and push the actually-
// upcoming releases off-screen. +4 weeks of future covers a typical
// season pace without dragging in episodes that may shift in scheduling.
const WINDOW_PAST_DAYS = 0;
const WINDOW_FUTURE_DAYS = 28;

// Five-way concurrency for fetchMeta. Higher numbers blow Cinemeta's
// rate limits on cold caches; lower stretches the loading skeleton.
const CONCURRENCY = 5;

function dateKeyLocal(d) {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return `${y}-${m}-${day}`;
}

function parseLocalDateKey(key) {
    const [y, m, d] = key.split('-').map(Number);
    return new Date(y, m - 1, d);
}

function formatDayHeader(dateKey, lang) {
    const today = new Date(); today.setHours(0, 0, 0, 0);
    const yesterday = new Date(today); yesterday.setDate(today.getDate() - 1);
    const tomorrow = new Date(today); tomorrow.setDate(today.getDate() + 1);

    const d = parseLocalDateKey(dateKey);
    const formatted = d.toLocaleDateString(lang, { weekday: 'long', month: 'long', day: 'numeric' });

    if (dateKey === dateKeyLocal(today)) return tf('discover.calendar.todayLabel', formatted);
    if (dateKey === dateKeyLocal(tomorrow)) return tf('discover.calendar.tomorrowLabel', formatted);
    if (dateKey === dateKeyLocal(yesterday)) return tf('discover.calendar.yesterdayLabel', formatted);
    return formatted;
}

function seasonEpisodeLabel(video) {
    const s = Number(video.season);
    if (s === 0) return t('discover.specials');
    return `S${video.season}E${video.episode}`;
}

// Bare heart badge — same position as the grid-view WatchlistBadge but
// without the dark pill backdrop, because the calendar row already sits
// on a tinted surface. Imported grid-view badge gets a black backdrop
// (needed over poster art) which reads as a noisy pill here.
function CalendarHeart({ inWatchlist, onClick }) {
    return (
        <button
            type="button"
            onClick={onClick}
            class={`absolute bottom-2 right-2 inline-flex items-center justify-center w-8 h-8 cursor-pointer opacity-60 hover:opacity-100 transition-opacity duration-150 active:scale-95 ${inWatchlist
                ? 'text-w-pinkL'
                : 'text-w-sub hover:text-w-pinkL'}`}
            title={inWatchlist ? t('discover.removeFromWatchlist') : t('discover.addToWatchlist')}
            aria-label={inWatchlist ? t('discover.removeFromWatchlist') : t('discover.addToWatchlist')}
            aria-pressed={inWatchlist}
        >
            <svg class="w-5 h-5" viewBox="0 0 24 24" fill={inWatchlist ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12Z" />
            </svg>
        </button>
    );
}

function CardShell({ item, primary, secondary, onClick, inWatchlist, onToggleWatchlist }) {
    const isImdb = item.id && item.id.startsWith('tt');
    const showHeart = isImdb && !!onToggleWatchlist;
    const handleHeartClick = useCallback((e) => {
        e.stopPropagation();
        e.preventDefault();
        onToggleWatchlist?.(item);
    }, [item, onToggleWatchlist]);
    return (
        <div
            onClick={onClick}
            class="relative flex gap-3 items-center cursor-pointer bg-w-surface/40 hover:bg-w-surface/80 rounded-lg p-2 pr-12 border border-w-line/20 hover:border-w-cyan/40 transition"
        >
            <div class="shrink-0 w-12 sm:w-16 aspect-[2/3] rounded overflow-hidden bg-w-surface">
                {item.poster ? (
                    <img
                        src={item.poster}
                        alt={item.name || ''}
                        loading="lazy"
                        class="w-full h-full object-cover"
                    />
                ) : null}
            </div>
            <div class="min-w-0 flex-1">
                <div class="text-w-text font-medium line-clamp-1">{item.name || t('discover.unknown')}</div>
                <div class="text-xs sm:text-sm text-w-sub mt-0.5 flex items-center gap-2 min-w-0">
                    {primary}
                    {secondary}
                </div>
            </div>
            {showHeart && (
                <CalendarHeart inWatchlist={inWatchlist} onClick={handleHeartClick} />
            )}
        </div>
    );
}

function EpisodeRow({ item, video, meta, onClick, inWatchlist, onToggleWatchlist }) {
    const handleClick = useCallback(() => onClick(item, video, meta), [item, video, meta, onClick]);
    // Cinemeta puts the episode title on `name`; some other addons use
    // `title`. Fall back across both so we surface a title wherever it
    // exists.
    const epTitle = video.name || video.title;
    return (
        <CardShell
            item={item}
            onClick={handleClick}
            inWatchlist={inWatchlist}
            onToggleWatchlist={onToggleWatchlist}
            primary={<span class="font-mono text-w-cyan shrink-0">{seasonEpisodeLabel(video)}</span>}
            secondary={epTitle ? <span class="line-clamp-1 text-w-sub min-w-0">{epTitle}</span> : null}
        />
    );
}

// SeasonDropRow collapses a same-day burst of episodes (Netflix-style
// season drop) into a single card. Cinemeta stamps every episode of a
// "released all at once" season with the same `released` value, which
// would otherwise produce a wall of identical rows on that day. The user
// gets one entry pointing at the season; clicking opens the episodes
// modal so they can pick which one to play.
function SeasonDropRow({ item, season, meta, episodeCount, onClick, inWatchlist, onToggleWatchlist }) {
    const handleClick = useCallback(() => onClick(item, season, meta), [item, season, meta, onClick]);
    const seasonLabel = Number(season) === 0
        ? t('discover.specials')
        : tf('discover.calendar.seasonLabel', season);
    return (
        <CardShell
            item={item}
            onClick={handleClick}
            inWatchlist={inWatchlist}
            onToggleWatchlist={onToggleWatchlist}
            primary={<span class="font-mono text-w-cyan shrink-0">{seasonLabel}</span>}
            secondary={<span class="text-w-sub">{tf('discover.calendar.episodeCount', episodeCount)}</span>}
        />
    );
}

function CalendarDay({ dateKey, entries, lang, watchlistIds, onEpisodeClick, onSeasonDropClick, onToggleWatchlist }) {
    return (
        <section>
            <h3 class="text-w-text font-semibold text-sm sm:text-base py-2 border-b border-w-line/40">
                {formatDayHeader(dateKey, lang)}
            </h3>
            {/* Two-column layout on wide screens — each day's cards pair
                up so a Cinemeta page of 25 series doesn't drag into a
                single tall column. Single-entry days collapse to a flex
                row so the lone card spans the full width instead of
                leaving an empty right cell. Single column under lg keeps
                mobile readable. */}
            <div class={entries.length === 1
                ? 'flex flex-col gap-2 mt-3'
                : 'grid grid-cols-1 lg:grid-cols-2 gap-2 mt-3'}>
                {entries.map(entry => {
                    const inWatchlist = !!(watchlistIds && watchlistIds.has && watchlistIds.has(entry.item.id));
                    return entry.kind === 'season-drop' ? (
                        <SeasonDropRow
                            key={`drop:${entry.item.id}:${entry.season}`}
                            item={entry.item}
                            meta={entry.meta}
                            season={entry.season}
                            episodeCount={entry.episodeCount}
                            inWatchlist={inWatchlist}
                            onClick={onSeasonDropClick}
                            onToggleWatchlist={onToggleWatchlist}
                        />
                    ) : (
                        <EpisodeRow
                            key={`ep:${entry.item.id}:${entry.video.season}:${entry.video.episode}`}
                            item={entry.item}
                            meta={entry.meta}
                            video={entry.video}
                            inWatchlist={inWatchlist}
                            onClick={onEpisodeClick}
                            onToggleWatchlist={onToggleWatchlist}
                        />
                    );
                })}
            </div>
        </section>
    );
}

function CalendarSkeleton({ done, total }) {
    return (
        <div class="flex flex-col gap-6 py-2">
            {total > 0 && (
                <div class="text-sm text-w-muted">
                    {tf('discover.calendar.loading', done, total)}
                </div>
            )}
            {[0, 1, 2].map(i => (
                <div key={i}>
                    <div class="h-5 w-40 bg-w-surface/60 rounded animate-pulse mb-3" />
                    <div class="flex flex-col gap-2">
                        {[0, 1].map(j => (
                            <div key={j} class="flex gap-3 items-center p-2">
                                <div class="w-12 sm:w-16 aspect-[2/3] bg-w-surface/60 rounded animate-pulse" />
                                <div class="flex-1">
                                    <div class="h-4 bg-w-surface/60 rounded animate-pulse w-3/4 mb-2" />
                                    <div class="h-3 bg-w-surface/60 rounded animate-pulse w-1/2" />
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            ))}
        </div>
    );
}

function CalendarEmptyState() {
    return (
        <div class="text-center py-12 max-w-md mx-auto">
            <div class="inline-flex items-center justify-center w-16 h-16 rounded-full bg-w-cyan/10 text-w-cyan mb-4">
                <svg class="w-8 h-8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M6.75 3v2.25M17.25 3v2.25M3 18.75V7.5a2.25 2.25 0 0 1 2.25-2.25h13.5A2.25 2.25 0 0 1 21 7.5v11.25m-18 0A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75m-18 0v-7.5A2.25 2.25 0 0 1 5.25 9h13.5A2.25 2.25 0 0 1 21 11.25v7.5" />
                </svg>
            </div>
            <h3 class="text-base font-semibold text-w-text mb-2">{t('discover.calendar.emptyTitle')}</h3>
            <p class="text-sm text-w-muted">{t('discover.calendar.empty')}</p>
        </div>
    );
}

// CalendarView renders the current series-only displayItems as a date-
// grouped episode timeline. It owns the meta-fetch lifecycle (5-way
// concurrent, abortable, cached via StremioClient.cache) so the parent
// just passes items + a click handler.
//
// items[] may include non-series rows (catalog mixes happen) — we filter
// internally and quietly drop them. If after filtering there are no
// episodes in the visibility window, an empty state is rendered.
export function CalendarView({ items, client, watchlistIds, onEpisodeClick, onSeasonDropClick, onToggleWatchlist }) {
    const seriesItems = useMemo(
        () => (items || []).filter(i => i && i.type === 'series'),
        [items],
    );
    // null = initial / re-fetching, [] = done with no episodes in window,
    // [{dateKey, entries}, ...] = grouped results
    const [grouped, setGrouped] = useState(null);
    const [progress, setProgress] = useState({ done: 0, total: 0 });
    const shownRef = useRef(false);

    useEffect(() => {
        // Fire the impression event once per CalendarView mount. Episode
        // CTR is reported as (calendar-episode-click / calendar-shown).
        if (shownRef.current) return;
        shownRef.current = true;
        window.umami?.track?.('discover-calendar-shown', {
            series_count: seriesItems.length,
        });
        // Intentionally only on mount.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useEffect(() => {
        if (!seriesItems.length) {
            setGrouped([]);
            setProgress({ done: 0, total: 0 });
            return;
        }
        const abort = new AbortController();
        let cancelled = false;
        setGrouped(null);
        setProgress({ done: 0, total: seriesItems.length });

        const today = new Date();
        today.setHours(0, 0, 0, 0);
        const from = new Date(today); from.setDate(from.getDate() - WINDOW_PAST_DAYS);
        const to = new Date(today); to.setDate(to.getDate() + WINDOW_FUTURE_DAYS);

        // Carry the catalog index alongside each item so we can sort by
        // popularity (Cinemeta returns catalog items popularity-ordered;
        // user addons follow whatever order their catalog imposes —
        // either way, smaller index = higher rank). The parallel workers
        // shuffle completion order so we can't rely on push-order.
        const queue = seriesItems.map((item, popularityIndex) => ({ item, popularityIndex }));
        const fetched = [];

        const workers = Array.from({ length: CONCURRENCY }, async () => {
            while (queue.length && !cancelled) {
                const { item, popularityIndex } = queue.shift();
                try {
                    const meta = await client.fetchMeta('series', item.id, { signal: abort.signal });
                    if (cancelled) return;
                    if (meta?.videos?.length) {
                        fetched.push({ item, meta, videos: meta.videos, popularityIndex });
                    }
                } catch (e) { /* drop failures, keep going */ }
                if (!cancelled) {
                    setProgress(p => ({ ...p, done: p.done + 1 }));
                }
            }
        });

        Promise.all(workers).then(() => {
            if (cancelled) return;
            const flat = [];
            for (const { item, meta, videos, popularityIndex } of fetched) {
                for (const v of videos) {
                    if (!v.released) continue;
                    const d = new Date(v.released);
                    if (isNaN(d.getTime()) || d < from || d > to) continue;
                    flat.push({
                        item,
                        meta,
                        video: v,
                        dateKey: dateKeyLocal(d),
                        releasedAt: d.getTime(),
                        popularityIndex,
                    });
                }
            }
            // Collapse season-drops: multiple episodes of the same series
            // and season landing on the same calendar day become one entry.
            // Netflix/Amazon "all at once" releases produce 8-13 identical
            // rows otherwise — see Off Campus (2026-05-13 audit).
            const bucketMap = new Map();
            for (const f of flat) {
                const key = `${f.item.id}|${f.video.season}|${f.dateKey}`;
                if (!bucketMap.has(key)) bucketMap.set(key, []);
                bucketMap.get(key).push(f);
            }
            const entries = [];
            for (const group of bucketMap.values()) {
                if (group.length > 1) {
                    const ref = group[0];
                    entries.push({
                        kind: 'season-drop',
                        item: ref.item,
                        meta: ref.meta,
                        season: ref.video.season,
                        episodeCount: group.length,
                        dateKey: ref.dateKey,
                        releasedAt: ref.releasedAt,
                        popularityIndex: ref.popularityIndex,
                    });
                } else {
                    const f = group[0];
                    entries.push({
                        kind: 'episode',
                        item: f.item,
                        meta: f.meta,
                        video: f.video,
                        dateKey: f.dateKey,
                        releasedAt: f.releasedAt,
                        popularityIndex: f.popularityIndex,
                    });
                }
            }
            const byKey = new Map();
            for (const e of entries) {
                if (!byKey.has(e.dateKey)) byKey.set(e.dateKey, []);
                byKey.get(e.dateKey).push(e);
            }
            const groups = [...byKey.entries()]
                .sort((a, b) => a[0].localeCompare(b[0]))
                .map(([dateKey, dayEntries]) => ({
                    dateKey,
                    // Within a day: popular series first (smaller catalog
                    // index = higher rank). releasedAt is a stable
                    // tiebreaker for items that share the same rank.
                    entries: dayEntries.sort((a, b) => {
                        const ai = a.popularityIndex ?? Number.MAX_SAFE_INTEGER;
                        const bi = b.popularityIndex ?? Number.MAX_SAFE_INTEGER;
                        if (ai !== bi) return ai - bi;
                        return a.releasedAt - b.releasedAt;
                    }),
                }));
            setGrouped(groups);
        });

        return () => {
            cancelled = true;
            abort.abort();
        };
    }, [seriesItems, client]);

    const handleEpisodeClick = useCallback((item, video, meta) => {
        window.umami?.track?.('discover-calendar-episode-click', {
            item_id: item.id,
            season: String(video.season),
            episode: String(video.episode),
        });
        // Forward the meta already fetched in the worker pool so the
        // parent can build backToEpisodes without a second fetchMeta.
        onEpisodeClick(item, video, meta);
    }, [onEpisodeClick]);

    const handleSeasonDropClick = useCallback((item, season, meta) => {
        window.umami?.track?.('discover-calendar-season-drop-click', {
            item_id: item.id,
            season: String(season),
        });
        onSeasonDropClick(item, season, meta);
    }, [onSeasonDropClick]);

    const lang = getLang();

    if (grouped === null) {
        return <CalendarSkeleton done={progress.done} total={progress.total} />;
    }
    if (grouped.length === 0) {
        return <CalendarEmptyState />;
    }

    return (
        <div class="flex flex-col gap-6 mt-2">
            {grouped.map(group => (
                <CalendarDay
                    key={group.dateKey}
                    dateKey={group.dateKey}
                    entries={group.entries}
                    lang={lang}
                    watchlistIds={watchlistIds}
                    onEpisodeClick={handleEpisodeClick}
                    onSeasonDropClick={handleSeasonDropClick}
                    onToggleWatchlist={onToggleWatchlist}
                />
            ))}
        </div>
    );
}
