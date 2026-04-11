import { useCallback, useEffect, useRef, useState } from 'preact/hooks';
import * as aiClient from '../../aiClient';
import { currentLocale } from '../../aiClient';
import { AIChipsRow } from './AIChipsRow';
import { AIQueryInput } from './AIQueryInput';
import { AIRecsGrid } from './AIRecsGrid';

// AISection is the self-contained AI recommendations UI mounted above the
// regular catalog browser on /discover. It owns no catalog state — it just
// dispatches `SHOW_MODAL` into the parent discover reducer when a card is
// clicked, which hands control back to the existing StreamModal flow.
//
// The section hides itself entirely when the feature flag is off (the
// server returns 404 on /discover/ai/chips and we flip to `disabled`).

const COPY = {
    en: {
        title: 'AI Recommendations',
        betaBadge: 'Beta',
        refresh: 'Refresh chips',
        tryThese: 'Or try one of these:',
        loadingChips: 'Brewing suggestions…',
        // Both phases say the same "Finding films…" line — the only
        // visible difference is the running counter once items start
        // arriving. Keeps the loading copy continuous instead of jumping
        // from a Claude-specific message to a different label.
        streamingClaude: 'Finding films…',
        streamingResolve: (got, expected) =>
            expected > 0 ? `Finding films… (${got}/${expected})` : `Finding films… (${got})`,
        showingFor: 'Showing results for:',
        newSearch: '← New search',
        noResults: 'No matching films found. Try a different query.',
        quotaFreeTitle: 'Daily AI quota reached',
        quotaFreeBody: 'You\u2019ve used today\u2019s free AI request.',
        quotaPaidTitle: 'Daily AI quota reached',
        quotaPaidBody: 'You\u2019ve hit your daily cap. Try again tomorrow.',
        quotaFreeCTA: 'Become a supporter',
        // Microcopy under the CTA — concrete numbers + value prop. Falls
        // back to a generic line if the backend hasn't sent upgrade_quota
        // yet (older deploy / paid user shouldn't see this anyway).
        quotaFreeCTAHint: (n) => n > 0
            ? `Unlock ${n} requests per day and a smarter model`
            : 'Unlock more daily requests and a smarter model',
        retry: 'Try again',
        errorGeneric: 'Something went wrong. Try again in a moment.',
        // n / total — total may be null until the first chips load lands;
        // in that case fall back to "n left today".
        remainingQuota: (n, total) => (total != null && total > 0)
            ? `${n}/${total} left today`
            : `${n} left today`,
        // Used inside the quota-exceeded card; explicit "0 / N" so the
        // user understands the cap, not just that they're blocked.
        quotaCounter: (total) => (total != null && total > 0) ? `0 / ${total}` : '0',
        quotaResetIn: (rel) => `Resets ${rel}`,
        quotaResetSoon: 'Resets in less than a minute',
        quotaResetNow: 'Resetting now…',
    },
    ru: {
        title: 'AI-рекомендации',
        betaBadge: 'Beta',
        refresh: 'Обновить чипсы',
        tryThese: 'Или попробуй:',
        loadingChips: 'Подбираю идеи…',
        streamingClaude: 'Подбираю фильмы…',
        streamingResolve: (got, expected) =>
            expected > 0 ? `Подбираю фильмы… (${got}/${expected})` : `Подбираю фильмы… (${got})`,
        showingFor: 'Показываю:',
        newSearch: '← Новый поиск',
        noResults: 'Ничего не нашлось. Попробуй другой запрос.',
        quotaFreeTitle: 'Дневной лимит AI исчерпан',
        quotaFreeBody: 'Бесплатная рекомендация на сегодня использована.',
        quotaPaidTitle: 'Дневной лимит AI исчерпан',
        quotaPaidBody: 'Лимит на сегодня закончился. Попробуй завтра.',
        quotaFreeCTA: 'Поддержать проект',
        quotaFreeCTAHint: (n) => n > 0
            ? `${n} запросов в день и более умная модель`
            : 'Больше запросов в день и более умная модель',
        retry: 'Попробовать снова',
        errorGeneric: 'Что-то пошло не так. Попробуй ещё раз.',
        remainingQuota: (n, total) => (total != null && total > 0)
            ? `Осталось: ${n}/${total}`
            : `Осталось: ${n}`,
        quotaCounter: (total) => (total != null && total > 0) ? `0 / ${total}` : '0',
        quotaResetIn: (rel) => `Сбросится ${rel}`,
        quotaResetSoon: 'Сбросится меньше чем через минуту',
        quotaResetNow: 'Уже сбрасывается…',
    },
};

