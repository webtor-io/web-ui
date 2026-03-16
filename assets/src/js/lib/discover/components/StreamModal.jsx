import { useRef, useEffect, useState, useMemo, useCallback } from 'preact/hooks';
import { rebindAsync } from '../../async';
import { initProgressLog } from '../../progressLog';
import { parseStreamName, extractInfoHash, extractFileIdx } from '../stream';
import { extractLanguages } from '../lang';
import { loadPrefs, savePrefs } from '../prefs';
import { chipClass } from './discoverUtils';

export function StreamModal({ modal, onClose, onEpisodeSelect, onStreamClick, onBackToEpisodes, onSeasonChange, hasCustomAddons, onSetupAddons }) {
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
            <div class="modal-box bg-w-card border border-w-line/50 rounded-2xl max-w-2xl">
                {onBackToEpisodes && (modal.view === 'streams' || modal.view === 'loading') && (
                    <button
                        class="btn btn-sm btn-ghost absolute left-2 top-2 text-w-muted hover:text-w-cyan gap-1 px-2"
                        onClick={onBackToEpisodes}
                    >
                        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M15 18l-6-6 6-6"/>
                        </svg>
                        Episodes
                    </button>
                )}
                <button
                    class="btn btn-sm btn-circle btn-ghost absolute right-2 top-2 text-w-muted hover:text-base-content"
                    onClick={handleClose}
                >
                    &#10005;
                </button>
                <div class={onBackToEpisodes && (modal.view === 'streams' || modal.view === 'loading') ? 'pt-8' : ''}>
                    <ModalBody modal={modal} onClose={handleClose} onEpisodeSelect={onEpisodeSelect} onStreamClick={onStreamClick} onSeasonChange={onSeasonChange} hasCustomAddons={hasCustomAddons} onSetupAddons={onSetupAddons} />
                </div>
            </div>
            <form method="dialog" class="modal-backdrop">
                <button>close</button>
            </form>
        </dialog>
    );
}

function ModalBody({ modal, onClose, onEpisodeSelect, onStreamClick, onSeasonChange, hasCustomAddons, onSetupAddons }) {
    if (modal.view === 'loading') {
        return (
            <div>
                <ModalHeader title={modal.title} poster={modal.poster} subtitle={modal.subtitle} />
                <p class="text-w-muted text-sm text-center py-6">{modal.subtitle || 'Loading...'}</p>
            </div>
        );
    }

    if (modal.view === 'fetching') {
        return <FetchingView modal={modal} />;
    }

    if (modal.view === 'progress') {
        return <ProgressView logUrl={modal.logUrl} title={modal.title} poster={modal.poster} fileIdx={modal.fileIdx} />;
    }

    if (modal.view === 'episodes') {
        return <EpisodePicker key={modal._seasonKey} modal={modal} onEpisodeSelect={onEpisodeSelect} defaultSeason={modal.defaultSeason} onSeasonChange={onSeasonChange} />;
    }

    if (modal.view === 'streams') {
        return <StreamContent modal={modal} onStreamClick={onStreamClick} hasCustomAddons={hasCustomAddons} onSetupAddons={onSetupAddons} />;
    }

    return null;
}

