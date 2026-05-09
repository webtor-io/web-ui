import { useRef, useEffect, useState, useMemo, useCallback } from 'preact/hooks';
import { rebindAsync } from '../../async';
import { initProgressLog } from '../../progressLog';
import { parseStreamName, extractInfoHash, extractFileIdx } from '../stream';
import { extractLanguages } from '../lang';
import { loadPrefs, savePrefs } from '../prefs';
import { chipClass } from './discoverUtils';
import { t, tf } from '../i18n';

export function StreamModal({ modal, onClose, onEpisodeSelect, onStreamClick, onBackToEpisodes, onSeasonChange, hasCustomAddons, onSetupAddons, onRetryStreams, userStatuses, watchlistIds, onToggleWatched, onRate, onToggleWatchlist }) {
    const dialogRef = useRef(null);

    useEffect(() => {
        const dialog = dialogRef.current;
        if (!dialog) return;
        if (modal) {
            if (!dialog.open) dialog.showModal();
        } else {
            dialog.close();
        }
    }, [modal]);

    // Rebind async link handlers after Preact renders new <a data-async-target> elements
    useEffect(() => {
        if (modal && dialogRef.current) rebindAsync(dialogRef.current);
    }, [modal]);

    // Handle close via backdrop or Escape
    const handleClose = useCallback(() => {
        onClose();
    }, [onClose]);

    if (!modal) return null;

    return (
        <dialog ref={dialogRef} class="modal" onClose={handleClose}>
            <div class="modal-box bg-w-card border border-w-line/50 rounded-2xl max-w-2xl max-h-[calc(100dvh-2rem)] flex flex-col overflow-hidden p-0">
                <div class="flex justify-between items-center shrink-0 px-2 sm:px-6 pt-2 sm:pt-4 pb-1 sm:pb-2">
                    {onBackToEpisodes && (modal.view === 'streams' || modal.view === 'loading') ? (
                        <button
                            class="btn btn-sm btn-ghost text-w-muted hover:text-w-cyan gap-1 px-2"
                            onClick={onBackToEpisodes}
                        >
                            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <path d="M15 18l-6-6 6-6"/>
                            </svg>
                            {t('discover.episodes')}
                        </button>
                    ) : <div />}
                    <button
                        class="btn btn-sm btn-circle btn-ghost text-w-muted hover:text-base-content"
                        onClick={handleClose}
                    >
                        &#10005;
                    </button>
                </div>
                <div class="overflow-y-auto px-3 sm:px-6 pb-4 sm:pb-6">
                    <ModalBody modal={modal} onClose={handleClose} onEpisodeSelect={onEpisodeSelect} onStreamClick={onStreamClick} onSeasonChange={onSeasonChange} hasCustomAddons={hasCustomAddons} onSetupAddons={onSetupAddons} onRetryStreams={onRetryStreams} userStatuses={userStatuses} watchlistIds={watchlistIds} onToggleWatched={onToggleWatched} onRate={onRate} onToggleWatchlist={onToggleWatchlist} />
                </div>
            </div>
            <form method="dialog" class="modal-backdrop">
                <button>close</button>
            </form>
        </dialog>
    );
}

function ModalBody({ modal, onClose, onEpisodeSelect, onStreamClick, onSeasonChange, hasCustomAddons, onSetupAddons, onRetryStreams, userStatuses, watchlistIds, onToggleWatched, onRate, onToggleWatchlist }) {
    const videoId = modal.metaId || modal.itemId;
    const videoType = modal.itemType;
    const isImdb = videoId && videoId.startsWith('tt') && !videoId.includes(':');
    const status = isImdb && videoType && userStatuses ? userStatuses[videoId] : null;
    const inWatchlist = !!(isImdb && watchlistIds && watchlistIds.has && watchlistIds.has(videoId));
    const statusButtons = isImdb && videoType ? (
        <WatchedRateButtons
            videoId={videoId}
            videoType={videoType}
            watched={status?.watched || false}
            rating={status?.rating || 0}
            inWatchlist={inWatchlist}
            onToggleWatched={onToggleWatched}
            onRate={onRate}
            onToggleWatchlist={onToggleWatchlist}
        />
    ) : null;
    const headerMeta = { year: modal.year || modal.releaseInfo, imdbRating: modal.imdbRating, description: modal.description };

    if (modal.view === 'loading') {
        return (
            <div>
                <ModalHeader title={modal.title} poster={modal.poster} subtitle={modal.subtitle} extra={statusButtons} {...headerMeta} />
                <p class="text-w-muted text-sm text-center py-6">{modal.subtitle || t('discover.loading')}</p>
            </div>
        );
    }

    if (modal.view === 'fetching') {
        return <FetchingView modal={modal} statusButtons={statusButtons} headerMeta={headerMeta} />;
    }

    if (modal.view === 'progress') {
        return <ProgressView logUrl={modal.logUrl} title={modal.title} poster={modal.poster} fileIdx={modal.fileIdx} />;
    }

    if (modal.view === 'episodes') {
        return <EpisodePicker key={modal._seasonKey} modal={modal} onEpisodeSelect={onEpisodeSelect} defaultSeason={modal.defaultSeason} onSeasonChange={onSeasonChange} statusButtons={statusButtons} headerMeta={headerMeta} />;
    }

    if (modal.view === 'streams') {
        return <StreamContent modal={modal} onStreamClick={onStreamClick} hasCustomAddons={hasCustomAddons} onSetupAddons={onSetupAddons} onRetryStreams={onRetryStreams} statusButtons={statusButtons} headerMeta={headerMeta} />;
    }

    return null;
}

