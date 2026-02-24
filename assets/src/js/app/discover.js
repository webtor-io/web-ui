import av from '../lib/av';

// --- Stremio Client ---
var CINEMETA_BASE = 'https://v3-cinemeta.strem.io';

class StremioClient {
    constructor(addonUrls) {
        this.addonUrls = addonUrls.map(u => u.replace(/\/manifest\.json$/, ''));
        this.cache = {};
    }

    async fetchManifest(baseUrl) {
        const url = baseUrl + '/manifest.json';
        const res = await fetch(url);
        if (!res.ok) throw new Error('Failed to fetch manifest from ' + url);
        return res.json();
    }

    async fetchAllManifests() {
        const results = await Promise.allSettled(
            this.addonUrls.map(async (url) => {
                const manifest = await this.fetchManifest(url);
                return { baseUrl: url, manifest };
            })
        );
        return results
            .filter(r => r.status === 'fulfilled')
            .map(r => r.value);
    }

    async fetchCatalog(baseUrl, type, catalogId, skip) {
        skip = skip || 0;
        const cacheKey = baseUrl + '/' + type + '/' + catalogId + '/' + skip;
        if (this.cache[cacheKey]) return this.cache[cacheKey];
        const url = baseUrl + '/catalog/' + type + '/' + catalogId + (skip > 0 ? '/skip=' + skip : '') + '.json';
        const res = await fetch(url);
        if (!res.ok) throw new Error('Failed to fetch catalog');
        const data = await res.json();
        this.cache[cacheKey] = data;
        return data;
    }

    async fetchMeta(type, id) {
        // Always try Cinemeta first — it's the standard meta provider
        try {
            const url = CINEMETA_BASE + '/meta/' + type + '/' + id + '.json';
            const res = await fetch(url);
            if (res.ok) {
                const data = await res.json();
                if (data.meta && data.meta.videos && data.meta.videos.length > 0) {
                    return data.meta;
                }
            }
        } catch (e) { /* fall through to user addons */ }

        // Fall back to user's meta-capable addons
        const metaAddons = this.manifests
            ? this.manifests.filter(m => {
                const resources = m.manifest.resources || [];
                return resources.some(r =>
                    (typeof r === 'string' && r === 'meta') ||
                    (r && r.name === 'meta')
                );
            })
            : [];
        const results = await Promise.allSettled(
            metaAddons.map(async (addon) => {
                const url = addon.baseUrl + '/meta/' + type + '/' + id + '.json';
                const res = await fetch(url);
                if (!res.ok) throw new Error('Failed to fetch meta');
                const data = await res.json();
                return data.meta || null;
            })
        );
        for (const r of results) {
            if (r.status === 'fulfilled' && r.value && r.value.videos && r.value.videos.length > 0) {
                return r.value;
            }
        }
        for (const r of results) {
            if (r.status === 'fulfilled' && r.value) return r.value;
        }
        return null;
    }

    getSearchCatalogs() {
        var catalogs = [];
        if (!this.manifests) return catalogs;
        for (var i = 0; i < this.manifests.length; i++) {
            var m = this.manifests[i];
            var cats = m.manifest.catalogs || [];
            for (var j = 0; j < cats.length; j++) {
                var cat = cats[j];
                var hasSearch = false;
                if (cat.extraSupported && cat.extraSupported.indexOf('search') !== -1) {
                    hasSearch = true;
                }
                if (!hasSearch && cat.extra) {
                    for (var k = 0; k < cat.extra.length; k++) {
                        if (cat.extra[k].name === 'search') { hasSearch = true; break; }
                    }
                }
                if (hasSearch) {
                    catalogs.push({
                        id: cat.id,
                        type: cat.type,
                        name: cat.name || cat.id,
                        addonName: m.manifest.name || 'Unknown',
                        baseUrl: m.baseUrl
                    });
                }
            }
        }
        return catalogs;
    }

    async searchCatalog(baseUrl, type, catalogId, query) {
        var controller = new AbortController();
        var timeoutId = setTimeout(function() { controller.abort(); }, 8000);
        try {
            var url = baseUrl + '/catalog/' + type + '/' + catalogId + '/search=' + encodeURIComponent(query) + '.json';
            var res = await fetch(url, { signal: controller.signal });
            clearTimeout(timeoutId);
            if (!res.ok) throw new Error('Search failed');
            var data = await res.json();
            return data.metas || [];
        } catch (e) {
            clearTimeout(timeoutId);
            throw e;
        }
    }

