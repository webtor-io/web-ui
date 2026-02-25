import { useReducer, useRef, useEffect, useCallback, useState, useMemo } from 'preact/hooks';
import { StremioClient, CINEMETA_BASE } from '../client';
import {
    discoverReducer, initialState,
    buildCatalogs, getCatalogsForType, getTypes,
    getSearchTypes, getSearchResultsForType,
} from './discoverReducer';
import { StreamModal } from './StreamModal';

export function DiscoverApp({ addonUrls }) {
    const [state, dispatch] = useReducer(discoverReducer, initialState);
    const clientRef = useRef(null);
    const abortRef = useRef(null);
    const searchGenRef = useRef(0);
    const debounceRef = useRef(null);

    // Create client once
    if (!clientRef.current) {
        clientRef.current = new StremioClient(addonUrls);
    }
    const client = clientRef.current;

    function abortCatalog() {
        if (abortRef.current) {
            abortRef.current.abort();
            abortRef.current = null;
        }
    }

    // --- Load catalog ---
    const loadCatalog = useCallback(async (catalog, skip, currentItems) => {
        if (!catalog) return;
        dispatch({ type: 'CATALOG_LOADING' });
        abortCatalog();
        abortRef.current = new AbortController();
        const { signal } = abortRef.current;

        try {
            const data = await client.fetchCatalog(catalog.baseUrl, catalog.type, catalog.id, skip, { signal });
            let metas = data.metas || [];
            if (skip > 0) {
                const existingIds = new Set(currentItems.map(i => i.id));
                metas = metas.filter(m => !existingIds.has(m.id));
            }
            dispatch({ type: 'CATALOG_LOADED', items: skip > 0 ? metas : metas, append: skip > 0, hasMore: (data.metas || []).length > 0 });
            window.umami?.track('discover-catalog-loaded', { type: catalog.type, catalog: catalog.id });
        } catch (e) {
            if (e.name === 'AbortError') return;
            dispatch({ type: 'CATALOG_ERROR', message: 'Failed to load catalog. Please try again.' });
        }
    }, [client]);

    // --- Init ---
    useEffect(() => {
        let cancelled = false;
        (async () => {
            try {
                const manifests = await client.fetchAllManifests();
                if (cancelled) return;
                client.manifests = manifests;
                const catalogs = buildCatalogs(manifests);
                const types = getTypes(catalogs);

                if (!types.length) {
                    dispatch({ type: 'SET_PHASE', phase: 'no-catalogs' });
                    return;
                }

                const selectedType = types[0];
                const selectedCatalog = getCatalogsForType(catalogs, selectedType)[0] || null;
                dispatch({ type: 'INIT_SUCCESS', manifests, catalogs, selectedType, selectedCatalog });
            } catch (e) {
                if (!cancelled) {
                    dispatch({ type: 'INIT_ERROR', message: 'Failed to load addon manifests. Please try again.' });
                }
            }
        })();
        return () => { cancelled = true; abortCatalog(); };
    }, [client]);

    // Load catalog when selection changes after init
    const prevCatalogRef = useRef(null);
    useEffect(() => {
        if (state.phase !== 'ready' || state.isSearchMode) return;
        if (!state.selectedCatalog) return;
        const key = `${state.selectedCatalog.baseUrl}::${state.selectedCatalog.id}::${state.selectedType}`;
        if (prevCatalogRef.current === key && state.skip > 0) return;
        prevCatalogRef.current = key;
        loadCatalog(state.selectedCatalog, 0, []);
    }, [state.phase, state.selectedCatalog, state.selectedType, state.isSearchMode, loadCatalog]);

    // --- Type/Catalog selection ---
    const selectType = useCallback((type) => {
        abortCatalog();
        dispatch({ type: 'SELECT_TYPE', selectedType: type });
    }, []);

    const selectCatalog = useCallback((catalog) => {
        abortCatalog();
        dispatch({ type: 'SELECT_CATALOG', catalog });
    }, []);

    // --- Load more ---
    const loadMore = useCallback(() => {
        loadCatalog(state.selectedCatalog, state.items.length, state.items);
    }, [state.selectedCatalog, state.items, loadCatalog]);

    // --- Search ---
    const performSearch = useCallback(async (query) => {
        query = query.trim();
        if (query.length < 2) {
            if (state.isSearchMode) {
                dispatch({ type: 'EXIT_SEARCH' });
            }
            return;
        }

        const gen = ++searchGenRef.current;
        dispatch({ type: 'SEARCH_START', query });
        abortCatalog();
        abortRef.current = new AbortController();
        const { signal } = abortRef.current;

        const searchCatalogs = client.getSearchCatalogs();
        const sources = [];
        const hasCinemetaMovie = searchCatalogs.some(sc => sc.baseUrl === CINEMETA_BASE && sc.type === 'movie' && sc.id === 'top');
        const hasCinemetaSeries = searchCatalogs.some(sc => sc.baseUrl === CINEMETA_BASE && sc.type === 'series' && sc.id === 'top');
        if (!hasCinemetaMovie) sources.push({ baseUrl: CINEMETA_BASE, type: 'movie', id: 'top' });
        if (!hasCinemetaSeries) sources.push({ baseUrl: CINEMETA_BASE, type: 'series', id: 'top' });
        sources.push(...searchCatalogs);

        const results = await Promise.allSettled(
            sources.map(src => client.searchCatalog(src.baseUrl, src.type, src.id, query, { signal }))
        );

        if (gen !== searchGenRef.current) return;

        const seen = new Set();
        const merged = [];
        for (let k = 0; k < results.length; k++) {
            if (results[k].status !== 'fulfilled') continue;
            const srcType = sources[k].type;
            for (const item of results[k].value) {
                if (!item.type) item.type = srcType;
                if (!seen.has(item.id)) {
                    seen.add(item.id);
                    merged.push(item);
                }
            }
        }

        dispatch({ type: 'SEARCH_RESULTS', results: merged });
        window.umami?.track('discover-search', { query, count: merged.length });
    }, [client, state.isSearchMode]);

    const exitSearch = useCallback(() => {
        abortCatalog();
        dispatch({ type: 'EXIT_SEARCH' });
    }, []);

    // --- Card click ---
    const cardClick = useCallback(async (item) => {
        const type = item.type || state.selectedType;
        const id = item.id;

        if (type === 'series') {
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'loading', title: item.name, poster: item.poster, subtitle: 'Loading episodes...' } });
            try {
                const meta = await client.fetchMeta(type, id);
                if (meta?.videos?.length > 0) {
                    dispatch({ type: 'SHOW_MODAL', modal: { view: 'episodes', title: item.name, poster: item.poster, meta, itemId: id, itemType: type } });
                } else {
                    await loadStreams(type, id, item);
                }
            } catch (e) {
                await loadStreams(type, id, item);
            }
        } else {
            await loadStreams(type, id, item);
        }
    }, [client, state.selectedType]);

    const loadStreams = useCallback(async (type, id, item) => {
        dispatch({ type: 'SHOW_MODAL', modal: { view: 'loading', title: item.name, poster: item.poster, subtitle: 'Loading streams...' } });
        try {
            const streams = await client.fetchStreams(type, id);
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'streams', title: item.name, poster: item.poster, streams } });
            window.umami?.track('discover-streams-loaded', { type, id, count: streams.length });
        } catch (e) {
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'streams', title: item.name, poster: item.poster, streams: [] } });
        }
    }, [client]);

    const onEpisodeSelect = useCallback(async (episode, item) => {
        const type = item.itemType || 'series';
        const epId = episode.id || `${item.itemId}:${episode.season}:${episode.episode}`;
        const epName = `${item.title} - S${episode.season || '?'}E${episode.episode || '?'}`;
        await loadStreams(type, epId, { name: epName, poster: item.poster });
    }, [loadStreams]);

    const closeModal = useCallback(() => {
        dispatch({ type: 'CLOSE_MODAL' });
    }, []);

    const retry = useCallback(() => {
        dispatch({ type: 'SET_PHASE', phase: 'loading' });
        // Re-trigger init by remounting - simplest: reset client manifests
        client.manifests = null;
        // Force re-init
        prevCatalogRef.current = null;
        (async () => {
            try {
                const manifests = await client.fetchAllManifests();
                client.manifests = manifests;
                const catalogs = buildCatalogs(manifests);
                const types = getTypes(catalogs);
                if (!types.length) {
                    dispatch({ type: 'SET_PHASE', phase: 'no-catalogs' });
                    return;
                }
                const selectedType = types[0];
                const selectedCatalog = getCatalogsForType(catalogs, selectedType)[0] || null;
                dispatch({ type: 'INIT_SUCCESS', manifests, catalogs, selectedType, selectedCatalog });
            } catch (e) {
                dispatch({ type: 'INIT_ERROR', message: 'Failed to load addon manifests. Please try again.' });
            }
        })();
    }, [client]);

    // --- Derived data ---
    const types = useMemo(() => getTypes(state.catalogs), [state.catalogs]);
    const catalogsForType = useMemo(() => getCatalogsForType(state.catalogs, state.selectedType), [state.catalogs, state.selectedType]);

    const searchTypes = useMemo(() => getSearchTypes(state.searchResults), [state.searchResults]);
    const displayItems = useMemo(() => {
        if (!state.isSearchMode) return state.items;
        if (state.searchType === 'all') return state.searchResults;
        return getSearchResultsForType(state.searchResults, state.searchType);
    }, [state.isSearchMode, state.items, state.searchResults, state.searchType]);

    const showBadges = state.isSearchMode && state.searchType === 'all';

    // --- Render ---
    if (state.phase === 'loading') {
        return <LoadingSpinner />;
    }

    if (state.phase === 'no-addons') {
        return <NoAddons />;
    }

    if (state.phase === 'no-catalogs') {
        return <NoCatalogs />;
    }

    if (state.phase === 'error' && state.items.length === 0) {
        return <ErrorState message={state.errorMessage} onRetry={retry} />;
    }

    return (
        <div>
            <SearchBar
                onSearch={performSearch}
                onExit={exitSearch}
                isSearchMode={state.isSearchMode}
            />

            {state.isSearchMode ? (
                <SearchTabs
                    searchResults={state.searchResults}
                    searchTypes={searchTypes}
                    searchType={state.searchType}
                    onSelect={(t) => dispatch({ type: 'SELECT_SEARCH_TYPE', searchType: t })}
                />
            ) : (
                <>
                    <TypeTabs types={types} selectedType={state.selectedType} onSelect={selectType} />
                    <CatalogSelector catalogs={catalogsForType} selectedCatalog={state.selectedCatalog} onSelect={selectCatalog} />
                </>
            )}

            {(state.catalogLoading || state.searchLoading) && <LoadingSpinner />}

            {!state.catalogLoading && !state.searchLoading && state.isSearchMode && state.searchResults.length === 0 && state.searchQuery && (
                <NoResults query={state.searchQuery} />
            )}

            {!state.catalogLoading && !state.searchLoading && !state.isSearchMode && state.items.length === 0 && state.phase === 'ready' && !state.hasMore && (
                <p class="text-w-muted text-center col-span-full py-8">No items found.</p>
            )}

            <ItemGrid items={displayItems} showBadges={showBadges} onClick={cardClick} />

            {!state.isSearchMode && !state.catalogLoading && state.hasMore && state.items.length > 0 && (
                <LoadMore onLoadMore={loadMore} />
            )}

            {state.modal && (
                <StreamModal
                    modal={state.modal}
                    onClose={closeModal}
                    onEpisodeSelect={onEpisodeSelect}
                />
            )}
        </div>
    );
}