function WatchedRateButtons({ videoId, videoType, watched, rating, inWatchlist, onToggleWatched, onRate, onToggleWatchlist }) {
    const handleWatched = useCallback((e) => {
        e.stopPropagation();
        if (onToggleWatched) onToggleWatched({ id: videoId, type: videoType });
    }, [videoId, videoType, onToggleWatched]);

    const handleRate = useCallback((e) => {
        e.stopPropagation();
        if (onRate) onRate({ id: videoId, type: videoType });
    }, [videoId, videoType, onRate]);

    const handleWatchlist = useCallback((e) => {
        e.stopPropagation();
        if (onToggleWatchlist) onToggleWatchlist({ id: videoId, type: videoType });
    }, [videoId, videoType, onToggleWatchlist]);

    // Mobile bumps the join group to btn-sm + larger icons for comfortable
    // touch targets (~36px tall, close to the 44px iOS guideline given the
    // inner icon's hit-box). Desktop stays btn-xs to keep the modal header
    // dense alongside the title and metadata.
    return (
        <div class="join mt-2 mb-1">
            {watched ? (
                <button type="button" onClick={handleWatched}
                    class="btn btn-ghost btn-sm sm:btn-xs join-item border border-green-500/20 text-green-400 hover:bg-green-500/10 whitespace-nowrap"
                    title={t('discover.unmarkWatched')}
                    aria-label={t('discover.unmarkWatched')}>
                    <svg class="w-5 h-5 sm:w-4 sm:h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                    <span class="hidden sm:inline">{t('discover.watched')}</span>
                </button>
            ) : (
                <button type="button" onClick={handleWatched}
                    class="btn btn-ghost btn-sm sm:btn-xs join-item border border-w-line text-w-sub hover:border-green-500/40 hover:text-green-400 whitespace-nowrap"
                    title={t('discover.markWatched')}
                    aria-label={t('discover.markWatched')}>
                    <svg class="w-5 h-5 sm:w-4 sm:h-4" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z" />
                        <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
                    </svg>
                    <span class="hidden sm:inline">{t('discover.watched')}</span>
                </button>
            )}
            {rating > 0 ? (
                <button type="button" onClick={handleRate}
                    class="btn btn-ghost btn-sm sm:btn-xs join-item border border-yellow-500/20 text-yellow-400 hover:bg-yellow-500/10 whitespace-nowrap"
                    title={t('discover.changeRating')}
                    aria-label={t('discover.changeRating')}>
                    <svg class="w-5 h-5 sm:w-4 sm:h-4" viewBox="0 0 24 24" fill="currentColor"><path fill-rule="evenodd" d="M10.788 3.21c.448-1.077 1.976-1.077 2.424 0l2.082 5.006 5.404.434c1.164.093 1.636 1.545.749 2.305l-4.117 3.527 1.257 5.273c.271 1.136-.964 2.033-1.96 1.425L12 18.354 7.373 21.18c-.996.608-2.231-.29-1.96-1.425l1.257-5.273-4.117-3.527c-.887-.76-.415-2.212.749-2.305l5.404-.434 2.082-5.005Z" clip-rule="evenodd" /></svg>
                    {rating}
                </button>
            ) : (
                <button type="button" onClick={handleRate}
                    class="btn btn-ghost btn-sm sm:btn-xs join-item border border-w-line text-w-sub hover:border-yellow-500/40 hover:text-yellow-400 whitespace-nowrap"
                    title={t('discover.rate')}
                    aria-label={t('discover.rate')}>
                    <svg class="w-5 h-5 sm:w-4 sm:h-4" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M11.48 3.499a.562.562 0 0 1 1.04 0l2.125 5.111a.563.563 0 0 0 .475.345l5.518.442c.499.04.701.663.321.988l-4.204 3.602a.563.563 0 0 0-.182.557l1.285 5.385a.562.562 0 0 1-.84.61l-4.725-2.885a.562.562 0 0 0-.586 0L6.982 20.54a.562.562 0 0 1-.84-.61l1.285-5.386a.562.562 0 0 0-.182-.557l-4.204-3.602a.562.562 0 0 1 .321-.988l5.518-.442a.563.563 0 0 0 .475-.345L11.48 3.5Z" />
                    </svg>
                    <span class="hidden sm:inline">{t('discover.rate')}</span>
                </button>
            )}
            {onToggleWatchlist && (
                inWatchlist ? (
                    <button type="button" onClick={handleWatchlist}
                        class="btn btn-ghost btn-sm sm:btn-xs join-item border border-w-pink/30 text-w-pinkL hover:bg-w-pink/10 hover:border-w-pink/40 whitespace-nowrap"
                        title={t('discover.removeFromWatchlist')}
                        aria-label={t('discover.removeFromWatchlist')}>
                        <svg class="w-5 h-5 sm:w-4 sm:h-4" viewBox="0 0 24 24" fill="currentColor"><path d="M11.645 20.91l-.007-.003-.022-.012a15.247 15.247 0 0 1-.383-.218 25.18 25.18 0 0 1-4.244-3.17C4.688 15.36 2.25 12.174 2.25 8.25 2.25 5.322 4.714 3 7.688 3A5.5 5.5 0 0 1 12 5.052 5.5 5.5 0 0 1 16.313 3c2.973 0 5.437 2.322 5.437 5.25 0 3.925-2.438 7.111-4.739 9.256a25.175 25.175 0 0 1-4.244 3.17 15.247 15.247 0 0 1-.383.219l-.022.012-.007.004-.003.001a.752.752 0 0 1-.704 0l-.003-.001Z"/></svg>
                        <span class="hidden sm:inline">{t('discover.watchlist.label')}</span>
                    </button>
                ) : (
                    <button type="button" onClick={handleWatchlist}
                        class="btn btn-ghost btn-sm sm:btn-xs join-item border border-w-line text-w-sub hover:border-w-pink/40 hover:text-w-pinkL whitespace-nowrap"
                        title={t('discover.addToWatchlist')}
                        aria-label={t('discover.addToWatchlist')}>
                        <svg class="w-5 h-5 sm:w-4 sm:h-4" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12Z" />
                        </svg>
                        <span class="hidden sm:inline">{t('discover.watchlist.label')}</span>
                    </button>
                )
            )}
        </div>
    );
}