    async fetchStreams(type, id) {
        const streamAddons = this.manifests
            ? this.manifests.filter(m => {
                const resources = m.manifest.resources || [];
                return resources.some(r =>
                    (typeof r === 'string' && r === 'stream') ||
                    (r && r.name === 'stream')
                );
            })
            : [];
        const results = await Promise.allSettled(
            streamAddons.map(async (addon) => {
                const url = addon.baseUrl + '/stream/' + type + '/' + id + '.json';
                const res = await fetch(url);
                if (!res.ok) throw new Error('Failed to fetch streams');
                const data = await res.json();
                return (data.streams || []).map(s => ({
                    ...s,
                    addonName: addon.manifest.name || 'Unknown'
                }));
            })
        );
        const streams = [];
        for (const r of results) {
            if (r.status === 'fulfilled') streams.push(...r.value);
        }
        return streams;
    }
}

// --- Discover State ---
class DiscoverState {
    constructor() {
        this.manifests = [];
        this.catalogs = [];
        this.selectedType = null;
        this.selectedCatalog = null;
        this.items = [];
        this.skip = 0;
        this.hasMore = true;
        this.pageSize = 100;
        // Search state
        this.searchQuery = '';
        this.searchResults = [];
        this.isSearchMode = false;
        this.searchCatalogs = [];
    }

    buildSearchCatalogs(client) {
        this.searchCatalogs = client.getSearchCatalogs();
    }

    getSearchResultsForType(type) {
        return this.searchResults.filter(function(item) { return item.type === type; });
    }

    getSearchTypes() {
        var typeSet = {};
        for (var i = 0; i < this.searchResults.length; i++) {
            typeSet[this.searchResults[i].type] = true;
        }
        var types = Object.keys(typeSet);
        var priority = ['movie', 'series'];
        types.sort(function(a, b) {
            var ai = priority.indexOf(a);
            var bi = priority.indexOf(b);
            if (ai !== -1 && bi !== -1) return ai - bi;
            if (ai !== -1) return -1;
            if (bi !== -1) return 1;
            return a.localeCompare(b);
        });
        return types;
    }

    resetSearch() {
        this.searchQuery = '';
        this.searchResults = [];
        this.isSearchMode = false;
    }

    buildCatalogs() {
        this.catalogs = [];
        for (const m of this.manifests) {
            const cats = m.manifest.catalogs || [];
            for (const cat of cats) {
                this.catalogs.push({
                    id: cat.id,
                    type: cat.type,
                    name: cat.name || cat.id,
                    addonName: m.manifest.name || 'Unknown',
                    baseUrl: m.baseUrl
                });
            }
        }
    }

    getTypes() {
        const types = [...new Set(this.catalogs.map(c => c.type))];
        const priority = ['movie', 'series'];
        types.sort((a, b) => {
            const ai = priority.indexOf(a);
            const bi = priority.indexOf(b);
            if (ai !== -1 && bi !== -1) return ai - bi;
            if (ai !== -1) return -1;
            if (bi !== -1) return 1;
            return a.localeCompare(b);
        });
        return types;
    }

    getCatalogsForType(type) {
        return this.catalogs.filter(c => c.type === type);
    }

    resetItems() {
        this.items = [];
        this.skip = 0;
        this.hasMore = true;
    }
}

// --- Discover UI ---
class DiscoverUI {
    constructor(container) {
        this.container = container;
        this.els = {
            typeTabs: container.querySelector('#discover-type-tabs'),
            catalogSelector: container.querySelector('#discover-catalog-selector'),
            loading: container.querySelector('#discover-loading'),
            noAddons: container.querySelector('#discover-no-addons'),
            noCatalogs: container.querySelector('#discover-no-catalogs'),
            error: container.querySelector('#discover-error'),
            errorMessage: container.querySelector('#discover-error-message'),
            retry: container.querySelector('#discover-retry'),
            grid: container.querySelector('#discover-grid'),
            loadMore: container.querySelector('#discover-load-more'),
            loadMoreBtn: container.querySelector('#discover-load-more-btn'),
            streamModal: container.querySelector('#discover-stream-modal'),
            streamContent: container.querySelector('#stream-modal-content'),
            search: container.querySelector('#discover-search'),
            searchInput: container.querySelector('#discover-search-input'),
            searchClear: container.querySelector('#discover-search-clear'),
            noResults: container.querySelector('#discover-no-results'),
            noResultsMessage: container.querySelector('#discover-no-results-message'),
        };
    }