// --- Sub-components ---

function SearchBar({ onSearch, onExit, isSearchMode }) {
    const [value, setValue] = useState('');
    const timerRef = useRef(null);
    const inputRef = useRef(null);

    // Reset input when exiting search mode
    useEffect(() => {
        if (!isSearchMode && value) {
            setValue('');
        }
    }, [isSearchMode]);

    const handleInput = useCallback((e) => {
        const v = e.target.value;
        setValue(v);
        clearTimeout(timerRef.current);
        timerRef.current = setTimeout(() => onSearch(v), 400);
    }, [onSearch]);

    const handleKeyDown = useCallback((e) => {
        if (e.key === 'Escape') {
            setValue('');
            onExit();
            inputRef.current?.blur();
        }
    }, [onExit]);

    const handleClear = useCallback(() => {
        setValue('');
        onExit();
    }, [onExit]);

    return (
        <div class="relative mb-6">
            <div class="flex items-center bg-w-surface border border-w-line rounded-xl focus-within:border-w-cyan/50 transition-colors">
                <svg class="w-5 h-5 text-w-muted ml-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <circle cx="11" cy="11" r="8"></circle>
                    <path d="m21 21-4.3-4.3"></path>
                </svg>
                <input
                    ref={inputRef}
                    type="text"
                    placeholder="Search movies and series..."
                    class="w-full bg-transparent border-none outline-none px-3 py-3 text-w-text placeholder:text-w-muted text-sm"
                    autocomplete="off"
                    value={value}
                    onInput={handleInput}
                    onKeyDown={handleKeyDown}
                />
                {value.length > 0 && (
                    <button class="mr-3 p-1 text-w-muted hover:text-w-text transition-colors flex-shrink-0" type="button" onClick={handleClear}>
                        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M18 6 6 18M6 6l12 12"></path>
                        </svg>
                    </button>
                )}
            </div>
        </div>
    );
}