function ModalHeader({ title, poster, subtitle, extra, afterDescription, year, imdbRating, description }) {
    const [imgError, setImgError] = useState(false);

    return (
        <div class="flex gap-3 sm:gap-5 mb-4">
            <div class="shrink-0 w-[100px] sm:w-[140px] aspect-[2/3] rounded-xl overflow-hidden border border-w-line/30 shadow-lg relative">
                <div class="absolute inset-0 bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 text-w-purpleL/60 flex items-center justify-center">
                    <div class="text-center font-bold text-sm p-3 line-clamp-3 drop-shadow-sm">
                        {title || t('discover.unknown')}
                    </div>
                </div>
                {poster && !imgError && (
                    <img
                        src={poster}
                        alt={title || ''}
                        class="absolute inset-0 w-full h-full object-cover"
                        onError={() => setImgError(true)}
                    />
                )}
            </div>
            <div class="flex flex-col justify-center min-w-0">
                <h3 class="font-bold text-lg line-clamp-2">{title || t('discover.unknown')}</h3>
                {(year || imdbRating) && (
                    <div class="flex flex-wrap items-center gap-2 mt-1 text-sm text-w-sub">
                        {year && <span>{year}</span>}
                        {imdbRating && (
                            <span class="flex items-center gap-1 text-yellow-400">
                                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor"><path fill-rule="evenodd" d="M10.788 3.21c.448-1.077 1.976-1.077 2.424 0l2.082 5.006 5.404.434c1.164.093 1.636 1.545.749 2.305l-4.117 3.527 1.257 5.273c.271 1.136-.964 2.033-1.96 1.425L12 18.354 7.373 21.18c-.996.608-2.231-.29-1.96-1.425l1.257-5.273-4.117-3.527c-.887-.76-.415-2.212.749-2.305l5.404-.434 2.082-5.005Z" clip-rule="evenodd" /></svg>
                                {parseFloat(imdbRating).toFixed(1)}
                            </span>
                        )}
                    </div>
                )}
                {extra}
                {description && <p class="text-sm text-w-sub leading-relaxed mt-1 line-clamp-3">{description}</p>}
                {afterDescription}
                {subtitle && <p class="text-sm text-w-muted mt-1">{subtitle}</p>}
            </div>
        </div>
    );
}