// formatRelativeReset turns a future unix-seconds timestamp into a
// localised "in 10h 30m" / "через 10ч 30мин" string. Returns:
//   - null  → resetAt is missing or already in the past (caller should
//             show the "now" copy or hide the line)
//   - 'soon' → less than 1 minute remaining (caller shows "soon" copy)
//   - string → formatted relative time
//
// We deliberately stop at hours+minutes precision; days don't happen for
// the daily-quota use case (max horizon is ~24h), and seconds would just
// look jittery on the once-a-minute tick.
function formatRelativeReset(resetAt, locale) {
    if (!resetAt || typeof resetAt !== 'number') return null;
    const deltaMs = resetAt * 1000 - Date.now();
    if (deltaMs <= 0) return null;
    if (deltaMs < 60_000) return 'soon';

    const totalMinutes = Math.floor(deltaMs / 60_000);
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;

    if (locale === 'ru') {
        if (hours > 0 && minutes > 0) return `через ${hours}ч ${minutes}мин`;
        if (hours > 0) return `через ${hours}ч`;
        return `через ${minutes}мин`;
    }
    if (hours > 0 && minutes > 0) return `in ${hours}h ${minutes}m`;
    if (hours > 0) return `in ${hours}h`;
    return `in ${minutes}m`;
}

// Skeleton used while chips are loading. Four pill shapes matching the
// real chip layout so the section doesn't jump on arrival.
function ChipsSkeleton() {
    return (
        <div class="flex flex-wrap gap-2 mt-3" aria-hidden="true">
            {[28, 22, 34, 20, 26].map((w, i) => (
                <div
                    key={i}
                    class="h-8 rounded-full bg-w-surface/60 animate-pulse"
                    style={{ width: `${w * 6}px` }}
                />
            ))}
        </div>
    );
}

// Skeleton used while recommendations are loading. Mirrors the chessboard
// layout used by the real grid, so the page doesn't jump when content lands.
function RecsSkeleton() {
    return (
        <div class="flex flex-col gap-8 sm:gap-10 mt-6" aria-hidden="true">
            {Array.from({ length: 3 }).map((_, i) => {
                const posterFirst = i % 2 === 0;
                return (
                    <div
                        key={i}
                        class="grid grid-cols-1 sm:grid-cols-[180px_1fr] md:grid-cols-[220px_1fr] gap-4 sm:gap-6 items-center"
                    >
                        <div class={`aspect-[2/3] rounded-xl bg-w-surface/60 animate-pulse ${posterFirst ? '' : 'sm:col-start-2 sm:row-start-1'}`} />
                        <div class={`space-y-3 ${posterFirst ? '' : 'sm:col-start-1 sm:row-start-1'}`}>
                            <div class="h-5 w-3/5 rounded bg-w-surface/60 animate-pulse" />
                            <div class="h-4 w-full rounded bg-w-surface/40 animate-pulse" />
                            <div class="h-4 w-4/5 rounded bg-w-surface/40 animate-pulse" />
                        </div>
                    </div>
                );
            })}
        </div>
    );
}