    hideAll() {
        this.els.loading.classList.add('hidden');
        this.els.noAddons.classList.add('hidden');
        this.els.noCatalogs.classList.add('hidden');
        this.els.error.classList.add('hidden');
        this.els.grid.innerHTML = '';
        this.els.loadMore.classList.add('hidden');
        this.els.typeTabs.innerHTML = '';
        this.els.catalogSelector.innerHTML = '';
        if (this.els.noResults) this.els.noResults.classList.add('hidden');
    }

    showNoResults(query) {
        if (this.els.noResults) {
            this.els.noResults.classList.remove('hidden');
            this.els.noResultsMessage.textContent = 'No results found for "' + query + '". Try a different search term.';
        }
    }

    showSearchLoading() {
        this.els.grid.innerHTML = '';
        this.els.loadMore.classList.add('hidden');
        if (this.els.noResults) this.els.noResults.classList.add('hidden');
        this.els.loading.classList.remove('hidden');
    }

    setSearchClearVisible(visible) {
        if (this.els.searchClear) {
            if (visible) {
                this.els.searchClear.classList.remove('hidden');
            } else {
                this.els.searchClear.classList.add('hidden');
            }
        }
    }

    showLoading() {
        this.hideAll();
        this.els.loading.classList.remove('hidden');
    }

    showNoAddons() {
        this.hideAll();
        this.els.noAddons.classList.remove('hidden');
    }

    showNoCatalogs() {
        this.els.noCatalogs.classList.remove('hidden');
    }

    showError(message) {
        this.hideAll();
        this.els.errorMessage.textContent = message || 'Could not load content.';
        this.els.error.classList.remove('hidden');
    }

    renderTypeTabs(types, selectedType, onSelect) {
        this.els.typeTabs.innerHTML = '';
        for (const type of types) {
            const btn = document.createElement('button');
            btn.textContent = type.charAt(0).toUpperCase() + type.slice(1);
            btn.className = type === selectedType
                ? 'btn btn-sm bg-w-cyan/15 border border-w-cyan/30 text-w-cyan'
                : 'btn btn-sm btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan';
            btn.addEventListener('click', function() { onSelect(type); });
            this.els.typeTabs.appendChild(btn);
        }
    }

    renderCatalogSelector(catalogs, selectedCatalog, onSelect) {
        this.els.catalogSelector.innerHTML = '';
        if (catalogs.length <= 1) return;
        const select = document.createElement('select');
        select.className = 'select select-sm bg-w-surface border-w-line text-w-text';
        for (const cat of catalogs) {
            const option = document.createElement('option');
            option.value = cat.baseUrl + '::' + cat.id;
            option.textContent = cat.name + ' (' + cat.addonName + ')';
            if (selectedCatalog && cat.baseUrl === selectedCatalog.baseUrl && cat.id === selectedCatalog.id) {
                option.selected = true;
            }
            select.appendChild(option);
        }
        select.addEventListener('change', function() {
            var parts = select.value.split('::');
            var match = catalogs.find(function(c) { return c.baseUrl === parts[0] && c.id === parts[1]; });
            if (match) onSelect(match);
        });
        this.els.catalogSelector.appendChild(select);
    }