// --- Progress View ---

function ProgressView({ logUrl, title, poster, fileIdx }) {
    const containerRef = useRef(null);

    useEffect(() => {
        if (!logUrl || !containerRef.current) return;
        const form = containerRef.current.querySelector('form');
        if (!form) return;

        const sdk = initProgressLog(form);
        return () => sdk.destroy();
    }, [logUrl]);

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={t('discover.preparingResource')} />
            <div ref={containerRef}>
                {logUrl ? (
                    <form class="progress-alert" data-async-progress-log={logUrl} data-async-target="main">
                        {fileIdx != null && <input type="hidden" name="file-idx" value={fileIdx} />}
                        <div class="log-target"></div>
                    </form>
                ) : (
                    <div class="text-center py-4">
                        <span class="loading loading-spinner loading-md text-w-cyan"></span>
                    </div>
                )}
            </div>
        </div>
    );
}

// --- Fetching View (per-addon progress) ---

function FetchingView({ modal, statusButtons, headerMeta }) {
    const { title, poster, addons } = modal;
    const doneCount = addons.filter(a => a.status !== 'fetching').length;
    const subtitle = tf('discover.fetchingStreams', doneCount, addons.length);

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={subtitle} extra={statusButtons} {...headerMeta} />
            <div class="flex flex-col gap-2 py-2">
                {addons.map((addon, i) => (
                    <div key={i} class="flex items-center gap-3 px-3 py-2 rounded-lg border border-w-line/50">
                        {addon.status === 'fetching' && (
                            <span class="loading loading-spinner loading-xs text-w-cyan flex-shrink-0"></span>
                        )}
                        {addon.status === 'done' && (
                            <svg class="w-4 h-4 text-green-500 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                                <path d="M20 6L9 17l-5-5"/>
                            </svg>
                        )}
                        {addon.status === 'error' && (
                            <svg class="w-4 h-4 text-red-400 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                                <path d="M18 6L6 18M6 6l12 12"/>
                            </svg>
                        )}
                        <span class={`text-sm truncate ${addon.status === 'error' ? 'text-red-400' : addon.status === 'done' ? 'text-w-sub' : 'text-w-text'}`}>
                            {addon.status === 'fetching' && tf('discover.fetchingAddon', addon.host)}
                            {addon.status === 'done' && tf('discover.addonStreams', addon.host, addon.count)}
                            {addon.status === 'error' && tf('discover.errorFetchingAddon', addon.host)}
                        </span>
                    </div>
                ))}
            </div>
        </div>
    );
}

// --- Stream Content ---

function is4kStream(parsedInfo) {
    return parsedInfo.labels.some(l => l === '4K');
}

