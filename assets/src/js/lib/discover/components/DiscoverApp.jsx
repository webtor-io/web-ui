import { useReducer, useRef, useEffect, useCallback, useMemo, useState } from 'preact/hooks';
import { StremioClient, CINEMETA_BASE } from '../client';
import {
    discoverReducer, initialState,
    getCatalogsForType, getTypes,
    getSearchTypes, getSearchResultsForType,
} from './discoverReducer';
import { StreamModal } from './StreamModal';
import { AddonWizard } from './AddonWizard';
import { loadPrefs, savePrefs } from '../prefs';
import { useDiscoverUrl } from './useDiscoverUrl';
import { restoreModalFromUrl, loadManifests, fetchUserStatuses, toggleWatched, rateVideo, unrateVideo, catalogChipClass, watchlistChipClass } from './discoverUtils';
import { fetchWatchlistIds, fetchWatchlist, addToWatchlist, removeFromWatchlist } from '../watchlistClient';
import { RatingDialog } from './RatingDialog';
import { SearchBar } from './SearchBar';
import { ItemGrid } from './ItemGrid';
import { TypeTabs, SearchTabs, CatalogSelector } from './Tabs';
import { LoadMore, LoadingSpinner, NoAddons, NoCatalogs, ErrorState, NoResults } from './EmptyStates';
import { AISection } from './ai/AISection';
import { t, langPath } from '../i18n';