    renderItems(items, append, showBadges) {
        if (!append) this.els.grid.innerHTML = '';
        for (const item of items) {
            var card = document.createElement('div');
            card.className = 'group cursor-pointer';
            card.setAttribute('data-item-id', item.id || '');
            card.setAttribute('data-item-type', item.type || '');

            var inner = document.createElement('div');
            inner.className = 'bg-w-card border border-w-line rounded-xl overflow-hidden hover:border-w-cyan/30 transition-all duration-300 flex flex-col w-full';

            var figure = document.createElement('figure');
            figure.className = 'aspect-[2/3] overflow-hidden relative';

            if (item.poster) {
                var img = document.createElement('img');
                img.className = 'w-full h-full object-cover group-hover:scale-105 transition-transform duration-300';
                img.src = item.poster;
                img.alt = item.name || '';
                img.loading = 'lazy';
                figure.appendChild(img);
            } else {
                var placeholder = document.createElement('div');
                placeholder.className = 'w-full h-full bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 text-w-purpleL/60 flex items-center justify-center';
                var titleDiv = document.createElement('div');
                titleDiv.className = 'text-center font-bold text-lg p-3 line-clamp-3 drop-shadow-sm';
                titleDiv.textContent = item.name || 'Unknown';
                placeholder.appendChild(titleDiv);
                figure.appendChild(placeholder);
            }

            if (showBadges && item.type) {
                var badge = document.createElement('span');
                badge.className = 'absolute top-2 left-2 text-[10px] font-semibold uppercase px-1.5 py-0.5 rounded bg-black/60 text-white backdrop-blur-sm';
                badge.textContent = item.type === 'series' ? 'Series' : item.type.charAt(0).toUpperCase() + item.type.slice(1);
                figure.appendChild(badge);
            }

            inner.appendChild(figure);

            var info = document.createElement('div');
            info.className = 'p-3';
            var title = document.createElement('h3');
            title.className = 'font-semibold text-sm line-clamp-1 group-hover:text-w-cyan transition-colors';
            title.textContent = item.name || 'Unknown';
            info.appendChild(title);

            if (item.releaseInfo || item.year) {
                var meta = document.createElement('span');
                meta.className = 'text-xs text-w-muted mt-1 block';
                meta.textContent = item.releaseInfo || item.year || '';
                info.appendChild(meta);
            }
            inner.appendChild(info);
            card.appendChild(inner);
            this.els.grid.appendChild(card);
        }
    }

    showLoadMore(show) {
        if (show) {
            this.els.loadMore.classList.remove('hidden');
        } else {
            this.els.loadMore.classList.add('hidden');
        }
    }

    renderModalHeader(content, title, poster, subtitleText) {
        var header = document.createElement('div');
        header.className = 'flex gap-4 mb-4';

        if (poster) {
            var img = document.createElement('img');
            img.src = poster;
            img.alt = title || '';
            img.className = 'w-20 h-28 object-cover rounded-lg flex-shrink-0';
            header.appendChild(img);
        }

        var titleDiv = document.createElement('div');
        titleDiv.className = 'flex flex-col justify-center min-w-0';
        var h3 = document.createElement('h3');
        h3.className = 'font-bold text-lg line-clamp-2';
        h3.textContent = title || 'Unknown';
        titleDiv.appendChild(h3);
        var subtitle = document.createElement('p');
        subtitle.className = 'text-sm text-w-muted mt-1';
        subtitle.textContent = subtitleText || '';
        titleDiv.appendChild(subtitle);
        header.appendChild(titleDiv);
        content.appendChild(header);
    }