function TypeTabs({ types, selectedType, onSelect }) {
    if (!types.length) return null;
    return (
        <div class="flex gap-2 mb-4 flex-wrap">
            {types.map(type => (
                <button
                    key={type}
                    class={type === selectedType ? 'btn btn-sm bg-w-cyan/15 border border-w-cyan/30 text-w-cyan' : 'btn btn-sm btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan'}
                    onClick={() => onSelect(type)}
                >
                    {type.charAt(0).toUpperCase() + type.slice(1)}
                </button>
            ))}
        </div>
    );
}

function SearchTabs({ searchResults, searchTypes, searchType, onSelect }) {
    const tabs = useMemo(() => [
        { key: 'all', label: 'All', count: searchResults.length },
        ...searchTypes.map(t => ({
            key: t,
            label: t.charAt(0).toUpperCase() + t.slice(1),
            count: getSearchResultsForType(searchResults, t).length,
        })),
    ], [searchResults, searchTypes]);

    return (
        <div class="flex gap-2 mb-4 flex-wrap">
            {tabs.map(tab => (
                <button
                    key={tab.key}
                    class={tab.key === searchType ? 'btn btn-sm bg-w-cyan/15 border border-w-cyan/30 text-w-cyan' : 'btn btn-sm btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan'}
                    onClick={() => onSelect(tab.key)}
                >
                    {tab.label} ({tab.count})
                </button>
            ))}
        </div>
    );
}

