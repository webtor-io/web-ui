import av from '../lib/av';
import { StremioClient, CINEMETA_BASE } from '../lib/discover/client';
import { DiscoverState } from '../lib/discover/state';
import { DiscoverUI } from '../lib/discover/ui';

function debounce(fn, delay) {
    let timer;
    return (...args) => {
        clearTimeout(timer);
        timer = setTimeout(() => fn(...args), delay);
    };
}

av(function () {
    const container = this;
    const isDiscoverPage = container.id === 'discover-page' ||
        container.querySelector?.('#discover-page');

    if (!isDiscoverPage) {
        // Fallback: old ribbon behavior
        const modal = container.querySelector('#discover-modal');
        if (modal) {
            const page = container;
            page.querySelectorAll('.discover-open').forEach(btn => {
                btn.addEventListener('click', () => {
                    modal.showModal();
                    window.umami?.track('discover-modal-shown');
                });
            });
            modal.querySelectorAll('.discover-close').forEach(btn => {
                btn.addEventListener('click', () => modal.close());
            });
        }
        return;
    }

    const addonUrls = [...(window._addonUrls || [])];
    if (!addonUrls.some(u => u.replace(/\/manifest\.json$/, '') === CINEMETA_BASE)) {
        addonUrls.unshift(CINEMETA_BASE);
    }

    const page = container.id === 'discover-page'
        ? container
        : container.querySelector('#discover-page') || container;
    const ui = new DiscoverUI(page);
    const state = new DiscoverState();
    const client = new StremioClient(addonUrls);

    // AbortController for in-flight catalog/search requests
    let catalogAbort = null;

    function abortCatalog() {
        if (catalogAbort) {
            catalogAbort.abort();
            catalogAbort = null;
        }
    }

    // --- Init ---
    async function init() {
        ui.showLoading();
        try {
            const manifests = await client.fetchAllManifests();
            client.manifests = manifests;
            state.manifests = manifests;
            state.buildCatalogs();
            state.buildSearchCatalogs(client);

            if (ui.els.search) ui.els.search.classList.remove('hidden');

            const types = state.getTypes();
            if (!types.length) {
                ui.hideAll();
                ui.showNoCatalogs();
                return;
            }

            state.selectedType = types[0];
            const catalogs = state.getCatalogsForType(state.selectedType);
            state.selectedCatalog = catalogs[0] || null;

            ui.els.loading.classList.add('hidden');
            renderControls();
            await loadCatalog();
        } catch (e) {
            ui.showError('Failed to load addon manifests. Please try again.');
        }
    }

    // --- Controls ---
    function renderControls() {
        if (state.isSearchMode) {
            renderSearchControls();
            return;
        }
        ui.renderTypeTabs(state.getTypes(), state.selectedType, async (type) => {
            abortCatalog();
            state.selectedType = type;
            const catalogs = state.getCatalogsForType(type);
            state.selectedCatalog = catalogs[0] || null;
            state.resetItems();
            renderControls();
            await loadCatalog();
        });

        const catalogs = state.getCatalogsForType(state.selectedType);
        ui.renderCatalogSelector(catalogs, state.selectedCatalog, async (cat) => {
            abortCatalog();
            state.selectedCatalog = cat;
            state.resetItems();
            await loadCatalog();
        });
    }

    // --- Catalog loading ---
    async function loadCatalog() {
        if (!state.selectedCatalog) return;
        if (state.skip === 0) ui.els.grid.innerHTML = '';
        ui.showLoadMore(false);
        ui.els.loading.classList.remove('hidden');

        abortCatalog();
        catalogAbort = new AbortController();
        const { signal } = catalogAbort;

        try {
            const data = await client.fetchCatalog(
                state.selectedCatalog.baseUrl,
                state.selectedType,
                state.selectedCatalog.id,
                state.skip,
                { signal }
            );
            ui.els.loading.classList.add('hidden');

            let metas = data.metas || [];
            if (!metas.length && state.skip === 0) {
                ui.els.grid.innerHTML = '<p class="text-w-muted text-center col-span-full py-8">No items found.</p>';
                return;
            }

            // Deduplicate against already loaded items
            if (state.skip > 0) {
                const existingIds = new Set(state.items.map(i => i.id));
                metas = metas.filter(m => !existingIds.has(m.id));
            }

            state.items = state.items.concat(metas);
            state.hasMore = (data.metas || []).length > 0;

            ui.renderItems(metas, state.skip > 0);
            ui.showLoadMore(state.hasMore);
            bindCardClicks();

            window.umami?.track('discover-catalog-loaded', {
                type: state.selectedType,
                catalog: state.selectedCatalog.id,
            });
        } catch (e) {
            if (e.name === 'AbortError') return;
            ui.els.loading.classList.add('hidden');
            if (state.skip === 0) {
                ui.showError('Failed to load catalog. Please try again.');
            }
        }
    }

    // --- Search ---
    let searchGeneration = 0;

    async function performSearch(query) {
        query = query.trim();
        if (query.length < 2) {
            if (state.isSearchMode) restoreCatalogBrowsing();
            return;
        }

        const gen = ++searchGeneration;
        state.isSearchMode = true;
        state.searchQuery = query;
        state.searchResults = [];

        abortCatalog();
        catalogAbort = new AbortController();
        const { signal } = catalogAbort;

        ui.showSearchLoading();
        ui.els.catalogSelector.innerHTML = '';

        // Build search sources: always include Cinemeta + user's search-capable catalogs
        const sources = [];
        const hasCinemetaMovie = state.searchCatalogs.some(
            sc => sc.baseUrl === CINEMETA_BASE && sc.type === 'movie' && sc.id === 'top'
        );
        const hasCinemetaSeries = state.searchCatalogs.some(
            sc => sc.baseUrl === CINEMETA_BASE && sc.type === 'series' && sc.id === 'top'
        );
        if (!hasCinemetaMovie) sources.push({ baseUrl: CINEMETA_BASE, type: 'movie', id: 'top' });
        if (!hasCinemetaSeries) sources.push({ baseUrl: CINEMETA_BASE, type: 'series', id: 'top' });
        sources.push(...state.searchCatalogs);

        const results = await Promise.allSettled(
            sources.map(src => client.searchCatalog(src.baseUrl, src.type, src.id, query, { signal }))
        );

        if (gen !== searchGeneration) return;

        // Merge and deduplicate
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

        state.searchResults = merged;
        ui.els.loading.classList.add('hidden');

        if (!merged.length) {
            ui.els.typeTabs.innerHTML = '';
            ui.showNoResults(query);
            return;
        }

        state.selectedType = 'all';
        renderSearchControls();
        renderSearchResults();

        window.umami?.track('discover-search', { query, count: merged.length });
    }

    function renderSearchControls() {
        const searchTypes = state.getSearchTypes();
        const tabs = [
            { key: 'all', label: 'All', count: state.searchResults.length },
            ...searchTypes.map(t => ({
                key: t,
                label: t.charAt(0).toUpperCase() + t.slice(1),
                count: state.getSearchResultsForType(t).length,
            })),
        ];

        ui.els.typeTabs.innerHTML = '';
        for (const tab of tabs) {
            const btn = document.createElement('button');
            btn.textContent = `${tab.label} (${tab.count})`;
            btn.className = tab.key === state.selectedType
                ? 'btn btn-sm bg-w-cyan/15 border border-w-cyan/30 text-w-cyan'
                : 'btn btn-sm btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan';
            btn.setAttribute('data-tab-key', tab.key);
            btn.addEventListener('click', () => {
                state.selectedType = tab.key;
                renderSearchControls();
                renderSearchResults();
            });
            ui.els.typeTabs.appendChild(btn);
        }
        ui.els.catalogSelector.innerHTML = '';
    }

    function renderSearchResults() {
        const filtered = state.selectedType === 'all'
            ? state.searchResults
            : state.getSearchResultsForType(state.selectedType);
        state.items = state.searchResults;
        const showBadges = state.isSearchMode && state.selectedType === 'all';
        ui.renderItems(filtered, false, showBadges);
        ui.showLoadMore(false);
        if (ui.els.noResults) ui.els.noResults.classList.add('hidden');
        bindCardClicks();
    }

    function restoreCatalogBrowsing() {
        abortCatalog();
        state.resetSearch();
        if (ui.els.noResults) ui.els.noResults.classList.add('hidden');

        const types = state.getTypes();
        if (types.length) {
            state.selectedType = types[0];
            state.selectedCatalog = state.getCatalogsForType(state.selectedType)[0] || null;
            state.resetItems();
            renderControls();
            loadCatalog();
        }
    }

    function exitSearchMode() {
        if (ui.els.searchInput) ui.els.searchInput.value = '';
        ui.setSearchClearVisible(false);
        restoreCatalogBrowsing();
    }

    // --- Streams ---
    async function showStreamsForId(type, id, item) {
        ui.showStreamModal(item.name, item.poster, [], 'Loading streams...');
        try {
            const streams = await client.fetchStreams(type, id);
            ui.showStreamModal(item.name, item.poster, streams);
            window.umami?.track('discover-streams-loaded', { type, id, count: streams.length });
        } catch (e) {
            ui.showStreamModal(item.name, item.poster, []);
        }
    }

    // --- Card click bindings ---
    function bindCardClicks() {
        const cards = ui.els.grid.querySelectorAll('[data-item-id]');
        for (const card of cards) {
            if (card._bound) continue;
            card._bound = true;
            card.addEventListener('click', async () => {
                const id = card.getAttribute('data-item-id');
                const type = card.getAttribute('data-item-type') || state.selectedType;
                const item = state.items.find(i => i.id === id);
                if (!item) return;

                if (type === 'series') {
                    ui.showStreamModal(item.name, item.poster, [], 'Loading episodes...');
                    try {
                        const meta = await client.fetchMeta(type, id);
                        if (meta?.videos?.length > 0) {
                            ui.showEpisodePicker(item.name, item.poster, meta, async (episode) => {
                                const epId = episode.id || `${id}:${episode.season}:${episode.episode}`;
                                await showStreamsForId(type, epId, {
                                    name: `${item.name} - S${episode.season || '?'}E${episode.episode || '?'}`,
                                    poster: item.poster,
                                });
                            });
                        } else {
                            await showStreamsForId(type, id, item);
                        }
                    } catch (e) {
                        await showStreamsForId(type, id, item);
                    }
                } else {
                    await showStreamsForId(type, id, item);
                }
            });
        }
    }

    // --- Event bindings ---
    ui.els.loadMoreBtn.addEventListener('click', async () => {
        state.skip = state.items.length;
        await loadCatalog();
    });

    ui.els.retry.addEventListener('click', () => init());

    if (ui.els.searchInput) {
        const debouncedSearch = debounce(query => performSearch(query), 400);

        ui.els.searchInput.addEventListener('input', () => {
            const val = ui.els.searchInput.value;
            ui.setSearchClearVisible(val.length > 0);
            debouncedSearch(val);
        });

        ui.els.searchInput.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                exitSearchMode();
                ui.els.searchInput.blur();
            }
        });
    }

    if (ui.els.searchClear) {
        ui.els.searchClear.addEventListener('click', () => exitSearchMode());
    }

    init();
});

export {};