    showEpisodePicker(title, poster, meta, onEpisodeSelect) {
        var content = this.els.streamContent;
        content.innerHTML = '';

        this.renderModalHeader(content, title, poster, 'Select an episode');

        var videos = (meta && meta.videos) || [];
        if (!videos.length) {
            var empty = document.createElement('p');
            empty.className = 'text-w-muted text-sm text-center py-6';
            empty.textContent = 'No episodes found.';
            content.appendChild(empty);
            this.els.streamModal.showModal();
            return;
        }

        // Group by season
        var seasons = {};
        for (var i = 0; i < videos.length; i++) {
            var v = videos[i];
            var s = v.season != null ? v.season : 0;
            if (!seasons[s]) seasons[s] = [];
            seasons[s].push(v);
        }
        var seasonNums = Object.keys(seasons).map(Number).sort(function(a, b) {
            if (a === 0) return 1;
            if (b === 0) return -1;
            return a - b;
        });

        // Season tabs
        if (seasonNums.length > 1) {
            var tabsRow = document.createElement('div');
            tabsRow.className = 'flex gap-1.5 mb-3 flex-wrap';
            var self = this;
            seasonNums.forEach(function(sn, idx) {
                var btn = document.createElement('button');
                btn.textContent = sn === 0 ? 'Specials' : 'S' + sn;
                btn.className = idx === 0
                    ? 'btn btn-xs bg-w-cyan/15 border border-w-cyan/30 text-w-cyan'
                    : 'btn btn-xs btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan';
                btn.addEventListener('click', function() {
                    // Update active tab style
                    tabsRow.querySelectorAll('button').forEach(function(b) {
                        b.className = 'btn btn-xs btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan';
                    });
                    btn.className = 'btn btn-xs bg-w-cyan/15 border border-w-cyan/30 text-w-cyan';
                    renderEpisodes(sn);
                });
                tabsRow.appendChild(btn);
            });
            content.appendChild(tabsRow);
        }

        var episodeContainer = document.createElement('div');
        episodeContainer.className = 'max-h-[350px] overflow-y-auto';
        content.appendChild(episodeContainer);

        function renderEpisodes(seasonNum) {
            episodeContainer.innerHTML = '';
            var eps = seasons[seasonNum] || [];
            eps.sort(function(a, b) { return (a.episode || 0) - (b.episode || 0); });

            var list = document.createElement('div');
            list.className = 'flex flex-col gap-1.5';

            for (var j = 0; j < eps.length; j++) {
                var ep = eps[j];
                (function(episode) {
                    var row = document.createElement('button');
                    row.className = 'flex items-center gap-3 p-2.5 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all w-full text-left cursor-pointer bg-transparent';

                    var epNum = document.createElement('span');
                    epNum.className = 'flex-shrink-0 w-8 h-8 rounded-full bg-w-cyan/10 flex items-center justify-center text-xs font-bold text-w-cyan';
                    epNum.textContent = episode.episode != null ? episode.episode : '?';
                    row.appendChild(epNum);

                    var epInfo = document.createElement('div');
                    epInfo.className = 'min-w-0 flex-1';
                    var epTitle = document.createElement('div');
                    epTitle.className = 'text-sm font-medium line-clamp-1';
                    epTitle.textContent = episode.title || episode.name || ('Episode ' + (episode.episode || '?'));
                    epInfo.appendChild(epTitle);

                    if (episode.released || episode.overview) {
                        var epMeta = document.createElement('div');
                        epMeta.className = 'text-xs text-w-muted line-clamp-1';
                        epMeta.textContent = episode.released ? new Date(episode.released).toLocaleDateString() : (episode.overview || '');
                        epInfo.appendChild(epMeta);
                    }
                    row.appendChild(epInfo);

                    var arrow = document.createElement('span');
                    arrow.className = 'text-w-muted flex-shrink-0';
                    arrow.innerHTML = '<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18l6-6-6-6"/></svg>';
                    row.appendChild(arrow);

                    row.addEventListener('click', function() {
                        onEpisodeSelect(episode);
                    });
                    list.appendChild(row);
                })(ep);
            }
            episodeContainer.appendChild(list);
        }

        renderEpisodes(seasonNums[0]);
        this.els.streamModal.showModal();
    }

    showStreamModal(title, poster, streams, subtitle) {
        var content = this.els.streamContent;
        content.innerHTML = '';

        var subText = subtitle || (streams.length + ' stream' + (streams.length !== 1 ? 's' : '') + ' found');
        this.renderModalHeader(content, title, poster, subText);

        if (streams.length === 0) {
            var empty = document.createElement('p');
            empty.className = 'text-w-muted text-sm text-center py-6';
            empty.textContent = 'No streams available for this title.';
            content.appendChild(empty);
            this.els.streamModal.showModal();
            return;
        }

        // Stream list
        var list = document.createElement('div');
        list.className = 'flex flex-col gap-2 max-h-[400px] overflow-y-auto';

        for (var i = 0; i < streams.length; i++) {
            var stream = streams[i];
            var infoHash = this.extractInfoHash(stream);
            var el;

            if (infoHash) {
                el = document.createElement('a');
                el.href = '/' + infoHash;
                el.setAttribute('data-async-target', 'main');
                el.addEventListener('click', (function(modal) {
                    return function() { modal.close(); };
                })(this.els.streamModal));
            } else {
                el = document.createElement('div');
                el.className = 'opacity-50';
            }

            el.className = (el.className || '') + ' flex items-center gap-3 p-3 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all';

            var icon = document.createElement('div');
            icon.className = 'flex-shrink-0 w-8 h-8 rounded-full bg-w-cyan/10 flex items-center justify-center';
            icon.innerHTML = '<svg class="w-4 h-4 text-w-cyan" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>';
            el.appendChild(icon);

            var streamInfo = document.createElement('div');
            streamInfo.className = 'min-w-0 flex-1';

            var streamName = document.createElement('div');
            streamName.className = 'text-sm font-medium line-clamp-1';
            streamName.textContent = stream.name || stream.addonName || 'Stream';
            streamInfo.appendChild(streamName);

            if (stream.title) {
                var streamTitle = document.createElement('div');
                streamTitle.className = 'text-xs text-w-sub line-clamp-2 whitespace-pre-line';
                streamTitle.textContent = stream.title;
                streamInfo.appendChild(streamTitle);
            }

            var streamDesc = document.createElement('div');
            streamDesc.className = 'text-xs text-w-muted line-clamp-1';
            var descParts = [];
            if (stream.addonName) descParts.push(stream.addonName);
            if (stream.description) descParts.push(stream.description);
            streamDesc.textContent = descParts.join(' \u00b7 ') || '';
            streamInfo.appendChild(streamDesc);

            el.appendChild(streamInfo);

            if (!infoHash) {
                var badge = document.createElement('span');
                badge.className = 'text-xs text-w-muted flex-shrink-0';
                badge.textContent = 'No torrent';
                el.appendChild(badge);
            }

            list.appendChild(el);
        }

        content.appendChild(list);
        this.els.streamModal.showModal();
    }