export function AISection({
    aiState,
    dispatch,
    onCardClick,
    userStatuses,
    onToggleWatched,
    onRate,
}) {
    const locale = currentLocale();
    const t = COPY[locale];
    const abortRef = useRef(null);
    const [refreshing, setRefreshing] = useState(false);

    // nowTick is a once-a-minute clock used by the quota-exceeded card to
    // re-render its "resets in 10h 30m" line. We only run the interval
    // while the card is actually visible (phase === 'quotaExceeded') so
    // idle pages don't burn a periodic timer for nothing.
    const [nowTick, setNowTick] = useState(() => Date.now());
    useEffect(() => {
        if (aiState.phase !== 'quotaExceeded') return undefined;
        // Refresh immediately when the card appears (in case the user
        // landed on it from a tab that's been backgrounded for hours).
        setNowTick(Date.now());
        const id = setInterval(() => setNowTick(Date.now()), 60_000);
        return () => clearInterval(id);
    }, [aiState.phase]);

    // Wrap the card-click / watched / rate handlers so Umami captures every
    // interaction that can originate from an AI recommendation. The regular
    // Discover handlers we chain into already track their own analytics for
    // the catalog flow; these wrappers add an `ai-*` event that lets us
    // measure the AI section's CTR independently.
    const trackedCardClick = useCallback((aiItem) => {
        window.umami?.track?.('ai-card-clicked', { id: aiItem.video_id });
        onCardClick?.(aiItem);
    }, [onCardClick]);

    const trackedToggleWatched = useCallback((aiItem) => {
        if (!onToggleWatched) return;
        const willBeWatched = !(userStatuses && userStatuses[aiItem.video_id]?.watched);
        window.umami?.track?.('ai-watched-toggled', { id: aiItem.video_id, on: willBeWatched });
        onToggleWatched(aiItem);
    }, [onToggleWatched, userStatuses]);

    const trackedRate = useCallback((aiItem) => {
        if (!onRate) return;
        window.umami?.track?.('ai-rate-opened', { id: aiItem.video_id });
        onRate(aiItem);
    }, [onRate]);

    // Abort any in-flight request on unmount.
    useEffect(() => () => abortRef.current?.abort?.(), []);

    // Initial chip load — fire exactly once on mount.
    //
    // We deliberately use an empty dependency array instead of
    // [aiState.phase === 'idle']. The latter looks natural ("only run while
    // we're idle") but has a subtle bug: the very first line of the effect
    // dispatches LOAD_CHIPS_START, which flips phase to 'loadingChips',
    // which flips the dep from [true] to [false], which makes React run
    // the effect's cleanup before the fetch lands. That cleanup would set
    // `cancelled = true` and the subsequent SUCCESS dispatch would be
    // silently dropped — JSON arrives, UI stays empty.
    //
    // With [] deps the effect runs once on mount, cleanup only fires on
    // unmount, and cancellation is handled by the separate unmount effect
    // above via abortRef.
    useEffect(() => {
        dispatch({ type: 'AI_LOAD_CHIPS_START' });
        const ac = new AbortController();
        abortRef.current = ac;
        (async () => {
            try {
                const data = await aiClient.fetchChips({ signal: ac.signal });
                dispatch({
                    type: 'AI_LOAD_CHIPS_SUCCESS',
                    chips: data.chips,
                    generatedAt: data.generated_at,
                    tier: data.tier,
                    remainingQuota: data.remaining_quota,
                    dailyQuota: data.daily_quota,
                });
                window.umami?.track?.('ai-chips-loaded', {
                    count: data.chips?.length || 0,
                    tier: data.tier,
                });
            } catch (err) {
                if (err.name === 'AbortError') return;
                if (err.status === 404) {
                    dispatch({ type: 'AI_DISABLED' });
                    window.umami?.track?.('ai-disabled');
                    return;
                }
                dispatch({ type: 'AI_LOAD_CHIPS_ERROR', error: { code: err.code, message: err.message } });
                window.umami?.track?.('ai-chips-error', { code: err.code });
            }
        })();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // streamRef holds the current EventSource handle so we can close it on
    // component unmount or when the user starts a new request mid-stream.
    const streamRef = useRef(null);

    // runRecommendation takes the `query` that actually hits Claude, plus an
    // optional `displayQuery` the UI shows in the "Showing for: ..." banner.
    // For a chip click, displayQuery is the chip's short human label while
    // the query itself is the long expanded prompt — without this split the
    // banner would spit back the entire instruction we fed Claude.
    //
    // Uses the streaming endpoints (SSE) so cards appear as the resolver
    // finishes them, rather than waiting for the whole 20-30s pipeline to
    // complete before painting anything.
    const runRecommendation = useCallback((query, isRefine = false, displayQuery = null) => {
        // Close any in-flight stream — user clicked something new before
        // the previous request finished.
        if (streamRef.current) {
            streamRef.current.close();
            streamRef.current = null;
        }

        dispatch({
            type: 'AI_STREAM_START',
            query,
            displayQuery: displayQuery || query,
            isRefine,
        });

        const callbacks = {
            onPhase: (data) => {
                dispatch({ type: 'AI_STREAM_PHASE', phase: data.phase, expected: data.expected });
            },
            onItem: (item) => {
                dispatch({ type: 'AI_STREAM_ITEM', item });
            },
            onDone: (data) => {
                dispatch({
                    type: 'AI_STREAM_DONE',
                    query,
                    remainingQuota: data.remaining_quota,
                    dailyQuota: data.daily_quota,
                    tier: data.tier,
                });
                window.umami?.track?.(isRefine ? 'ai-refine' : 'ai-recommend', {
                    items: data.total || 0,
                    tier: data.tier,
                });
                streamRef.current = null;
            },
            onError: (err) => {
                if (err?.code === 'quota_exceeded') {
                    dispatch({
                        type: 'AI_QUOTA_EXCEEDED',
                        tier: err.tier,
                        dailyQuota: err.dailyQuota,
                        upgradeQuota: err.upgradeQuota,
                        quotaResetAt: err.resetAt,
                    });
                    window.umami?.track?.('ai-quota-hit', {
                        tier: err.tier,
                        phase: isRefine ? 'refine' : 'recommend',
                    });
                } else {
                    dispatch({
                        type: 'AI_STREAM_ERROR',
                        error: { code: err?.code || 'unknown', message: err?.message || '' },
                    });
                    window.umami?.track?.('ai-recommend-error', {
                        code: err?.code || 'unknown',
                        phase: isRefine ? 'refine' : 'recommend',
                    });
                }
                streamRef.current = null;
            },
        };

        streamRef.current = isRefine
            ? aiClient.refineStream(query, aiState.conversation, callbacks)
            : aiClient.recommendStream(query, callbacks);
    }, [dispatch, aiState.conversation]);

    // Tear down any open EventSource on unmount.
    useEffect(() => () => {
        if (streamRef.current) {
            streamRef.current.close();
            streamRef.current = null;
        }
    }, []);

    const handleChipClick = useCallback((chip) => {
        window.umami?.track?.('ai-chip-clicked', { id: chip.id });
        // Send the long expanded query to Claude, show the short label in UI.
        runRecommendation(chip.query, false, chip.label);
    }, [runRecommendation]);

    const handleQuerySubmit = useCallback((q) => {
        window.umami?.track?.('ai-query-submitted');
        runRecommendation(q, false);
    }, [runRecommendation]);

    const handleRefineSubmit = useCallback((q) => {
        window.umami?.track?.('ai-refine-submitted');
        runRecommendation(q, true);
    }, [runRecommendation]);

    const handleNewSearch = useCallback(() => {
        window.umami?.track?.('ai-new-search');
        dispatch({ type: 'AI_RESET' });
    }, [dispatch]);

    const handleRetry = useCallback(() => {
        window.umami?.track?.('ai-retry');
        dispatch({ type: 'AI_RESET' });
    }, [dispatch]);

    const handleRefreshChips = useCallback(async () => {
        if (refreshing) return;
        setRefreshing(true);
        window.umami?.track?.('ai-chips-refresh-requested');
        const ac = new AbortController();
        abortRef.current = ac;
        try {
            const data = await aiClient.refreshChips({ signal: ac.signal });
            dispatch({
                type: 'AI_LOAD_CHIPS_SUCCESS',
                chips: data.chips,
                generatedAt: data.generated_at,
                tier: data.tier,
                remainingQuota: data.remaining_quota,
            });
            window.umami?.track?.('ai-chips-refreshed', { count: data.chips?.length || 0 });
        } catch (err) {
            if (err.name === 'AbortError') return;
            if (err.status === 402) {
                dispatch({
                    type: 'AI_QUOTA_EXCEEDED',
                    tier: err.tier,
                    dailyQuota: err.dailyQuota,
                    upgradeQuota: err.upgradeQuota,
                    quotaResetAt: err.resetAt,
                });
                window.umami?.track?.('ai-quota-hit', { tier: err.tier, phase: 'chips-refresh' });
                return;
            }
            dispatch({ type: 'AI_LOAD_CHIPS_ERROR', error: { code: err.code, message: err.message } });
            window.umami?.track?.('ai-chips-error', { code: err.code });
        } finally {
            setRefreshing(false);
        }
    }, [refreshing, dispatch]);

    if (aiState.phase === 'disabled') return null;

    const showQuotaCounter = Number.isFinite(aiState.remainingQuota) && aiState.remainingQuota >= 0;
    const busy =
        aiState.phase === 'loadingChips' ||
        aiState.phase === 'streamingClaude' ||
        aiState.phase === 'streamingResolve' ||
        refreshing;

    return (
        <section class="mb-6 rounded-2xl border border-w-cyan/25 bg-gradient-to-br from-w-cyan/[0.06] via-w-bg/40 to-w-purple/[0.04] p-4 sm:p-5">
            <header class="flex items-center justify-between flex-wrap gap-2">
                <div class="flex items-center gap-2">
                    <span class="text-xl">✨</span>
                    <h2 class="text-lg font-semibold text-w-text">{t.title}</h2>
                    <span class="inline-block px-2 py-0.5 rounded bg-w-cyan/10 text-w-cyan text-[10px] font-semibold uppercase tracking-wider">
                        {t.betaBadge}
                    </span>
                </div>
                <div class="flex items-center gap-3 text-xs text-w-muted">
                    {showQuotaCounter && (
                        <span class="tabular-nums">{t.remainingQuota(aiState.remainingQuota, aiState.dailyQuota)}</span>
                    )}
                    {/*
                        Manual chip refresh is intentionally hidden from the
                        UI to keep AI quota spending under control — it was
                        the cheapest way for users to burn a unit by accident.
                        Chips still rotate naturally on cache TTL expiry
                        (4h). The handler and endpoint remain wired up so
                        re-enabling the button is a one-line revert.

                    {aiState.phase === 'chipsReady' && (
                        <button
                            type="button"
                            onClick={handleRefreshChips}
                            disabled={busy}
                            class="inline-flex items-center gap-1 text-w-cyan hover:text-w-cyan/80 transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                            title={t.refresh}
                        >
                            <svg class={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <polyline points="23 4 23 10 17 10" /><polyline points="1 20 1 14 7 14" /><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
                            </svg>
                            <span class="hidden sm:inline">{t.refresh}</span>
                        </button>
                    )}
                    */}
                </div>
            </header>

            {/* Query input is always visible except when results are being
                streamed in / displayed (refine input takes its place) and
                in the quota-exceeded state. */}
            {aiState.phase !== 'quotaExceeded' &&
             aiState.phase !== 'recsReady' &&
             aiState.phase !== 'streamingClaude' &&
             aiState.phase !== 'streamingResolve' && (
                <div class="mt-3">
                    <AIQueryInput
                        mode="initial"
                        disabled={busy}
                        onSubmit={handleQuerySubmit}
                    />
                </div>
            )}

            {/* Phase-specific content. */}
            {aiState.phase === 'loadingChips' && <ChipsSkeleton />}

            {aiState.phase === 'chipsError' && (
                <div class="mt-3 text-sm text-w-muted">
                    {t.errorGeneric}
                    {' '}
                    <button
                        type="button"
                        onClick={handleRetry}
                        class="text-w-cyan underline cursor-pointer"
                    >
                        {t.retry}
                    </button>
                </div>
            )}

            {aiState.phase === 'chipsReady' && aiState.chips.length > 0 && (
                <>
                    <p class="mt-3 text-xs text-w-muted">{t.tryThese}</p>
                    <AIChipsRow chips={aiState.chips} onSelect={handleChipClick} disabled={busy} />
                </>
            )}

            {/* Streaming + ready states share the same shell — the only
                difference is whether the loader/skeleton is visible and
                whether the refine input is enabled. We render them in one
                block so the chessboard grid stays mounted as items stream
                in (no remount flicker). */}
            {(aiState.phase === 'streamingClaude' ||
              aiState.phase === 'streamingResolve' ||
              aiState.phase === 'recsReady') && (
                <>
                    <div class="mt-3 flex items-center justify-between flex-wrap gap-2">
                        <p class="text-xs text-w-muted italic line-clamp-2 min-w-0 flex-1">
                            {t.showingFor} <span class="text-w-cyan not-italic">{aiState.currentQuery}</span>
                        </p>
                        {aiState.phase === 'recsReady' && (
                            <button
                                type="button"
                                onClick={handleNewSearch}
                                class="text-xs text-w-cyan hover:underline cursor-pointer"
                            >
                                {t.newSearch}
                            </button>
                        )}
                    </div>

                    {/* Phase indicator. Replaced by the new-search button on
                        recsReady, so during streaming it sits in its own
                        line under the "Showing for" text. */}
                    {(aiState.phase === 'streamingClaude' || aiState.phase === 'streamingResolve') && (
                        <div class="mt-3 flex items-center gap-2 text-sm text-w-cyan">
                            <svg class="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <circle cx="12" cy="12" r="10" stroke-dasharray="60 20" />
                            </svg>
                            <span>
                                {aiState.phase === 'streamingClaude'
                                    ? t.streamingClaude
                                    : t.streamingResolve(aiState.recommendations.length, aiState.streamExpected)}
                            </span>
                        </div>
                    )}

                    {/* Cards: render whatever's in state. During
                        streamingClaude there's nothing yet so we show the
                        skeleton; during streamingResolve we show whatever
                        items have arrived plus a slimmer skeleton for the
                        remainder; on recsReady we just show the cards. */}
                    {aiState.phase === 'streamingClaude' && <RecsSkeleton />}

                    {aiState.recommendations.length > 0 && (
                        <AIRecsGrid
                            items={aiState.recommendations}
                            onCardClick={trackedCardClick}
                            userStatuses={userStatuses}
                            onToggleWatched={trackedToggleWatched}
                            onRate={trackedRate}
                            expanded={aiState.recsExpanded}
                            onExpand={() => {
                                window.umami?.track?.('ai-show-more');
                                dispatch({ type: 'AI_EXPAND_RECS' });
                            }}
                        />
                    )}

                    {aiState.phase === 'recsReady' && aiState.recommendations.length === 0 && (
                        <p class="mt-4 text-sm text-w-muted text-center py-8">{t.noResults}</p>
                    )}

                    {aiState.phase === 'recsReady' && (
                        <div class="mt-4 pt-4 border-t border-w-line/30">
                            <AIQueryInput
                                mode="refine"
                                disabled={busy}
                                onSubmit={handleRefineSubmit}
                            />
                        </div>
                    )}
                </>
            )}

            {aiState.phase === 'recsError' && (
                <div class="mt-4 text-center py-6">
                    <p class="text-sm text-w-muted mb-3">{t.errorGeneric}</p>
                    <button
                        type="button"
                        onClick={handleRetry}
                        class="btn btn-soft-cyan cursor-pointer"
                    >
                        {t.retry}
                    </button>
                </div>
            )}

            {aiState.phase === 'quotaExceeded' && (() => {
                // Recompute relative reset on every render — nowTick re-renders
                // us once a minute via the effect above, so the line stays
                // accurate while the card is visible.
                void nowTick;
                const rel = formatRelativeReset(aiState.quotaResetAt, locale);
                let resetLine = null;
                if (aiState.quotaResetAt) {
                    if (rel === 'soon') resetLine = t.quotaResetSoon;
                    else if (rel === null) resetLine = t.quotaResetNow;
                    else resetLine = t.quotaResetIn(rel);
                }
                return (
                    <div class="mt-4 rounded-xl border border-w-cyan/30 bg-w-cyan/5 p-6 sm:p-8 flex flex-col items-center justify-center text-center gap-3 min-h-[200px]">
                        <div class="text-3xl sm:text-4xl font-bold tabular-nums text-w-cyan">
                            {t.quotaCounter(aiState.dailyQuota)}
                        </div>
                        <h3 class="text-base sm:text-lg font-semibold text-w-text">
                            {aiState.tier === 'free' ? t.quotaFreeTitle : t.quotaPaidTitle}
                        </h3>
                        <p class="text-sm text-w-muted max-w-[420px]">
                            {aiState.tier === 'free' ? t.quotaFreeBody : t.quotaPaidBody}
                        </p>
                        {resetLine && (
                            <p class="text-xs text-w-cyan/80 tabular-nums">{resetLine}</p>
                        )}
                        {aiState.tier === 'free' && (
                            <div class="mt-1 flex flex-col items-center gap-1.5">
                                <a
                                    href="/donate"
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    class="btn btn-soft-cyan cursor-pointer"
                                    onClick={() => window.umami?.track?.('ai-upgrade-clicked')}
                                >
                                    {t.quotaFreeCTA}
                                </a>
                                <p class="text-[11px] text-w-muted">
                                    {t.quotaFreeCTAHint(aiState.upgradeQuota)}
                                </p>
                            </div>
                        )}
                    </div>
                );
            })()}
        </section>
    );
}
