import { useReducer, useRef, useEffect, useCallback, useState, useMemo } from 'preact/hooks';
import { rebindAsync } from '../../async';
import { StremioClient, CINEMETA_BASE } from '../client';
import {
    discoverReducer, initialState,
    buildCatalogs, getCatalogsForType, getTypes,
    getSearchTypes, getSearchResultsForType,
} from './discoverReducer';
import { StreamModal } from './StreamModal';
import { loadPrefs, savePrefs } from '../prefs';

function setUrlParams(params) {
    const url = new URLSearchParams(window.location.search);
    for (const [k, v] of Object.entries(params)) {
        if (v != null && v !== '') url.set(k, String(v));
        else url.delete(k);
    }
    const search = url.toString() ? `?${url}` : '';
    const existing = window.history.state || {};
    window.history.replaceState(existing, '', window.location.pathname + search);
}

function removeUrlParam(key) {
    setUrlParams({ [key]: null });
}

export function DiscoverApp({ addonUrls }) {
    const [state, dispatch] = useReducer(discoverReducer, initialState);
    const clientRef = useRef(null);
    const abortRef = useRef(null);
    const searchGenRef = useRef(0);
    const debounceRef = useRef(null);
    const searchAfterInit = useRef(null);
    const restoredPageRef = useRef(0);
    const modalItemIdRef = useRef(null);
    const modalEpisodeRef = useRef(null); // { season, episode }
    const restoreInProgressRef = useRef(false);

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

    // --- Init (with URL state restoration) ---
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

                // Restore state from URL params and history.state
                const urlParams = new URLSearchParams(window.location.search);
                const urlType = urlParams.get('type');
                const urlSearch = urlParams.get('search');
                const urlSearchType = urlParams.get('search-type');
                const urlPage = parseInt(urlParams.get('page'), 10) || 0;
                const urlId = urlParams.get('id');
                const urlSeason = urlParams.get('season');
                const urlEpisode = urlParams.get('episode');
                const urlCatalogBase = urlParams.get('catalog-base');
                const urlCatalogId = urlParams.get('catalog-id');

                const prefs = loadPrefs();
                let selectedType = types[0];
                let selectedCatalog;

                // Type: URL > localStorage > default
                if (urlType && types.includes(urlType)) {
                    selectedType = urlType;
                } else if (prefs.type && types.includes(prefs.type)) {
                    selectedType = prefs.type;
                }

                selectedCatalog = getCatalogsForType(catalogs, selectedType)[0] || null;

                // Catalog: URL > localStorage > default
                if (urlCatalogBase && urlCatalogId) {
                    const match = getCatalogsForType(catalogs, selectedType)
                        .find(c => c.baseUrl === urlCatalogBase && c.id === urlCatalogId);
                    if (match) selectedCatalog = match;
                } else if (prefs.catalogBase && prefs.catalogId) {
                    const match = getCatalogsForType(catalogs, selectedType)
                        .find(c => c.baseUrl === prefs.catalogBase && c.id === prefs.catalogId);
                    if (match) selectedCatalog = match;
                }

                dispatch({ type: 'INIT_SUCCESS', manifests, catalogs, selectedType, selectedCatalog });

                // Store page/modal restore targets
                if (urlPage > 0) restoredPageRef.current = urlPage;
                if (urlId) modalItemIdRef.current = urlId;
                if (urlSeason != null && urlEpisode != null) {
                    modalEpisodeRef.current = { season: urlSeason, episode: urlEpisode };
                }

                // Enter search mode if URL has search param
                if (urlSearch) {
                    dispatch({ type: 'SEARCH_START', query: urlSearch });
                    if (urlSearchType && urlSearchType !== 'all') {
                        dispatch({ type: 'SELECT_SEARCH_TYPE', searchType: urlSearchType });
                    }
                    // Trigger actual search
                    searchAfterInit.current = urlSearch;
                }
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
        savePrefs({ type });
        dispatch({ type: 'SELECT_TYPE', selectedType: type });
    }, []);

    const selectCatalog = useCallback((catalog) => {
        abortCatalog();
        savePrefs({ catalogBase: catalog.baseUrl, catalogId: catalog.id });
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

    // Trigger search restored from URL after init
    useEffect(() => {
        if (state.phase !== 'ready') return;
        if (!searchAfterInit.current) return;
        const query = searchAfterInit.current;
        searchAfterInit.current = null;
        performSearch(query);
    }, [state.phase, performSearch]);

    // --- Card click & streams (defined before restore effects that use them) ---
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

    const cardClick = useCallback(async (item) => {
        const type = item.type || state.selectedType;
        const id = item.id;

        setUrlParams({ id, season: null, episode: null });

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
    }, [client, state.selectedType, loadStreams]);

    const openModalById = useCallback(async (id) => {
        // Try to find the item in loaded items or search results
        const item = state.items.find(i => i.id === id)
            || state.searchResults.find(i => i.id === id);
        const ep = modalEpisodeRef.current;
        modalEpisodeRef.current = null;

        if (ep) {
            // Restore directly to streams for a specific episode, fetching meta for back-nav
            const type = item?.type || state.selectedType || 'series';
            const epId = `${id}:${ep.season}:${ep.episode}`;
            const name = item?.name || id;
            const epName = `${name} - ${Number(ep.season) === 0 ? 'Specials' : `S${ep.season}`} E${ep.episode}`;
            const poster = item?.poster;
            setUrlParams({ id, season: ep.season, episode: ep.episode });

            dispatch({ type: 'SHOW_MODAL', modal: { view: 'loading', title: epName, poster, subtitle: 'Loading streams...' } });

            // Fetch meta in parallel with streams so we can offer "back to episodes"
            const [streamsResult, metaResult] = await Promise.allSettled([
                client.fetchStreams(type, epId),
                client.fetchMeta(type, id),
            ]);

            const streams = streamsResult.status === 'fulfilled' ? streamsResult.value : [];
            const meta = metaResult.status === 'fulfilled' ? metaResult.value : null;

            const backToEpisodes = meta?.videos?.length > 0 ? {
                title: name, poster, meta, itemId: id, itemType: type, season: ep.season,
            } : undefined;

            dispatch({ type: 'SHOW_MODAL', modal: { view: 'streams', title: epName, poster, streams, backToEpisodes } });
            if (streams.length > 0) {
                window.umami?.track('discover-streams-loaded', { type, id: epId, count: streams.length });
            }
        } else if (item) {
            cardClick(item);
        } else {
            // Item not in loaded data — open with minimal info, modal will load via API
            cardClick({ id, name: id, type: state.selectedType });
        }
    }, [client, state.items, state.searchResults, state.selectedType, cardClick, loadStreams]);

    const onEpisodeSelect = useCallback(async (episode, item) => {
        const type = item.itemType || 'series';
        const epId = episode.id || `${item.itemId}:${episode.season}:${episode.episode}`;
        const epName = `${item.title} - ${Number(episode.season) === 0 ? 'Specials' : `S${episode.season || '?'}`} E${episode.episode || '?'}`;
        setUrlParams({ season: episode.season, episode: episode.episode });

        const backToEpisodes = {
            title: item.title,
            poster: item.poster,
            meta: item.meta,
            itemId: item.itemId,
            itemType: item.itemType,
            season: episode.season,
        };

        dispatch({ type: 'SHOW_MODAL', modal: { view: 'loading', title: epName, poster: item.poster, subtitle: 'Loading streams...', backToEpisodes } });
        try {
            const streams = await client.fetchStreams(type, epId);
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'streams', title: epName, poster: item.poster, streams, backToEpisodes } });
            window.umami?.track('discover-streams-loaded', { type, id: epId, count: streams.length });
        } catch (e) {
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'streams', title: epName, poster: item.poster, streams: [], backToEpisodes } });
        }
    }, [client]);

    const onBackToEpisodes = useCallback(() => {
        const back = state.modal?.backToEpisodes;
        if (!back) return;
        setUrlParams({ season: null, episode: null });
        dispatch({
            type: 'SHOW_MODAL',
            modal: {
                view: 'episodes',
                title: back.title,
                poster: back.poster,
                meta: back.meta,
                itemId: back.itemId,
                itemType: back.itemType,
                defaultSeason: back.season != null ? Number(back.season) : undefined,
            },
        });
    }, [state.modal]);

    const handleStreamClick = useCallback(async (infoHash, fileIdx) => {
        const currentTitle = state.modal?.title;
        const currentPoster = state.modal?.poster;

        dispatch({ type: 'SHOW_MODAL', modal: {
            view: 'progress', title: currentTitle, poster: currentPoster, logUrl: null, fileIdx: fileIdx != null ? fileIdx : null,
        }});

        try {
            const formData = new FormData();
            formData.append('resource', infoHash);
            formData.append('_csrf', window._CSRF);
            const response = await fetch('/', {
                method: 'POST',
                body: formData,
                headers: {
                    'Accept': 'application/json',
                    'X-CSRF-TOKEN': window._CSRF,
                },
            });

            if (!response.ok) throw new Error('POST failed');
            const data = await response.json();
            const logUrl = data.job_log_url;
            if (!logUrl) throw new Error('No job log URL');

            dispatch({ type: 'SHOW_MODAL', modal: {
                view: 'progress', title: currentTitle, poster: currentPoster, logUrl, fileIdx: fileIdx != null ? fileIdx : null,
            }});
        } catch (e) {
            dispatch({ type: 'SHOW_MODAL', modal: {
                view: 'streams', title: currentTitle, poster: currentPoster, streams: [],
            }});
        }
    }, [state.modal]);

    const closeModal = useCallback(() => {
        dispatch({ type: 'CLOSE_MODAL' });
        setUrlParams({ id: null, season: null, episode: null });
    }, []);

    // --- Restore pages from URL: after each catalog load, if pages remain, trigger another loadMore ---
    useEffect(() => {
        if (state.phase !== 'ready') return;
        if (state.catalogLoading) return;
        if (state.isSearchMode) return;
        if (state.items.length === 0) return;

        if (restoredPageRef.current > 0 && state.hasMore) {
            restoredPageRef.current--;
            restoreInProgressRef.current = true;
            loadCatalog(state.selectedCatalog, state.items.length, state.items);
            return;
        }

        // Pages done (or were 0). Restore modal if needed.
        if (restoreInProgressRef.current) {
            restoreInProgressRef.current = false;
        }
        if (modalItemIdRef.current) {
            const id = modalItemIdRef.current;
            modalItemIdRef.current = null;
            openModalById(id);
        }
    }, [state.phase, state.catalogLoading, state.isSearchMode, state.items.length, loadCatalog, openModalById]);

    // Restore modal from URL after search results load
    useEffect(() => {
        if (state.phase !== 'ready') return;
        if (!state.isSearchMode) return;
        if (state.searchLoading) return;
        if (!modalItemIdRef.current) return;
        if (state.searchResults.length === 0) return;

        const id = modalItemIdRef.current;
        modalItemIdRef.current = null;
        openModalById(id);
    }, [state.phase, state.isSearchMode, state.searchLoading, state.searchResults.length, openModalById]);

    // Sync state to URL
    useEffect(() => {
        if (state.phase !== 'ready') return;
        // Don't overwrite URL during page/modal restore
        if (restoreInProgressRef.current) return;

        const params = {};
        if (state.isSearchMode) {
            if (state.searchQuery) params.search = state.searchQuery;
            if (state.searchType && state.searchType !== 'all') params['search-type'] = state.searchType;
        } else {
            if (state.selectedType) params.type = state.selectedType;
            if (state.selectedCatalog) {
                params['catalog-base'] = state.selectedCatalog.baseUrl;
                params['catalog-id'] = state.selectedCatalog.id;
            }
            if (state.page > 0) params.page = state.page;
        }

        // Preserve modal params if present
        const currentUrl = new URLSearchParams(window.location.search);
        const currentId = currentUrl.get('id');
        if (currentId) params.id = currentId;
        const currentSeason = currentUrl.get('season');
        const currentEpisode = currentUrl.get('episode');
        if (currentSeason != null) params.season = currentSeason;
        if (currentEpisode != null) params.episode = currentEpisode;

        const url = new URLSearchParams();
        for (const [k, v] of Object.entries(params)) {
            if (v != null && v !== '') url.set(k, String(v));
        }
        const search = url.toString() ? `?${url}` : '';
        const newUrl = window.location.pathname + search;

        const existingState = window.history.state || {};
        window.history.replaceState(existingState, '', newUrl);
    }, [state.phase, state.selectedType, state.selectedCatalog, state.isSearchMode, state.searchQuery, state.searchType, state.page]);

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
                initialQuery={state.searchQuery}
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
                    onStreamClick={handleStreamClick}
                    onBackToEpisodes={state.modal.backToEpisodes ? onBackToEpisodes : undefined}
                />
            )}
        </div>
    );
}

// --- Sub-components ---

function SearchBar({ onSearch, onExit, isSearchMode, initialQuery }) {
    const [value, setValue] = useState('');
    const timerRef = useRef(null);
    const inputRef = useRef(null);

    // Sync input value with initialQuery (restored from URL)
    useEffect(() => {
        if (initialQuery && !value) {
            setValue(initialQuery);
        }
    }, [initialQuery]);

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
    const ref = useRef(null);
    useEffect(() => {
        if (ref.current) rebindAsync(ref.current);
    }, []);
    return (
        <div ref={ref} class="text-center py-16">
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
    const ref = useRef(null);
    useEffect(() => {
        if (ref.current) rebindAsync(ref.current);
    }, []);
    return (
        <div ref={ref} class="text-center py-16">
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