function StreamContent({ modal, onStreamClick, hasCustomAddons, onSetupAddons, onRetryStreams, statusButtons, headerMeta }) {
    const { title, poster, streams, error, failedAddons } = modal;
    const failed = failedAddons || [];
    const [retrying, setRetrying] = useState(false);
    const handleRetry = useCallback(async (e) => {
        e?.stopPropagation?.();
        if (retrying || !onRetryStreams) return;
        setRetrying(true);
        try { await onRetryStreams(); } finally { setRetrying(false); }
    }, [onRetryStreams, retrying]);

    const [show4k, setShow4k] = useState(() => {
        const prefs = loadPrefs();
        return prefs.show4k === true;
    });
    const [show4kWarning, setShow4kWarning] = useState(false);

    const parsed = useMemo(() => streams.map(s => parseStreamName(s.name)), [streams]);

    const streamLangs = useMemo(() =>
        streams.map(s => extractLanguages(s.title || '').map(l => l.name)),
        [streams]
    );

    // Count how many 4K streams exist (before any filtering)
    const total4kCount = useMemo(() => parsed.filter(p => is4kStream(p)).length, [parsed]);

    // Base streams: exclude 4K when toggle is off
    const { baseStreams, baseParsed, baseLangs } = useMemo(() => {
        if (show4k) {
            return { baseStreams: streams, baseParsed: parsed, baseLangs: streamLangs };
        }
        const indices = [];
        for (let i = 0; i < parsed.length; i++) {
            if (!is4kStream(parsed[i])) indices.push(i);
        }
        return {
            baseStreams: indices.map(i => streams[i]),
            baseParsed: indices.map(i => parsed[i]),
            baseLangs: indices.map(i => streamLangs[i]),
        };
    }, [streams, parsed, streamLangs, show4k]);

    const { allSources, allLabels, allLangs } = useMemo(() => {
        const sources = [];
        const labels = [];
        const seenLabelsLower = {};
        for (const info of baseParsed) {
            if (!sources.includes(info.source)) sources.push(info.source);
            for (const lbl of info.labels) {
                const lower = lbl.toLowerCase();
                if (!seenLabelsLower[lower]) {
                    seenLabelsLower[lower] = true;
                    labels.push(lbl);
                }
            }
        }
        const langs = [];
        const seenLangs = {};
        for (const s of baseStreams) {
            for (const lang of extractLanguages(s.title || '')) {
                if (!seenLangs[lang.name]) {
                    seenLangs[lang.name] = true;
                    langs.push(lang);
                }
            }
        }
        return { allSources: sources, allLabels: labels, allLangs: langs };
    }, [baseParsed, baseStreams]);

    const [activeSources, setActiveSources] = useState(() => {
        const prefs = loadPrefs();
        if (!prefs.sources) return {};
        const result = {};
        for (const src of prefs.sources) {
            if (allSources.includes(src)) result[src] = true;
        }
        return result;
    });

    const [activeLabels, setActiveLabels] = useState(() => {
        const prefs = loadPrefs();
        if (!prefs.labels) return {};
        const result = {};
        for (const lbl of prefs.labels) {
            if (allLabels.some(l => l.toLowerCase() === lbl.toLowerCase())) result[lbl] = true;
        }
        return result;
    });

    const [activeLang, setActiveLang] = useState(() => {
        const prefs = loadPrefs();
        if (!prefs.lang) return null;
        return allLangs.some(l => l.name === prefs.lang) ? prefs.lang : null;
    });

    const hasFilters = allSources.length > 1 || allLabels.length > 0 || allLangs.length > 1;

    const filteredStreams = useMemo(() => {
        const activeSrcKeys = Object.keys(activeSources);
        const activeLblKeys = Object.keys(activeLabels);
        if (!activeSrcKeys.length && !activeLblKeys.length && !activeLang) {
            return baseStreams.map((s, i) => ({ stream: s, parsed: baseParsed[i], langs: baseLangs[i], visible: true }));
        }
        return baseStreams.map((s, i) => {
            let show = true;
            if (activeSrcKeys.length > 0 && !activeSources[baseParsed[i].source]) show = false;
            if (show && activeLblKeys.length > 0) {
                const lblLower = baseParsed[i].labels.map(l => l.toLowerCase());
                if (!activeLblKeys.every(k => lblLower.includes(k.toLowerCase()))) show = false;
            }
            if (show && activeLang && !baseLangs[i].includes(activeLang)) show = false;
            return { stream: s, parsed: baseParsed[i], langs: baseLangs[i], visible: show };
        });
    }, [baseStreams, baseParsed, baseLangs, activeSources, activeLabels, activeLang]);

    const visibleCount = filteredStreams.filter(s => s.visible).length;
    const hasActiveFilters = Object.keys(activeSources).length > 0 || Object.keys(activeLabels).length > 0 || activeLang;

    const toggle4k = useCallback(() => {
        if (!show4k) {
            setShow4kWarning(true);
            window.umami?.track('discover-4k-toggle-attempt');
        } else {
            setShow4k(false);
            savePrefs({ show4k: false });
            window.umami?.track('discover-4k-disabled');
        }
    }, [show4k]);

    const confirm4k = useCallback(() => {
        setShow4k(true);
        setShow4kWarning(false);
        savePrefs({ show4k: true });
        window.umami?.track('discover-4k-enabled');
    }, []);

    const cancel4k = useCallback(() => {
        setShow4kWarning(false);
        window.umami?.track('discover-4k-cancelled');
    }, []);

    const subtitleText = useMemo(() => {
        const total = baseStreams.length;
        if (hasActiveFilters) {
            return tf('discover.streamsFiltered', visibleCount, total);
        }
        return tf('discover.streamsFound', total);
    }, [hasActiveFilters, visibleCount, baseStreams.length]);

    const toggleSource = useCallback((src) => {
        setActiveSources(prev => {
            const next = { ...prev };
            if (next[src]) delete next[src];
            else next[src] = true;
            savePrefs({ sources: Object.keys(next) });
            return next;
        });
    }, []);

    const toggleLabel = useCallback((lbl) => {
        setActiveLabels(prev => {
            const next = { ...prev };
            if (next[lbl]) delete next[lbl];
            else next[lbl] = true;
            savePrefs({ labels: Object.keys(next) });
            return next;
        });
    }, []);

    const toggleLang = useCallback((langName) => {
        setActiveLang(prev => {
            const next = prev === langName ? null : langName;
            savePrefs({ lang: next });
            return next;
        });
    }, []);

    if (streams.length === 0) {
        // When the empty result is caused (or partly caused) by addon
        // failures, surface that explicitly instead of the generic
        // "no streams" copy. Without this the user can't tell whether
        // the title genuinely has no streams or their Torrentio is down.
        if (failed.length > 0) {
            const onlyFailure = failed[0];
            const headline = failed.length === 1
                ? tf('discover.streamsFailedOne', onlyFailure.name || onlyFailure.host)
                : tf('discover.streamsFailedMany', failed.length);
            return (
                <div>
                    <ModalHeader title={title} poster={poster} subtitle={t('discover.streamsFailedTitle')} extra={statusButtons} {...headerMeta} />
                    <div class="text-center py-4">
                        <svg class="w-12 h-12 text-yellow-400/50 mx-auto mb-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"/>
                        </svg>
                        <p class="text-sm text-w-text mb-1">{headline}</p>
                        <p class="text-xs text-w-muted mb-4">{t('discover.streamsFailedBody')}</p>
                        {failed.length > 1 && (
                            <ul class="text-xs text-w-sub mb-4 inline-block text-left">
                                {failed.map(f => (
                                    <li key={f.host}>· {f.name || f.host}</li>
                                ))}
                            </ul>
                        )}
                        <div class="flex justify-center gap-2">
                            <button
                                class="btn btn-soft-cyan btn-sm"
                                onClick={handleRetry}
                                disabled={retrying}
                            >
                                {retrying && <span class="loading loading-spinner loading-xs"></span>}
                                {t('discover.retry')}
                            </button>
                        </div>
                    </div>
                </div>
            );
        }
        return (
            <div>
                <ModalHeader title={title} poster={poster} subtitle={subtitleText} extra={statusButtons} {...headerMeta} />
                <div class="text-center py-6">
                    <p class="text-w-muted text-sm">
                        {error || t('discover.noStreams')}
                    </p>
                    {!hasCustomAddons && (
                        <>
                            <p class="text-w-sub text-xs mt-2 mb-4">
                                {t('discover.installAddonsHint')}
                            </p>
                            <button
                                class="btn btn-ghost btn-sm border border-w-line hover:border-w-cyan/30 hover:text-w-cyan"
                                onClick={onSetupAddons}
                            >
                                {t('discover.setupAddonsBtn')}
                            </button>
                        </>
                    )}
                </div>
            </div>
        );
    }

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={subtitleText} {...headerMeta}
                extra={statusButtons}
                afterDescription={total4kCount > 0 ? (
                    <Toggle4k show4k={show4k} count={total4kCount} onToggle={toggle4k}
                        showWarning={show4kWarning} onConfirm={confirm4k} onCancel={cancel4k} />
                ) : undefined}
            />

            {failed.length > 0 && (
                <div class="mb-3 flex items-center gap-2 px-3 py-2 rounded-lg border border-yellow-500/30 bg-yellow-500/5">
                    <svg class="w-4 h-4 text-yellow-400 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"/>
                    </svg>
                    <span class="text-xs text-yellow-100/85 flex-1 min-w-0">
                        {failed.length === 1
                            ? tf('discover.streamsPartialFailureOne', failed[0].name || failed[0].host)
                            : tf('discover.streamsPartialFailureMany', failed.length)}
                    </span>
                    <button
                        class="btn btn-ghost btn-xs text-yellow-100/85 hover:bg-yellow-500/10 hover:text-yellow-100 flex-shrink-0"
                        onClick={handleRetry}
                        disabled={retrying}
                    >
                        {retrying && <span class="loading loading-spinner loading-xs"></span>}
                        {t('discover.retry')}
                    </button>
                </div>
            )}

            {hasFilters && (
                <FilterChips
                    allSources={allSources}
                    allLabels={allLabels}
                    allLangs={allLangs}
                    activeSources={activeSources}
                    activeLabels={activeLabels}
                    activeLang={activeLang}
                    onToggleSource={toggleSource}
                    onToggleLabel={toggleLabel}
                    onToggleLang={toggleLang}
                />
            )}

            {baseStreams.length === 0 && !show4k && total4kCount > 0 ? (
                <p class="text-w-muted text-sm text-center py-6">
                    {tf('discover.all4kStreams', total4kCount)}
                </p>
            ) : (
                <>
                    <div class="flex flex-col gap-2 max-h-[400px] overflow-y-auto">
                        {filteredStreams.map(({ stream, parsed: info, visible }, i) => (
                            visible && <StreamRow key={i} stream={stream} info={info} onStreamClick={onStreamClick} />
                        ))}
                    </div>

                    {hasActiveFilters && visibleCount === 0 && (
                        <p class="text-w-muted text-sm text-center py-6">{t('discover.noFilterMatch')}</p>
                    )}
                </>
            )}
        </div>
    );
}