function CatalogSelector({ catalogs, selectedCatalog, onSelect }) {
    if (catalogs.length <= 1) return null;

    const handleChange = useCallback((e) => {
        const parts = e.target.value.split('::');
        const match = catalogs.find(c => c.baseUrl === parts[0] && c.id === parts[1]);
        if (match) onSelect(match);
    }, [catalogs, onSelect]);

    return (
        <div class="mb-6">
            <select
                class="select select-sm bg-w-surface border-w-line text-w-text"
                onChange={handleChange}
                value={selectedCatalog ? `${selectedCatalog.baseUrl}::${selectedCatalog.id}` : ''}
            >
                {catalogs.map(cat => (
                    <option key={`${cat.baseUrl}::${cat.id}`} value={`${cat.baseUrl}::${cat.id}`}>
                        {cat.name} ({cat.addonName})
                    </option>
                ))}
            </select>
        </div>
    );
}

function ItemGrid({ items, showBadges, onClick }) {
    if (!items.length) return null;
    return (
        <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
            {items.map(item => (
                <ItemCard key={item.id} item={item} showBadge={showBadges} onClick={onClick} />
            ))}
        </div>
    );
}

function ItemCard({ item, showBadge, onClick }) {
    const handleClick = useCallback(() => onClick(item), [item, onClick]);

    return (
        <div class="group cursor-pointer" onClick={handleClick}>
            <div class="bg-w-card border border-w-line rounded-xl overflow-hidden hover:border-w-cyan/30 transition-all duration-300 flex flex-col w-full">
                <figure class="aspect-[2/3] overflow-hidden relative">
                    {item.poster ? (
                        <img
                            class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
                            src={item.poster}
                            alt={item.name || ''}
                            loading="lazy"
                        />
                    ) : (
                        <div class="w-full h-full bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 text-w-purpleL/60 flex items-center justify-center">
                            <div class="text-center font-bold text-lg p-3 line-clamp-3 drop-shadow-sm">
                                {item.name || 'Unknown'}
                            </div>
                        </div>
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

function LoadMore({ onLoadMore }) {
    return (
        <div class="text-center mt-8">
            <button class="btn btn-ghost border border-w-line btn-sm px-8" onClick={onLoadMore}>
                Load more
            </button>
        </div>
    );
}

function LoadingSpinner() {
    return (
        <div class="text-center py-16">
            <span class="loading loading-spinner loading-lg text-w-cyan"></span>
            <p class="text-w-sub mt-4">Loading catalogs...</p>
        </div>
    );
}

function NoAddons() {
    return (
        <div class="text-center py-16">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1" stroke="currentColor" class="w-16 h-16 text-w-muted/40 mx-auto mb-4">
                <path stroke-linecap="round" stroke-linejoin="round" d="M15.59 14.37a6 6 0 0 1-5.84 7.38v-4.8m5.84-2.58a14.98 14.98 0 0 0 6.16-12.12A14.98 14.98 0 0 0 9.631 8.41m5.96 5.96a14.926 14.926 0 0 1-5.841 2.58m-.119-8.54a6 6 0 0 0-7.381 5.84h4.8m2.581-5.84a14.927 14.927 0 0 0-2.58 5.84m2.699 2.7c-.103.021-.207.041-.311.06a15.09 15.09 0 0 1-2.448-2.448 14.9 14.9 0 0 1 .06-.312m-2.24 2.39a4.493 4.493 0 0 0-1.757 4.306 4.493 4.493 0 0 0 4.306-1.758M16.5 9a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0Z" />
            </svg>
            <p class="text-lg font-semibold text-w-sub mb-2">No addons configured</p>
            <p class="text-sm text-w-muted mb-6">Add Stremio addons in your profile to start discovering content.</p>
            <a class="btn btn-soft-cyan btn-sm px-5" href="/profile" data-async-target="main">Go to Profile</a>
        </div>
    );
}

function NoCatalogs() {
    return (
        <div class="text-center py-16">
            <p class="text-lg font-semibold text-w-sub mb-2">No catalogs available</p>
            <p class="text-sm text-w-muted mb-6">Your addons don't provide any catalogs. Try adding a catalog addon like Cinemeta.</p>
            <a class="btn btn-soft-cyan btn-sm px-5" href="/profile" data-async-target="main">Go to Profile</a>
        </div>
    );
}

function ErrorState({ message, onRetry }) {
    return (
        <div class="text-center py-16">
            <p class="text-lg font-semibold text-w-sub mb-2">Something went wrong</p>
            <p class="text-sm text-w-muted mb-6">{message || 'Could not load content.'}</p>
            <button class="btn btn-soft-cyan btn-sm px-5" onClick={onRetry}>Retry</button>
        </div>
    );
}

function NoResults({ query }) {
    return (
        <div class="text-center py-16">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1" stroke="currentColor" class="w-16 h-16 text-w-muted/40 mx-auto mb-4">
                <circle cx="11" cy="11" r="8"></circle>
                <path stroke-linecap="round" d="m21 21-4.3-4.3"></path>
            </svg>
            <p class="text-lg font-semibold text-w-sub mb-2">No results found</p>
            <p class="text-sm text-w-muted mb-6">No results found for "{query}". Try a different search term.</p>
        </div>
    );
}
