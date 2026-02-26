import { useReducer, useRef, useEffect, useCallback, useMemo } from 'preact/hooks';
import { StremioClient, CINEMETA_BASE } from '../client';
import {
    discoverReducer, initialState,
    getCatalogsForType, getTypes,
    getSearchTypes, getSearchResultsForType,
} from './discoverReducer';
import { StreamModal } from './StreamModal';
import { loadPrefs, savePrefs } from '../prefs';
import { useDiscoverUrl } from './useDiscoverUrl';
import { restoreModalFromUrl, loadManifests } from './discoverUtils';
import { SearchBar } from './SearchBar';
import { ItemGrid } from './ItemGrid';
import { TypeTabs, SearchTabs, CatalogSelector } from './Tabs';
import { LoadMore, LoadingSpinner, NoAddons, NoCatalogs, ErrorState, NoResults } from './EmptyStates';

export function DiscoverApp({ addonUrls }) {
    const [state, dispatch] = useReducer(discoverReducer, initialState);
    const clientRef = useRef(null);
    const abortRef = useRef(null);
    const searchGenRef = useRef(0);
    const searchAfterInit = useRef(null);
    const restoredPageRef = useRef(0);
    const modalItemIdRef = useRef(null);
    const modalEpisodeRef = useRef(null); // { season, episode }
    const modalSeasonRef = useRef(null); // season-only (no episode) from URL
    const restoreInProgressRef = useRef(false);
    const stateRef = useRef(null); // latest state snapshot for popstate handler
    const modalRef = useRef(null); // current modal for popstate handler
    const openModalByIdRef = useRef(null); // latest openModalById for popstate handler
    const performSearchRef = useRef(null); // latest performSearch for popstate handler

    const url = useDiscoverUrl('/discover');

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
                const { manifests, catalogs, types } = await loadManifests(client);
                if (cancelled) return;

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
                } else if (urlSeason != null) {
                    modalSeasonRef.current = urlSeason;
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
        const catalogs = getCatalogsForType(state.catalogs, type);
        const catalog = catalogs[0] || null;
        url.push({
            type, 'catalog-base': catalog?.baseUrl, 'catalog-id': catalog?.id,
            id: null, season: null, episode: null, search: null, 'search-type': null,
        });
        dispatch({ type: 'SELECT_TYPE', selectedType: type });
    }, [state.catalogs]);

    const selectCatalog = useCallback((catalog) => {
        abortCatalog();
        savePrefs({ catalogBase: catalog.baseUrl, catalogId: catalog.id });
        url.push({
            'catalog-base': catalog.baseUrl, 'catalog-id': catalog.id,
            id: null, season: null, episode: null,
        });
        dispatch({ type: 'SELECT_CATALOG', catalog });
    }, []);

    // --- Load more ---
    const loadMore = useCallback(() => {
        loadCatalog(state.selectedCatalog, state.items.length, state.items);
    }, [state.selectedCatalog, state.items, loadCatalog]);

    // --- Search ---
    const exitSearch = useCallback(() => {
        abortCatalog();
        if (!url.isPopstate.current) {
            const types = getTypes(state.catalogs);
            const type = types[0] || null;
            const catalog = type ? getCatalogsForType(state.catalogs, type)[0] : null;
            url.push({
                type, 'catalog-base': catalog?.baseUrl, 'catalog-id': catalog?.id,
                search: null, 'search-type': null, id: null, season: null, episode: null,
            });
        }
        dispatch({ type: 'EXIT_SEARCH' });
    }, [state.catalogs]);

    const performSearch = useCallback(async (query) => {
        query = query.trim();
        if (query.length < 2) {
            if (state.isSearchMode) {
                exitSearch();
            }
            return;
        }

        // Push history entry when first entering search mode (not during popstate)
        if (!state.isSearchMode && !url.isPopstate.current) {
            url.push({ search: query, 'search-type': null, type: null, 'catalog-base': null, 'catalog-id': null, id: null, season: null, episode: null });
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
    }, [client, state.isSearchMode, exitSearch]);

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
        const restoreSeason = modalSeasonRef.current;
        modalSeasonRef.current = null;

        if (url.isPopstate.current) {
            url.replace({ id, season: restoreSeason ?? null, episode: null });
        } else {
            url.push({ id, season: null, episode: null });
        }

        if (type === 'series') {
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'loading', title: item.name, poster: item.poster, subtitle: 'Loading episodes...' } });
            try {
                const meta = await client.fetchMeta(type, id);
                if (meta?.videos?.length > 0) {
                    dispatch({ type: 'SHOW_MODAL', modal: {
                        view: 'episodes', title: item.name, poster: item.poster, meta, itemId: id, itemType: type,
                        defaultSeason: restoreSeason != null ? Number(restoreSeason) : undefined,
                    } });
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
            if (url.isPopstate.current) {
                url.replace({ id, season: ep.season, episode: ep.episode });
            } else {
                url.push({ season: ep.season, episode: ep.episode });
            }

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
        url.push({ season: episode.season, episode: episode.episode });

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
        window.history.back();
    }, []);

    const onSeasonChange = useCallback((season) => {
        url.push({ season, episode: null });
    }, []);

    const handleStreamClick = useCallback(async (infoHash, fileIdx) => {
        const currentTitle = state.modal?.title;
        const currentPoster = state.modal?.poster;
        const currentBackToEpisodes = state.modal?.backToEpisodes;

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
                error: 'Failed to prepare the resource. Please try again.',
                backToEpisodes: currentBackToEpisodes,
            }});
        }
    }, [state.modal]);

    const closeModal = useCallback(() => {
        dispatch({ type: 'CLOSE_MODAL' });
        url.replace({ id: null, season: null, episode: null });
    }, []);

    const selectSearchType = useCallback((searchType) => {
        url.push({ 'search-type': searchType === 'all' ? null : searchType });
        dispatch({ type: 'SELECT_SEARCH_TYPE', searchType });
    }, []);

    // Keep refs in sync for popstate handler
    modalRef.current = state.modal;
    openModalByIdRef.current = openModalById;
    performSearchRef.current = performSearch;
    stateRef.current = state;

    // --- Browser back/forward ---
    url.onPopstate((urlParams) => {
        const id = urlParams.get('id');
        const season = urlParams.get('season');
        const episode = urlParams.get('episode');
        const type = urlParams.get('type');
        const catalogBase = urlParams.get('catalog-base');
        const catalogId = urlParams.get('catalog-id');
        const search = urlParams.get('search');
        const searchType = urlParams.get('search-type');

        const cur = stateRef.current;

        // Handle search mode
        if (search) {
            if (!cur.isSearchMode || cur.searchQuery !== search) {
                url.withPopstate(() => performSearchRef.current(search));
            }
            const targetType = searchType || 'all';
            if (targetType !== (cur.searchType || 'all')) {
                dispatch({ type: 'SELECT_SEARCH_TYPE', searchType: targetType });
            }
            // Handle modal within search results
            if (id && season && episode) {
                modalEpisodeRef.current = { season, episode };
                url.withPopstate(() => openModalByIdRef.current(id));
            } else if (id) {
                url.withPopstate(() => openModalByIdRef.current(id));
            } else if (modalRef.current) {
                dispatch({ type: 'CLOSE_MODAL' });
            }
            return;
        }

        // If no search param but currently in search mode — exit search
        if (cur.isSearchMode) {
            url.withPopstate(() => { abortCatalog(); dispatch({ type: 'EXIT_SEARCH' }); });
        }

        // Handle type change
        if (type && cur && type !== cur.selectedType) {
            const types = getTypes(cur.catalogs);
            if (types.includes(type)) {
                savePrefs({ type });
                dispatch({ type: 'SELECT_TYPE', selectedType: type });
            }
        }

        // Handle catalog change
        if (catalogBase && catalogId && cur) {
            const curCat = cur.selectedCatalog;
            if (!curCat || curCat.baseUrl !== catalogBase || curCat.id !== catalogId) {
                const match = getCatalogsForType(cur.catalogs, type || cur.selectedType)
                    .find(c => c.baseUrl === catalogBase && c.id === catalogId);
                if (match) {
                    savePrefs({ catalogBase: match.baseUrl, catalogId: match.id });
                    dispatch({ type: 'SELECT_CATALOG', catalog: match });
                }
            }
        }

        // Handle modal state
        if (!id) {
            if (modalRef.current) {
                dispatch({ type: 'CLOSE_MODAL' });
            }
            return;
        }

        if (id && season && episode) {
            modalEpisodeRef.current = { season, episode };
            url.withPopstate(() => openModalByIdRef.current(id));
        } else if (id) {
            const currentModal = modalRef.current;
            if (currentModal?.view === 'episodes' && currentModal?.itemId === id) {
                // Same series, navigating between seasons — force remount via _seasonKey
                dispatch({
                    type: 'SHOW_MODAL',
                    modal: {
                        ...currentModal,
                        defaultSeason: season != null ? Number(season) : undefined,
                        _seasonKey: Date.now(),
                    },
                });
            } else if (currentModal?.backToEpisodes) {
                const back = currentModal.backToEpisodes;
                dispatch({
                    type: 'SHOW_MODAL',
                    modal: {
                        view: 'episodes',
                        title: back.title,
                        poster: back.poster,
                        meta: back.meta,
                        itemId: back.itemId,
                        itemType: back.itemType,
                        defaultSeason: season != null ? Number(season) : (back.season != null ? Number(back.season) : undefined),
                    },
                });
            } else {
                url.withPopstate(() => openModalByIdRef.current(id));
            }
        }
    });

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
            restoreModalFromUrl(id, url, openModalById, modalEpisodeRef);
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
        restoreModalFromUrl(id, url, openModalById, modalEpisodeRef);
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
        const cur = new URLSearchParams(window.location.search);
        ['id', 'season', 'episode'].forEach(k => { const v = cur.get(k); if (v != null) params[k] = v; });

        url.replaceAll(params);
    }, [state.phase, state.selectedType, state.selectedCatalog, state.isSearchMode, state.searchQuery, state.searchType, state.page]);

    const retry = useCallback(() => {
        dispatch({ type: 'SET_PHASE', phase: 'loading' });
        client.manifests = null;
        prevCatalogRef.current = null;
        (async () => {
            try {
                const { catalogs, types } = await loadManifests(client);
                if (!types.length) {
                    dispatch({ type: 'SET_PHASE', phase: 'no-catalogs' });
                    return;
                }
                const selectedType = types[0];
                const selectedCatalog = getCatalogsForType(catalogs, selectedType)[0] || null;
                dispatch({ type: 'INIT_SUCCESS', manifests: client.manifests, catalogs, selectedType, selectedCatalog });
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

    const showBadges = useMemo(() => state.isSearchMode && state.searchType === 'all', [state.isSearchMode, state.searchType]);

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
                    onSelect={selectSearchType}
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
                    onSeasonChange={onSeasonChange}
                />
            )}
        </div>
    );
}