function Toggle4k({ show4k, count, onToggle, showWarning, onConfirm, onCancel }) {
    return (
        <div class="mt-3 relative">
            <label class="flex items-center gap-1.5 cursor-pointer select-none">
                <input
                    type="checkbox"
                    checked={show4k}
                    onChange={onToggle}
                    class="toggle toggle-xs toggle-soft"
                />
                <span class="text-xs text-w-sub">
                    {t('discover.include4k')}
                    <span class="text-w-muted ml-0.5">({count})</span>
                </span>
                {!show4k && (
                    <span class="text-[10px] text-w-muted">{t('discover.mayNotWork')}</span>
                )}
            </label>

            {showWarning && (
                <div class="absolute left-0 top-full mt-1.5 z-dropdown bg-w-card border border-w-line rounded-xl shadow-lg p-3 w-64">
                    <p class="text-[10px] font-semibold text-w-text uppercase tracking-wide">{t('discover.warning4kTitle')}</p>
                    <p class="text-[11px] text-w-muted mt-0.5 leading-snug">
                        {t('discover.warning4kBody')}
                    </p>
                    <div class="flex justify-between gap-1.5 mt-2">
                        <button
                            class="btn btn-ghost btn-xs text-w-muted"
                            onClick={onCancel}
                        >
                            {t('discover.cancel')}
                        </button>
                        <button
                            class="btn btn-xs btn-ghost border border-red-400/30 text-red-400/70 hover:bg-red-400/10 hover:text-red-400"
                            onClick={onConfirm}
                        >
                            {t('discover.show4k')}
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}

function FilterChips({ allSources, allLabels, allLangs, activeSources, activeLabels, activeLang, onToggleSource, onToggleLabel, onToggleLang }) {
    return (
        <div class="flex flex-wrap gap-1.5 mb-3">
            {allSources.map(src => (
                <button
                    key={`src-${src}`}
                    class={chipClass(activeSources[src], 'xs')}
                    onClick={() => onToggleSource(src)}
                >
                    {src}
                </button>
            ))}
            {allLabels.map(lbl => (
                <button
                    key={`lbl-${lbl}`}
                    class={chipClass(activeLabels[lbl], 'xs')}
                    onClick={() => onToggleLabel(lbl)}
                >
                    {lbl}
                </button>
            ))}
            {allLangs.map(lang => (
                <button
                    key={`lang-${lang.name}`}
                    class={chipClass(activeLang === lang.name, 'xs')}
                    onClick={() => onToggleLang(lang.name)}
                >
                    {lang.flag} {lang.name}
                </button>
            ))}
        </div>
    );
}

const PLAY_ICON = (
    <svg class="w-4 h-4 text-w-cyan" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <polygon points="5 3 19 12 5 21 5 3"></polygon>
    </svg>
);

function StreamRow({ stream, info, onStreamClick }) {
    const infoHash = extractInfoHash(stream);
    const fileIdx = extractFileIdx(stream);
    const titleLines = (stream.title || '').split('\n').filter(Boolean);

    const content = (
        <>
            <div class="flex-shrink-0 w-8 h-8 rounded-full bg-w-cyan/10 flex items-center justify-center">
                {PLAY_ICON}
            </div>
            <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1.5 flex-wrap">
                    <span class="text-sm font-medium">{info.source}</span>
                    {info.labels.map(label => (
                        <span key={label} class="bg-w-cyan/10 text-w-cyan text-[10px] px-1.5 py-0.5 rounded font-medium">{label}</span>
                    ))}
                </div>
                {titleLines.map((line, i) => (
                    <div key={i} class="text-xs text-w-sub line-clamp-1">{line}</div>
                ))}
            </div>
            {!infoHash && (
                <span class="text-xs text-w-muted flex-shrink-0">{t('discover.noTorrent')}</span>
            )}
        </>
    );

    if (infoHash) {
        return (
            <div
                onClick={() => onStreamClick(infoHash, fileIdx)}
                class="cursor-pointer flex items-center gap-3 p-3 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all"
            >
                {content}
            </div>
        );
    }

    return (
        <div class="opacity-50 flex items-center gap-3 p-3 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all">
            {content}
        </div>
    );
}

// --- Episode Picker ---

function EpisodePicker({ modal, onEpisodeSelect, defaultSeason, onSeasonChange, statusButtons, headerMeta }) {
    const { title, poster, meta } = modal;
    const videos = meta?.videos || [];

    const { seasons, seasonNums } = useMemo(() => {
        const s = {};
        for (const v of videos) {
            const sn = v.season != null ? v.season : 0;
            if (!s[sn]) s[sn] = [];
            s[sn].push(v);
        }
        const nums = Object.keys(s).map(Number).sort((a, b) => {
            if (a === 0) return 1;
            if (b === 0) return -1;
            return a - b;
        });
        return { seasons: s, seasonNums: nums };
    }, [videos]);

    const [activeSeason, setActiveSeason] = useState(() => {
        if (defaultSeason != null && seasonNums.includes(Number(defaultSeason))) {
            return Number(defaultSeason);
        }
        return seasonNums[0] ?? 0;
    });

    // Sync activeSeason when defaultSeason changes (e.g. popstate — component already mounted, useState initializer won't re-run)
    useEffect(() => {
        if (defaultSeason != null && seasonNums.includes(Number(defaultSeason))) {
            setActiveSeason(Number(defaultSeason));
        } else if (defaultSeason == null) {
            setActiveSeason(seasonNums[0] ?? 0);
        }
    }, [defaultSeason]);

    const episodes = useMemo(() =>
        (seasons[activeSeason] || []).slice().sort((a, b) => (a.episode || 0) - (b.episode || 0)),
        [seasons, activeSeason]
    );

    if (!videos.length) {
        return (
            <div>
                <ModalHeader title={title} poster={poster} subtitle={t('discover.selectEpisode')} extra={statusButtons} {...headerMeta} />
                <p class="text-w-muted text-sm text-center py-6">{t('discover.noEpisodes')}</p>
            </div>
        );
    }

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={t('discover.selectEpisode')} extra={statusButtons} {...headerMeta} />

            {seasonNums.length > 1 && (
                <div class="flex gap-1.5 mb-3 flex-wrap">
                    {seasonNums.map(sn => (
                        <button
                            key={sn}
                            class={chipClass(sn === activeSeason, 'xs')}
                            onClick={() => { setActiveSeason(sn); if (onSeasonChange) onSeasonChange(sn); }}
                        >
                            {sn === 0 ? t('discover.specials') : `S${sn}`}
                        </button>
                    ))}
                </div>
            )}

            <div class="max-h-[350px] overflow-y-auto">
                <div class="flex flex-col gap-1.5">
                    {episodes.map(episode => (
                        <button
                            key={episode.id || `${episode.season}-${episode.episode}`}
                            class="flex items-center gap-3 p-2.5 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all w-full text-left cursor-pointer bg-transparent"
                            onClick={() => onEpisodeSelect(episode, modal)}
                        >
                            <span class="flex-shrink-0 w-8 h-8 rounded-full bg-w-cyan/10 flex items-center justify-center text-xs font-bold text-w-cyan">
                                {episode.episode != null ? String(episode.episode) : '?'}
                            </span>
                            <div class="min-w-0 flex-1">
                                <div class="text-sm font-medium line-clamp-1">
                                    {episode.title || episode.name || tf('discover.episodeLabel', episode.episode || '?')}
                                </div>
                                {(episode.released || episode.overview) && (
                                    <div class="text-xs text-w-muted line-clamp-1">
                                        {episode.released
                                            ? new Date(episode.released).toLocaleDateString()
                                            : (episode.overview || '')}
                                    </div>
                                )}
                            </div>
                            <span class="text-w-muted flex-shrink-0">
                                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <path d="M9 18l6-6-6-6"/>
                                </svg>
                            </span>
                        </button>
                    ))}
                </div>
            </div>
        </div>
    );
}