function ModalHeader({ title, poster, subtitle, extra }) {
    const [imgError, setImgError] = useState(false);

    return (
        <div class="flex gap-4 mb-4">
            {poster && !imgError ? (
                <img
                    src={poster}
                    alt={title || ''}
                    class="w-20 h-28 object-cover rounded-lg flex-shrink-0"
                    onError={() => setImgError(true)}
                />
            ) : (
                <div class="w-20 h-28 rounded-lg flex-shrink-0 bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 text-w-purpleL/60 flex items-center justify-center">
                    <div class="text-center font-bold text-[10px] p-1 line-clamp-3 drop-shadow-sm">
                        {title || 'Unknown'}
                    </div>
                </div>
            )}
            <div class="flex flex-col justify-center min-w-0">
                <h3 class="font-bold text-lg line-clamp-2">{title || 'Unknown'}</h3>
                {subtitle && <p class="text-sm text-w-muted mt-1">{subtitle}</p>}
                {extra}
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
            <ModalHeader title={title} poster={poster} subtitle="Preparing resource..." />
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

function FetchingView({ modal }) {
    const { title, poster, addons } = modal;
    const doneCount = addons.filter(a => a.status !== 'fetching').length;
    const subtitle = `Fetching streams... (${doneCount}/${addons.length})`;

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={subtitle} />
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
                            {addon.status === 'fetching' && `Fetching ${addon.host}...`}
                            {addon.status === 'done' && `${addon.host} — ${addon.count} stream${addon.count !== 1 ? 's' : ''}`}
                            {addon.status === 'error' && `Error fetching ${addon.host}`}
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

function StreamContent({ modal, onStreamClick, hasCustomAddons, onSetupAddons }) {
    const { title, poster, streams, error } = modal;

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
            return `${visibleCount} of ${total} stream${total !== 1 ? 's' : ''}`;
        }
        return `${total} stream${total !== 1 ? 's' : ''} found`;
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
        return (
            <div>
                <ModalHeader title={title} poster={poster} subtitle={subtitleText} />
                <div class="text-center py-6">
                    <p class="text-w-muted text-sm">
                        {error || 'No streams available for this title.'}
                    </p>
                    {!hasCustomAddons && (
                        <>
                            <p class="text-w-sub text-xs mt-2 mb-4">
                                Install streaming addons to get torrent streams.
                            </p>
                            <button
                                class="btn btn-ghost btn-sm border border-w-line hover:border-w-cyan/30 hover:text-w-cyan"
                                onClick={onSetupAddons}
                            >
                                Set up addons
                            </button>
                        </>
                    )}
                </div>
            </div>
        );
    }

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={subtitleText}
                extra={total4kCount > 0 && (
                    <Toggle4k show4k={show4k} count={total4kCount} onToggle={toggle4k}
                        showWarning={show4kWarning} onConfirm={confirm4k} onCancel={cancel4k} />
                )}
            />

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
                    All {total4kCount} stream{total4kCount !== 1 ? 's are' : ' is'} 4K. Enable "Include 4K" above to see them.
                </p>
            ) : (
                <>
                    <div class="flex flex-col gap-2 max-h-[400px] overflow-y-auto">
                        {filteredStreams.map(({ stream, parsed: info, visible }, i) => (
                            visible && <StreamRow key={i} stream={stream} info={info} onStreamClick={onStreamClick} />
                        ))}
                    </div>

                    {hasActiveFilters && visibleCount === 0 && (
                        <p class="text-w-muted text-sm text-center py-6">No streams match the selected filters.</p>
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
                    Include 4K
                    <span class="text-w-muted ml-0.5">({count})</span>
                </span>
                {!show4k && (
                    <span class="text-[10px] text-w-muted">(streaming may not work)</span>
                )}
            </label>

            {showWarning && (
                <div class="absolute left-0 top-full mt-1.5 z-50 bg-w-card border border-w-line rounded-xl shadow-lg p-3 w-64">
                    <p class="text-[10px] font-semibold text-w-text uppercase tracking-wide">4K streaming warning</p>
                    <p class="text-[11px] text-w-muted mt-0.5 leading-snug">
                        4K content is very large and video streaming will most likely not work. It may also require transcoding, which is disabled for 4K. Only download is recommended.
                    </p>
                    <div class="flex justify-between gap-1.5 mt-2">
                        <button
                            class="btn btn-ghost btn-xs text-w-muted"
                            onClick={onCancel}
                        >
                            Cancel
                        </button>
                        <button
                            class="btn btn-xs btn-ghost border border-red-400/30 text-red-400/70 hover:bg-red-400/10 hover:text-red-400"
                            onClick={onConfirm}
                        >
                            Show 4K
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
                <span class="text-xs text-w-muted flex-shrink-0">No torrent</span>
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

function EpisodePicker({ modal, onEpisodeSelect, defaultSeason, onSeasonChange }) {
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
                <ModalHeader title={title} poster={poster} subtitle="Select an episode" />
                <p class="text-w-muted text-sm text-center py-6">No episodes found.</p>
            </div>
        );
    }

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle="Select an episode" />

            {seasonNums.length > 1 && (
                <div class="flex gap-1.5 mb-3 flex-wrap">
                    {seasonNums.map(sn => (
                        <button
                            key={sn}
                            class={chipClass(sn === activeSeason, 'xs')}
                            onClick={() => { setActiveSeason(sn); if (onSeasonChange) onSeasonChange(sn); }}
                        >
                            {sn === 0 ? 'Specials' : `S${sn}`}
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
                                    {episode.title || episode.name || `Episode ${episode.episode || '?'}`}
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