export function DiscoverApp({ addonUrls, hasCustomAddons }) {
    const [state, dispatch] = useReducer(discoverReducer, initialState);
    const [showWizard, setShowWizard] = useState(false);
    const [addonsInstalled, setAddonsInstalled] = useState(false);
    const [ratingTarget, setRatingTarget] = useState(null);
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
    const pendingStreamRef = useRef(null); // stream to reload after wizard

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
            // Fire-and-forget user status query for the newly arrived batch.
            fetchUserStatuses(metas.map(m => m.id)).then(statuses => {
                if (Object.keys(statuses).length) dispatch({ type: 'USER_STATUSES_MERGED', statuses });
            });
        } catch (e) {
            if (e.name === 'AbortError') return;
            dispatch({ type: 'CATALOG_ERROR', message: t('discover.catalogLoadError') });
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
                } else if (urlParams.get('watchlist') === '1') {
                    // Restore watchlist mode from URL. Items load via the
                    // mount-time prefetch effect below + a lazy fetch on
                    // first open; doing the fetch from here would race the
                    // INIT_SUCCESS dispatch above.
                    dispatch({ type: 'WATCHLIST_FILTER_SET', enabled: true });
                    const wlType = urlParams.get('watchlist-type');
                    if (wlType && wlType !== 'all') {
                        dispatch({ type: 'SELECT_WATCHLIST_TYPE', watchlistType: wlType });
                    }
                }
            } catch (e) {
                if (!cancelled) {
                    dispatch({ type: 'INIT_ERROR', message: t('discover.manifestLoadError') });
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
        fetchUserStatuses(merged.map(m => m.id)).then(statuses => {
            if (Object.keys(statuses).length) dispatch({ type: 'USER_STATUSES_MERGED', statuses });
        });
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
    const loadStreams = useCallback(async (type, id, item, modalExtra = {}) => {
        const title = item.name || item.title;
        const poster = item.poster;
        const metaId = id.split(':')[0];
        const itemType = type;
        const year = item.year;
        const releaseInfo = item.releaseInfo;
        const imdbRating = item.imdbRating;
        const description = item.description;
        const addons = client.getStreamAddons();
        const itemMeta = { year, releaseInfo, imdbRating, description };
        if (!addons.length) {
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'streams', title, poster, metaId, itemType, ...itemMeta, streams: [], ...modalExtra } });
            return;
        }

        const addonStatuses = addons.map(a => {
            let host;
            try { host = new URL(a.baseUrl).hostname; } catch { host = a.baseUrl; }
            return { name: a.manifest.name || host, host, status: 'fetching' };
        });

        dispatch({ type: 'SHOW_MODAL', modal: { view: 'fetching', title, poster, metaId, itemType, ...itemMeta, addons: [...addonStatuses], ...modalExtra } });

        const allStreams = [];
        const promises = addons.map(async (addon, i) => {
            try {
                const streams = await client.fetchStreamFromAddon(addon, type, id);
                addonStatuses[i] = { ...addonStatuses[i], status: 'done', count: streams.length };
                allStreams.push(...streams);
            } catch (e) {
                addonStatuses[i] = { ...addonStatuses[i], status: 'error' };
            }
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'fetching', title, poster, metaId, itemType, ...itemMeta, addons: [...addonStatuses], ...modalExtra } });
        });

        await Promise.allSettled(promises);
        dispatch({ type: 'SHOW_MODAL', modal: { view: 'streams', title, poster, metaId, itemType, ...itemMeta, streams: allStreams, ...modalExtra } });
        window.umami?.track('discover-streams-loaded', { type, id, count: allStreams.length });
    }, [client]);

    const cardClick = useCallback(async (item) => {
        const type = item.type || state.selectedType;
        const id = item.id;
        const restoreSeason = modalSeasonRef.current;
        modalSeasonRef.current = null;

        // Watchlist conversion telemetry: a click that originates from the
        // watchlist view is the only signal we have today that "saved → opened
        // to stream" — without this we cannot answer whether Watchlist drives
        // any retention. Fired before any async work so a fetch failure still
        // counts the intent.
        if (stateRef.current?.watchlistFilterEnabled) {
            window.umami?.track?.('stream-from-watchlist');
        }

        if (url.isPopstate.current) {
            url.replace({ id, season: restoreSeason ?? null, episode: null });
        } else {
            url.push({ id, season: null, episode: null });
        }

        // Build metadata from catalog item; fetchMeta will fill in missing fields.
        const cardMeta = { year: item.year, releaseInfo: item.releaseInfo, imdbRating: item.imdbRating, description: item.description };

        // Enrich cardMeta from Stremio meta response (has description, imdbRating, etc.)
        function enrichFromMeta(meta) {
            if (!meta) return;
            if (!cardMeta.description && meta.description) cardMeta.description = meta.description;
            if (!cardMeta.imdbRating && meta.imdbRating) cardMeta.imdbRating = meta.imdbRating;
            if (!cardMeta.releaseInfo && meta.releaseInfo) cardMeta.releaseInfo = meta.releaseInfo;
            if (!cardMeta.year && meta.year) cardMeta.year = meta.year;
        }

        if (type === 'series') {
            dispatch({ type: 'SHOW_MODAL', modal: { view: 'loading', title: item.name, poster: item.poster, subtitle: t('discover.loadingEpisodes'), itemType: type, itemId: id, ...cardMeta } });
            try {
                const meta = await client.fetchMeta(type, id);
                enrichFromMeta(meta);
                if (meta?.videos?.length > 0) {
                    dispatch({ type: 'SHOW_MODAL', modal: {
                        view: 'episodes', title: meta.name || item.name, poster: meta.poster || item.poster, meta, itemId: id, itemType: type, ...cardMeta,
                        defaultSeason: restoreSeason != null ? Number(restoreSeason) : undefined,
                    } });
                } else {
                    await loadStreams(type, id, { ...item, ...cardMeta });
                }
            } catch (e) {
                await loadStreams(type, id, { ...item, ...cardMeta });
            }
        } else {
            // Fetch meta in parallel with streams to enrich modal with description/rating.
            const metaPromise = client.fetchMeta(type, id).catch(() => null);
            const streamsPromise = loadStreams(type, id, { ...item, ...cardMeta });
            const meta = await metaPromise;
            await streamsPromise;
            if (meta) {
                enrichFromMeta(meta);
                const cur = stateRef.current?.modal;
                if (cur && cur.metaId === id.split(':')[0]) {
                    dispatch({ type: 'SHOW_MODAL', modal: { ...cur, ...cardMeta, title: meta.name || cur.title, poster: meta.poster || cur.poster } });
                }
            }
        }
    }, [client, state.selectedType, loadStreams]);

    // Bridge from an AI recommendation card to the existing stream-loading
    // flow. AI items carry video_id (IMDB) / title / poster / plot / reason
    // / rating, which we shape into the catalog-item contract cardClick
    // expects.
    const handleAICardClick = useCallback((aiItem) => {
        if (!aiItem?.video_id) return;
        cardClick({
            id: aiItem.video_id,
            name: aiItem.title,
            poster: aiItem.poster,
            type: aiItem.type || 'movie',
            year: aiItem.year,
            imdbRating: aiItem.rating,
            description: aiItem.plot,
        });
    }, [cardClick]);

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

            // Fetch meta in parallel with stream loading so we can offer "back to episodes"
            const metaPromise = client.fetchMeta(type, id).catch(() => null);

            await loadStreams(type, epId, { name: epName, poster, year: item?.year, releaseInfo: item?.releaseInfo, imdbRating: item?.imdbRating, description: item?.description }, {});

            const meta = await metaPromise;
            if (meta?.videos?.length > 0) {
                const backToEpisodes = { title: name, poster, meta, itemId: id, itemType: type, season: ep.season, year: item?.year, releaseInfo: item?.releaseInfo, imdbRating: item?.imdbRating, description: item?.description };
                const cur = stateRef.current?.modal;
                if (cur) dispatch({ type: 'SHOW_MODAL', modal: { ...cur, backToEpisodes } });
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
            year: item.year,
            releaseInfo: item.releaseInfo,
            imdbRating: item.imdbRating,
            description: item.description,
        };

        await loadStreams(type, epId, { name: epName, poster: item.poster, year: item.year, releaseInfo: item.releaseInfo, imdbRating: item.imdbRating, description: item.description }, { backToEpisodes });
    }, [loadStreams]);

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
            const metaId = state.modal?.metaId;
            if (metaId) formData.append('hint_video_id', metaId);
            const response = await fetch(langPath('/'), {
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
                error: t('discover.resourcePrepError'),
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
        const watchlist = urlParams.get('watchlist') === '1';
        const watchlistType = urlParams.get('watchlist-type');

        const cur = stateRef.current;

        // Handle watchlist mode toggle. We do this before search so back
        // navigation from a search-while-watchlist-open lands correctly.
        if (watchlist !== cur.watchlistFilterEnabled) {
            url.withPopstate(() => setMode(watchlist ? 'watchlist' : 'catalog', { skipUrl: true }));
        }
        if (watchlist) {
            const targetWlType = watchlistType || 'all';
            if (targetWlType !== (cur.watchlistType || 'all')) {
                dispatch({ type: 'SELECT_WATCHLIST_TYPE', watchlistType: targetWlType });
            }
            // No catalog/search restoration applies to the watchlist view —
            // bail out so the rest of the handler doesn't try.
            if (id && modalRef.current?.itemId !== id) {
                url.withPopstate(() => openModalByIdRef.current(id));
            } else if (!id && modalRef.current) {
                dispatch({ type: 'CLOSE_MODAL' });
            }
            return;
        }

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
                        year: back.year,
                        releaseInfo: back.releaseInfo,
                        imdbRating: back.imdbRating,
                        description: back.description,
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
        } else if (state.watchlistFilterEnabled) {
            params.watchlist = '1';
            if (state.watchlistType && state.watchlistType !== 'all') params['watchlist-type'] = state.watchlistType;
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
    }, [state.phase, state.selectedType, state.selectedCatalog, state.isSearchMode, state.searchQuery, state.searchType, state.watchlistFilterEnabled, state.watchlistType, state.page]);

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

    // --- Wizard callbacks ---
    const onWizardComplete = useCallback(async (savedUrls, result) => {
        setShowWizard(false);
        setAddonsInstalled(true);
        if (result && window.toast) {
            const msg = `${result.added} addon${result.added !== 1 ? 's' : ''} added${result.skipped > 0 ? `, ${result.skipped} skipped` : ''}${result.limitReached ? ' (free tier limit reached)' : ''}`;
            window.toast.success(msg);
        }
        // Add new URLs to client (strip /manifest.json suffix)
        for (const u of savedUrls) {
            const base = u.replace(/\/manifest\.json$/, '');
            if (!client.addonUrls.includes(base)) {
                client.addonUrls.push(base);
            }
        }

        const pending = pendingStreamRef.current;
        pendingStreamRef.current = null;

        if (pending) {
            // Came from "Set up addons" in stream modal — silently refresh manifests,
            // keep current type/catalog, just reload streams
            try {
                const manifests = await client.fetchAllManifests();
                client.manifests = manifests;
            } catch (e) { /* streams may still work */ }

            dispatch({ type: 'SHOW_MODAL', modal: { view: 'loading', title: pending.title, poster: pending.poster, subtitle: 'Loading streams...' } });
            let streams = [];
            for (let attempt = 0; attempt < 3; attempt++) {
                try {
                    streams = await client.fetchStreams(pending.type, pending.streamId);
                } catch (e) {
                    streams = [];
                }
                if (streams.length > 0) break;
                if (attempt < 2) await new Promise(r => setTimeout(r, 2000));
            }
            dispatch({ type: 'SHOW_MODAL', modal: {
                view: 'streams', title: pending.title, poster: pending.poster, streams,
                backToEpisodes: pending.backToEpisodes,
            } });
            // Restore URL params so browser back returns to this modal
            url.push({ id: pending.id, season: pending.season, episode: pending.episode });
        } else {
            // Initial wizard (page load) — full re-init to populate catalogs
            client.manifests = null;
            prevCatalogRef.current = null;
            dispatch({ type: 'SET_PHASE', phase: 'loading' });
            try {
                const { manifests, catalogs, types } = await loadManifests(client);
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
        }
    }, [client]);

    const onWizardSkip = useCallback(() => {
        setShowWizard(false);
        const pending = pendingStreamRef.current;
        if (pending) {
            pendingStreamRef.current = null;
            dispatch({ type: 'SHOW_MODAL', modal: pending.modal });
            url.push({ id: pending.id, season: pending.season, episode: pending.episode });
        }
    }, []);

    const onSetupAddons = useCallback(() => {
        const params = new URLSearchParams(window.location.search);
        const id = params.get('id');
        if (id) {
            const season = params.get('season');
            const episode = params.get('episode');
            // For episodes, stream ID is "tt123:season:episode"
            const streamId = (season != null && episode != null) ? `${id}:${season}:${episode}` : id;
            const type = state.modal?.backToEpisodes?.itemType || state.selectedType;
            pendingStreamRef.current = {
                streamId,
                type,
                title: state.modal?.title,
                poster: state.modal?.poster,
                backToEpisodes: state.modal?.backToEpisodes,
                id,
                season,
                episode,
                modal: state.modal,
            };
        }
        closeModal();
        setShowWizard(true);
    }, [state.modal, state.selectedType, closeModal]);

    // --- Watched / Rating ---
    const handleToggleWatched = useCallback(async (item) => {
        const type = item.type || state.selectedType;
        const currentlyWatched = state.userStatuses[item.id]?.watched || false;
        const prev = state.userStatuses[item.id];

        if (currentlyWatched) {
            // Unmark: clear watched + rating (server deletes entire status row)
            dispatch({ type: 'USER_STATUSES_MERGED', statuses: {
                [item.id]: { watched: false, rating: 0 },
            }});
            const result = await toggleWatched(item.id, type, true);
            if (!result) {
                dispatch({ type: 'USER_STATUSES_MERGED', statuses: { [item.id]: { ...prev } } });
            }
        } else {
            // Mark watched, then prompt for rating
            dispatch({ type: 'USER_STATUSES_MERGED', statuses: {
                [item.id]: { ...prev, watched: true },
            }});
            const result = await toggleWatched(item.id, type, false);
            if (result) {
                if (result.rateForm) setRatingTarget({ item, type, currentRating: 0 });
            } else {
                dispatch({ type: 'USER_STATUSES_MERGED', statuses: { [item.id]: { ...prev } } });
            }
        }
    }, [state.selectedType, state.userStatuses]);

    const handleOpenRating = useCallback((item) => {
        const type = item.type || state.selectedType;
        const currentRating = state.userStatuses[item.id]?.rating || 0;
        setRatingTarget({ item, type, currentRating });
    }, [state.selectedType, state.userStatuses]);

    // AI shims for watched / rating. Defined AFTER handleToggleWatched and
    // handleOpenRating because those const useCallbacks live in the same
    // function scope and JavaScript's temporal dead zone would otherwise
    // throw on first render — the upstream declarations are further down
    // the file.
    const handleAIToggleWatched = useCallback((aiItem) => {
        if (!aiItem?.video_id) return;
        handleToggleWatched({
            id: aiItem.video_id,
            type: aiItem.type || 'movie',
        });
    }, [handleToggleWatched]);

    const handleAIOpenRating = useCallback((aiItem) => {
        if (!aiItem?.video_id) return;
        handleOpenRating({
            id: aiItem.video_id,
            type: aiItem.type || 'movie',
        });
    }, [handleOpenRating]);

    // --- Watchlist ---
    // Cheap prefetch on mount: ids only, for bookmark-badge highlighting on
    // every IMDB card across catalog / search / AI grids. We don't pull the
    // full items here — that's lazy on toggle-on (loadWatchlistItems below).
    useEffect(() => {
        let cancelled = false;
        fetchWatchlistIds().then(({ ids, limit }) => {
            if (cancelled) return;
            dispatch({ type: 'WATCHLIST_IDS_LOADED', ids, limit });
        });
        return () => { cancelled = true; };
    }, []);

    const loadWatchlistItems = useCallback(async () => {
        const data = await fetchWatchlist();
        dispatch({ type: 'WATCHLIST_ITEMS_LOADED', items: data.items, ids: data.ids, limit: data.limit });
    }, []);

    // If the watchlist view is active (either via direct URL or popstate)
    // and items aren't loaded yet, fetch them. Covers the init-from-URL
    // path where setMode wasn't the trigger.
    useEffect(() => {
        if (!state.watchlistFilterEnabled) return;
        if (state.watchlistItemsLoaded) return;
        loadWatchlistItems();
    }, [state.watchlistFilterEnabled, state.watchlistItemsLoaded, loadWatchlistItems]);

    // setMode flips between the catalog grid and the watchlist grid. It is
    // the single entry point used by the Catalog | Watchlist switcher and
    // by popstate (so back/forward navigation restores the correct view).
    // Lazy-loads watchlist items the first time the user opens the view;
    // subsequent switches just flip the boolean.
    //
    // skipUrl is set by the popstate handler so we don't push a redundant
    // history entry while we're consuming the URL state.
    const setMode = useCallback((mode, { skipUrl = false } = {}) => {
        const wantWatchlist = mode === 'watchlist';
        if (wantWatchlist === state.watchlistFilterEnabled) return;
        window.umami?.track?.('watchlist-mode-changed', { mode });
        if (!skipUrl) {
            // Switching modes is a navigation event — push history so back
            // returns to the previous view. Clear catalog and modal params
            // when entering watchlist (they don't apply); on the way out
            // they get restored from prefs by the catalog effect.
            url.push(wantWatchlist
                ? { watchlist: '1', 'catalog-base': null, 'catalog-id': null, type: null, page: null, id: null, season: null, episode: null }
                : { watchlist: null, id: null, season: null, episode: null });
        }
        dispatch({ type: 'WATCHLIST_FILTER_SET', enabled: wantWatchlist });
        if (wantWatchlist && !state.watchlistItemsLoaded) {
            loadWatchlistItems();
        }
    }, [state.watchlistFilterEnabled, state.watchlistItemsLoaded, loadWatchlistItems]);

    // handleToggleWatchlist accepts a Cinemeta-shape item ({id, type, name,
    // poster, ...}) OR an AI shape ({video_id, title, poster, type, ...})
    // and normalises before calling the server. Both shapes flow through
    // the same code path because catalog + AI cards use the same toggle
    // affordance.
    const handleToggleWatchlist = useCallback(async (rawItem) => {
        if (!rawItem) return;
        const videoId = rawItem.id || rawItem.video_id;
        if (!videoId || !videoId.startsWith('tt')) return;
        const type = rawItem.type || state.selectedType || 'movie';
        if (type !== 'movie' && type !== 'series') return;

        const inList = state.watchlistIds.has(videoId);
        // Determine source bucket for analytics: AI cards have video_id,
        // catalog/search cards have id.
        const source = rawItem.video_id ? 'ai' : (state.isSearchMode ? 'search' : 'catalog');

        // Toast text always comes from the server response — server-side
        // i18n.T(c, key) keeps the locale aligned with the page even when
        // the user changes language mid-session. We only fall back to a
        // generic English message when the server didn't respond at all
        // (network failure → empty message), which mirrors the apiPost
        // pattern in discoverUtils.js.
        if (inList) {
            const snapshot = state.watchlistItems.find(it => it.id === videoId);
            dispatch({ type: 'WATCHLIST_REMOVE', videoId });
            window.umami?.track?.('watchlist-removed', { id: videoId, source });
            const result = await removeFromWatchlist(videoId, type);
            if (!result.ok) {
                if (snapshot) dispatch({ type: 'WATCHLIST_ADD', item: snapshot });
                if (window.toast) window.toast.error(result.message || t('discover.networkError'));
            } else if (window.toast && result.message) {
                window.toast.success(result.message);
            }
        } else {
            const item = {
                id: videoId,
                type,
                name: rawItem.name || rawItem.title || '',
                year: rawItem.year,
                poster: rawItem.poster || `/lib/${type}/poster/${videoId}/500.jpg`,
                imdbRating: rawItem.imdbRating != null ? rawItem.imdbRating : (rawItem.rating || undefined),
            };
            dispatch({ type: 'WATCHLIST_ADD', item });
            window.umami?.track?.('watchlist-added', { id: videoId, source });
            const result = await addToWatchlist(videoId, type, source);
            if (!result.ok) {
                dispatch({ type: 'WATCHLIST_REMOVE', videoId });
                if (window.toast) window.toast.error(result.message || t('discover.networkError'));
            } else if (window.toast && result.message) {
                window.toast.success(result.message);
            }
        }
    }, [state.watchlistIds, state.watchlistItems, state.selectedType, state.isSearchMode]);

    const handleRate = useCallback(async (rating) => {
        if (!ratingTarget) return;
        const { item, type } = ratingTarget;
        setRatingTarget(null);
        const prevStatus = state.userStatuses[item.id];
        dispatch({ type: 'USER_STATUSES_MERGED', statuses: {
            [item.id]: { ...prevStatus, watched: true, rating },
        }});
        const ok = await rateVideo(item.id, type, rating);
        if (!ok) {
            dispatch({ type: 'USER_STATUSES_MERGED', statuses: { [item.id]: { ...prevStatus } } });
        }
    }, [ratingTarget, state.userStatuses]);

    const handleUnrate = useCallback(async () => {
        if (!ratingTarget) return;
        const { item, type } = ratingTarget;
        setRatingTarget(null);
        const prev = state.userStatuses[item.id];
        dispatch({ type: 'USER_STATUSES_MERGED', statuses: {
            [item.id]: { ...prev, rating: 0 },
        }});
        const ok = await unrateVideo(item.id, type);
        if (!ok) {
            dispatch({ type: 'USER_STATUSES_MERGED', statuses: { [item.id]: { ...prev } } });
        }
    }, [ratingTarget, state.userStatuses]);

    const handleCloseRating = useCallback(() => {
        setRatingTarget(null);
    }, []);

    // --- Derived data ---
    const types = useMemo(() => getTypes(state.catalogs), [state.catalogs]);
    const catalogsForType = useMemo(() => getCatalogsForType(state.catalogs, state.selectedType), [state.catalogs, state.selectedType]);

    const searchTypes = useMemo(() => getSearchTypes(state.searchResults), [state.searchResults]);
    const watchlistTypes = useMemo(() => getSearchTypes(state.watchlistItems), [state.watchlistItems]);

    const displayItems = useMemo(() => {
        if (state.isSearchMode) {
            if (state.searchType === 'all') return state.searchResults;
            return getSearchResultsForType(state.searchResults, state.searchType);
        }
        if (state.watchlistFilterEnabled) {
            if (state.watchlistType === 'all') return state.watchlistItems;
            return getSearchResultsForType(state.watchlistItems, state.watchlistType);
        }
        return state.items;
    }, [state.isSearchMode, state.items, state.searchResults, state.searchType,
        state.watchlistFilterEnabled, state.watchlistItems, state.watchlistType]);

    const showBadges = useMemo(() => {
        if (state.isSearchMode) return state.searchType === 'all';
        if (state.watchlistFilterEnabled) return state.watchlistType === 'all';
        return false;
    }, [state.isSearchMode, state.searchType, state.watchlistFilterEnabled, state.watchlistType]);

    const selectWatchlistType = useCallback((wt) => {
        dispatch({ type: 'SELECT_WATCHLIST_TYPE', watchlistType: wt });
    }, []);

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
            {/* AI recommendations section — self-contained, hides itself when
                the feature flag is off (phase === 'disabled'). Rendered above
                the search bar as a top-of-page spotlight. */}
            <AISection
                aiState={state.ai}
                dispatch={dispatch}
                onCardClick={handleAICardClick}
                userStatuses={state.userStatuses}
                watchlistIds={state.watchlistIds}
                onToggleWatched={handleAIToggleWatched}
                onRate={handleAIOpenRating}
                onToggleWatchlist={handleToggleWatchlist}
            />

            <SearchBar
                onSearch={performSearch}
                onExit={exitSearch}
                isSearchMode={state.isSearchMode}
                initialQuery={state.searchQuery}
            />

            {/* Sticky tab bar — stays below navbar (72px) on scroll */}
            <div class="sticky top-[72px] z-10 -mx-3 sm:-mx-6 px-3 sm:px-6 py-3 bg-w-bg/90 backdrop-blur-lg border-b border-w-line/30">
                {state.isSearchMode ? (
                    <SearchTabs
                        searchResults={state.searchResults}
                        searchTypes={searchTypes}
                        searchType={state.searchType}
                        onSelect={selectSearchType}
                    />
                ) : (
                    <>
                        {/* Type tabs on the left, mode switcher on the right.
                            justify-between keeps them on opposite ends; on
                            tight screens flex-wrap drops the switcher to a
                            new line where it stays right-aligned via
                            ml-auto on its row. */}
                        <div class="flex items-center justify-between gap-2 flex-wrap">
                            <div class="min-w-0">
                                {state.watchlistFilterEnabled ? (
                                    <SearchTabs
                                        searchResults={state.watchlistItems}
                                        searchTypes={watchlistTypes}
                                        searchType={state.watchlistType}
                                        onSelect={selectWatchlistType}
                                    />
                                ) : (
                                    <TypeTabs types={types} selectedType={state.selectedType} onSelect={selectType} />
                                )}
                            </div>
                            <div class="join ml-auto">
                                <button
                                    type="button"
                                    onClick={() => setMode('catalog')}
                                    class={catalogChipClass(!state.watchlistFilterEnabled)}
                                    title={t('discover.modeCatalog')}
                                    aria-label={t('discover.modeCatalog')}
                                >
                                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                                        <path d="M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z" />
                                    </svg>
                                    <span class="hidden sm:inline">{t('discover.modeCatalog')}</span>
                                </button>
                                <button
                                    type="button"
                                    onClick={() => setMode('watchlist')}
                                    class={watchlistChipClass(state.watchlistFilterEnabled)}
                                    title={t('discover.watchlist.label')}
                                    aria-label={t('discover.watchlist.label')}
                                >
                                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill={state.watchlistFilterEnabled ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                                        <path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12Z" />
                                    </svg>
                                    <span class="hidden sm:inline">{t('discover.watchlist.label')}</span>
                                    {state.watchlistIds.size > 0 && (
                                        <span class="text-xs opacity-70 ml-1 tabular-nums">{state.watchlistIds.size}</span>
                                    )}
                                </button>
                            </div>
                        </div>
                        {!state.watchlistFilterEnabled && (
                            <CatalogSelector catalogs={catalogsForType} selectedCatalog={state.selectedCatalog} onSelect={selectCatalog} />
                        )}
                    </>
                )}
            </div>

            {(state.catalogLoading || state.searchLoading) && <LoadingSpinner />}

            {!state.catalogLoading && !state.searchLoading && state.isSearchMode && state.searchResults.length === 0 && state.searchQuery && (
                <NoResults query={state.searchQuery} />
            )}

            {!state.catalogLoading && !state.searchLoading && !state.isSearchMode && !state.watchlistFilterEnabled && state.items.length === 0 && state.phase === 'ready' && !state.hasMore && (
                <p class="text-w-muted text-center col-span-full py-8">No items found.</p>
            )}

            {!state.isSearchMode && state.watchlistFilterEnabled && state.watchlistItemsLoaded && state.watchlistItems.length === 0 && (
                <div class="text-center py-12 max-w-md mx-auto">
                    <div class="inline-flex items-center justify-center w-16 h-16 rounded-full bg-w-pink/10 text-w-pinkL mb-4">
                        <svg class="w-8 h-8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12Z" />
                        </svg>
                    </div>
                    <h3 class="text-base font-semibold text-w-text mb-2">{t('discover.watchlist.emptyTitle')}</h3>
                    <p class="text-sm text-w-muted">{t('discover.watchlist.emptyHint')}</p>
                </div>
            )}

            <ItemGrid
                items={displayItems}
                showBadges={showBadges}
                userStatuses={state.userStatuses}
                watchlistIds={state.watchlistIds}
                onClick={cardClick}
                onToggleWatched={handleToggleWatched}
                onRate={handleOpenRating}
                onToggleWatchlist={handleToggleWatchlist}
            />

            {!state.isSearchMode && !state.watchlistFilterEnabled && !state.catalogLoading && state.hasMore && state.items.length > 0 && (
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
                    hasCustomAddons={hasCustomAddons || addonsInstalled}
                    onSetupAddons={onSetupAddons}
                    userStatuses={state.userStatuses}
                    watchlistIds={state.watchlistIds}
                    onToggleWatched={handleToggleWatched}
                    onRate={handleOpenRating}
                    onToggleWatchlist={handleToggleWatchlist}
                />
            )}

            {showWizard && state.phase === 'ready' && (
                <AddonWizard onComplete={onWizardComplete} onSkip={onWizardSkip} />
            )}

            {ratingTarget && (
                <RatingDialog
                    currentRating={ratingTarget.currentRating}
                    onRate={handleRate}
                    onUnrate={handleUnrate}
                    onClose={handleCloseRating}
                />
            )}

        </div>
    );
}