    extractInfoHash(stream) {
        if (stream.infoHash) return stream.infoHash;
        if (stream.url) {
            var match = stream.url.match(/([0-9a-fA-F]{40})/);
            if (match) return match[1];
        }
        if (stream.externalUrl) {
            var match2 = stream.externalUrl.match(/([0-9a-fA-F]{40})/);
            if (match2) return match2[1];
        }
        return null;
    }
}

// --- Main controller ---
av(function() {
    var container = this;
    var isDiscoverPage = container.id === 'discover-page' || (container.querySelector && container.querySelector('#discover-page'));

    // Only init on the discover page
    if (!isDiscoverPage) {
        // Fallback: old ribbon behavior
        var modal = container.querySelector('#discover-modal');
        if (modal) {
            var openBtns = page.querySelectorAll('.discover-open');
            var closeBtns = modal.querySelectorAll('.discover-close');
            openBtns.forEach(function(btn) {
                btn.addEventListener('click', function() {
                    modal.showModal();
                    if (window.umami) window.umami.track('discover-modal-shown');
                });
            });
            closeBtns.forEach(function(btn) {
                btn.addEventListener('click', function() { modal.close(); });
            });
        }
        return;
    }

    var addonUrls = window._addonUrls || [];

    // Always include Cinemeta catalogs by default
    var hasCinemeta = false;
    for (var i = 0; i < addonUrls.length; i++) {
        if (addonUrls[i].replace(/\/manifest\.json$/, '') === CINEMETA_BASE) {
            hasCinemeta = true;
            break;
        }
    }
    if (!hasCinemeta) {
        addonUrls.unshift(CINEMETA_BASE);
    }

    var page = container.id === 'discover-page' ? container : container.querySelector('#discover-page') || container;
    var ui = new DiscoverUI(page);
    var state = new DiscoverState();

    var client = new StremioClient(addonUrls);

    async function init() {
        ui.showLoading();
        try {
            var manifests = await client.fetchAllManifests();
            client.manifests = manifests;
            state.manifests = manifests;
            state.buildCatalogs();
            state.buildSearchCatalogs(client);

            // Show search bar once manifests are loaded
            if (ui.els.search) ui.els.search.classList.remove('hidden');

            var types = state.getTypes();
            if (!types.length) {
                ui.hideAll();
                ui.showNoCatalogs();
                return;
            }

            state.selectedType = types[0];
            var catalogs = state.getCatalogsForType(state.selectedType);
            state.selectedCatalog = catalogs[0] || null;

            ui.els.loading.classList.add('hidden');
            renderControls();
            await loadCatalog();
        } catch (e) {
            ui.showError('Failed to load addon manifests. Please try again.');
        }
    }

    function renderControls() {
        if (state.isSearchMode) {
            renderSearchControls();
            return;
        }
        var types = state.getTypes();
        ui.renderTypeTabs(types, state.selectedType, async function(type) {
            state.selectedType = type;
            var catalogs = state.getCatalogsForType(type);
            state.selectedCatalog = catalogs[0] || null;
            state.resetItems();
            renderControls();
            await loadCatalog();
        });

        var catalogs = state.getCatalogsForType(state.selectedType);
        ui.renderCatalogSelector(catalogs, state.selectedCatalog, async function(cat) {
            state.selectedCatalog = cat;
            state.resetItems();
            await loadCatalog();
        });
    }

    // --- Search logic ---
    var searchGeneration = 0;

    function debounce(fn, delay) {
        var timer;
        return function() {
            var args = arguments;
            var ctx = this;
            clearTimeout(timer);
            timer = setTimeout(function() { fn.apply(ctx, args); }, delay);
        };
    }

    async function performSearch(query) {
        query = query.trim();
        if (query.length < 2) {
            if (state.isSearchMode) {
                restoreCatalogBrowsing();
            }
            return;
        }

        var gen = ++searchGeneration;
        state.isSearchMode = true;
        state.searchQuery = query;
        state.searchResults = [];

        ui.showSearchLoading();
        ui.els.catalogSelector.innerHTML = '';

        // Build search sources: always include Cinemeta + user's search-capable catalogs
        var sources = [];

        // Always add Cinemeta for movie/top and series/top
        var cinemetaBase = CINEMETA_BASE;
        var hasCinemetaMovie = false;
        var hasCinemetaSeries = false;
        for (var i = 0; i < state.searchCatalogs.length; i++) {
            var sc = state.searchCatalogs[i];
            if (sc.baseUrl === cinemetaBase && sc.type === 'movie' && sc.id === 'top') hasCinemetaMovie = true;
            if (sc.baseUrl === cinemetaBase && sc.type === 'series' && sc.id === 'top') hasCinemetaSeries = true;
        }
        if (!hasCinemetaMovie) {
            sources.push({ baseUrl: cinemetaBase, type: 'movie', id: 'top' });
        }
        if (!hasCinemetaSeries) {
            sources.push({ baseUrl: cinemetaBase, type: 'series', id: 'top' });
        }

        // Add user's search-capable catalogs
        for (var j = 0; j < state.searchCatalogs.length; j++) {
            sources.push(state.searchCatalogs[j]);
        }

        var results = await Promise.allSettled(
            sources.map(function(src) {
                return client.searchCatalog(src.baseUrl, src.type, src.id, query);
            })
        );

        // Discard if a newer search was started
        if (gen !== searchGeneration) return;

        // Merge and deduplicate
        var seen = {};
        var merged = [];
        for (var k = 0; k < results.length; k++) {
            if (results[k].status !== 'fulfilled') continue;
            var items = results[k].value;
            var srcType = sources[k].type;
            for (var m = 0; m < items.length; m++) {
                var item = items[m];
                if (!item.type) item.type = srcType;
                if (!seen[item.id]) {
                    seen[item.id] = true;
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

        // Default to "all" in search mode
        state.selectedType = 'all';

        renderSearchControls();
        renderSearchResults();

        if (window.umami) {
            window.umami.track('discover-search', { query: query, count: merged.length });
        }
    }

    function renderSearchControls() {
        var searchTypes = state.getSearchTypes();
        // Build tabs: "All" + per-type with counts
        var tabs = [{ key: 'all', label: 'All', count: state.searchResults.length }];
        for (var i = 0; i < searchTypes.length; i++) {
            var t = searchTypes[i];
            var count = state.getSearchResultsForType(t).length;
            tabs.push({ key: t, label: t.charAt(0).toUpperCase() + t.slice(1), count: count });
        }
        ui.els.typeTabs.innerHTML = '';
        for (var j = 0; j < tabs.length; j++) {
            var tab = tabs[j];
            var btn = document.createElement('button');
            btn.textContent = tab.label + ' (' + tab.count + ')';
            btn.className = tab.key === state.selectedType
                ? 'btn btn-sm bg-w-cyan/15 border border-w-cyan/30 text-w-cyan'
                : 'btn btn-sm btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan';
            btn.setAttribute('data-tab-key', tab.key);
            btn.addEventListener('click', (function(key) {
                return function() {
                    state.selectedType = key;
                    renderSearchControls();
                    renderSearchResults();
                };
            })(tab.key));
            ui.els.typeTabs.appendChild(btn);
        }
        ui.els.catalogSelector.innerHTML = '';
    }

    function renderSearchResults() {
        var filtered = state.selectedType === 'all'
            ? state.searchResults
            : state.getSearchResultsForType(state.selectedType);
        // Ensure items are findable for card click handler
        state.items = state.searchResults;

        var showBadges = state.isSearchMode && state.selectedType === 'all';
        ui.renderItems(filtered, false, showBadges);
        ui.showLoadMore(false);
        if (ui.els.noResults) ui.els.noResults.classList.add('hidden');
        bindCardClicks();
    }

    function restoreCatalogBrowsing() {
        state.resetSearch();
        if (ui.els.noResults) ui.els.noResults.classList.add('hidden');

        var types = state.getTypes();
        if (types.length) {
            state.selectedType = types[0];
            var catalogs = state.getCatalogsForType(state.selectedType);
            state.selectedCatalog = catalogs[0] || null;
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

    async function loadCatalog() {
        if (!state.selectedCatalog) return;
        ui.els.grid.innerHTML = '';
        ui.showLoadMore(false);
        ui.els.loading.classList.remove('hidden');

        try {
            var data = await client.fetchCatalog(
                state.selectedCatalog.baseUrl,
                state.selectedType,
                state.selectedCatalog.id,
                state.skip
            );
            ui.els.loading.classList.add('hidden');

            var metas = data.metas || [];
            if (!metas.length && state.skip === 0) {
                ui.els.grid.innerHTML = '<p class="text-w-muted text-center col-span-full py-8">No items found.</p>';
                return;
            }

            state.items = state.items.concat(metas);
            state.hasMore = metas.length >= state.pageSize;

            ui.renderItems(metas, state.skip > 0);
            ui.showLoadMore(state.hasMore);
            bindCardClicks();

            if (window.umami) {
                window.umami.track('discover-catalog-loaded', {
                    type: state.selectedType,
                    catalog: state.selectedCatalog.id
                });
            }
        } catch (e) {
            ui.els.loading.classList.add('hidden');
            if (state.skip === 0) {
                ui.showError('Failed to load catalog. Please try again.');
            }
        }
    }

    async function showStreamsForId(type, id, item) {
        ui.showStreamModal(item.name, item.poster, [], 'Loading streams...');
        try {
            var streams = await client.fetchStreams(type, id);
            ui.showStreamModal(item.name, item.poster, streams);
            if (window.umami) {
                window.umami.track('discover-streams-loaded', {
                    type: type,
                    id: id,
                    count: streams.length
                });
            }
        } catch (e) {
            ui.showStreamModal(item.name, item.poster, []);
        }
    }

    function bindCardClicks() {
        var cards = ui.els.grid.querySelectorAll('[data-item-id]');
        cards.forEach(function(card) {
            if (card._bound) return;
            card._bound = true;
            card.addEventListener('click', async function() {
                var id = card.getAttribute('data-item-id');
                var type = card.getAttribute('data-item-type') || state.selectedType;
                var item = state.items.find(function(i) { return i.id === id; });
                if (!item) return;

                if (type === 'series') {
                    // Show loading in modal, then fetch meta for episodes
                    ui.showStreamModal(item.name, item.poster, [], 'Loading episodes...');
                    try {
                        var meta = await client.fetchMeta(type, id);
                        if (meta && meta.videos && meta.videos.length > 0) {
                            ui.showEpisodePicker(item.name, item.poster, meta, async function(episode) {
                                var epId = episode.id || (id + ':' + episode.season + ':' + episode.episode);
                                await showStreamsForId(type, epId, {
                                    name: item.name + ' - S' + (episode.season || '?') + 'E' + (episode.episode || '?'),
                                    poster: item.poster
                                });
                            });
                        } else {
                            // No episodes found, try direct streams
                            await showStreamsForId(type, id, item);
                        }
                    } catch (e) {
                        await showStreamsForId(type, id, item);
                    }
                } else {
                    await showStreamsForId(type, id, item);
                }
            });
        });
    }

    // Load more
    ui.els.loadMoreBtn.addEventListener('click', async function() {
        state.skip += state.pageSize;
        await loadCatalog();
    });

    // Retry
    ui.els.retry.addEventListener('click', function() {
        init();
    });

    // Search input bindings
    if (ui.els.searchInput) {
        var debouncedSearch = debounce(function(query) {
            performSearch(query);
        }, 400);

        ui.els.searchInput.addEventListener('input', function() {
            var val = ui.els.searchInput.value;
            ui.setSearchClearVisible(val.length > 0);
            debouncedSearch(val);
        });

        ui.els.searchInput.addEventListener('keydown', function(e) {
            if (e.key === 'Escape') {
                exitSearchMode();
                ui.els.searchInput.blur();
            }
        });
    }

    if (ui.els.searchClear) {
        ui.els.searchClear.addEventListener('click', function() {
            exitSearchMode();
        });
    }

    init();
});

export {}
