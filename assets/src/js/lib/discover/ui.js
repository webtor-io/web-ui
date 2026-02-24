// Discover page UI rendering

import { extractLanguages } from './lang';
import { parseStreamName, extractInfoHash } from './stream';

// --- DOM helper ---
export function el(tag, attrs, ...children) {
    const e = document.createElement(tag);
    if (attrs) {
        for (const [k, v] of Object.entries(attrs)) {
            if (k === 'className') e.className = v;
            else if (k === 'textContent') e.textContent = v;
            else if (k === 'innerHTML') e.innerHTML = v;
            else if (k.startsWith('on')) e.addEventListener(k.slice(2).toLowerCase(), v);
            else e.setAttribute(k, v);
        }
    }
    for (const child of children) {
        if (typeof child === 'string') e.appendChild(document.createTextNode(child));
        else if (child) e.appendChild(child);
    }
    return e;
}

// --- CSS class constants ---
export const CSS = {
    CHIP_ACTIVE: 'btn btn-xs bg-w-cyan/15 border border-w-cyan/30 text-w-cyan transition-all',
    CHIP_INACTIVE: 'btn btn-xs btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan transition-all',
    TAB_ACTIVE: 'btn btn-sm bg-w-cyan/15 border border-w-cyan/30 text-w-cyan',
    TAB_INACTIVE: 'btn btn-sm btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan',
    CARD: 'bg-w-card border border-w-line rounded-xl overflow-hidden hover:border-w-cyan/30 transition-all duration-300 flex flex-col w-full',
    STREAM_ROW: 'flex items-center gap-3 p-3 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all',
    EPISODE_ROW: 'flex items-center gap-3 p-2.5 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all w-full text-left cursor-pointer bg-transparent',
    LABEL_BADGE: 'bg-w-cyan/10 text-w-cyan text-[10px] px-1.5 py-0.5 rounded font-medium',
    TYPE_BADGE: 'absolute top-2 left-2 text-[10px] font-semibold uppercase px-1.5 py-0.5 rounded bg-black/60 text-white backdrop-blur-sm',
};

const PLAY_ICON_SVG = '<svg class="w-4 h-4 text-w-cyan" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>';
const ARROW_ICON_SVG = '<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18l6-6-6-6"/></svg>';

// --- UI Class ---
export class DiscoverUI {
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
            this.els.noResultsMessage.textContent = `No results found for "${query}". Try a different search term.`;
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
            this.els.searchClear.classList.toggle('hidden', !visible);
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
            const btn = el('button', {
                className: type === selectedType ? CSS.TAB_ACTIVE : CSS.TAB_INACTIVE,
                textContent: type.charAt(0).toUpperCase() + type.slice(1),
                onClick: () => onSelect(type),
            });
            this.els.typeTabs.appendChild(btn);
        }
    }

    renderCatalogSelector(catalogs, selectedCatalog, onSelect) {
        this.els.catalogSelector.innerHTML = '';
        if (catalogs.length <= 1) return;

        const select = el('select', {
            className: 'select select-sm bg-w-surface border-w-line text-w-text',
            onChange: () => {
                const parts = select.value.split('::');
                const match = catalogs.find(c => c.baseUrl === parts[0] && c.id === parts[1]);
                if (match) onSelect(match);
            },
        });

        for (const cat of catalogs) {
            const option = el('option', {
                value: `${cat.baseUrl}::${cat.id}`,
                textContent: `${cat.name} (${cat.addonName})`,
            });
            if (selectedCatalog && cat.baseUrl === selectedCatalog.baseUrl && cat.id === selectedCatalog.id) {
                option.selected = true;
            }
            select.appendChild(option);
        }
        this.els.catalogSelector.appendChild(select);
    }

    renderItems(items, append, showBadges) {
        if (!append) this.els.grid.innerHTML = '';

        for (const item of items) {
            const figure = el('figure', { className: 'aspect-[2/3] overflow-hidden relative' });

            if (item.poster) {
                figure.appendChild(el('img', {
                    className: 'w-full h-full object-cover group-hover:scale-105 transition-transform duration-300',
                    src: item.poster,
                    alt: item.name || '',
                    loading: 'lazy',
                }));
            } else {
                figure.appendChild(
                    el('div', { className: 'w-full h-full bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 text-w-purpleL/60 flex items-center justify-center' },
                        el('div', {
                            className: 'text-center font-bold text-lg p-3 line-clamp-3 drop-shadow-sm',
                            textContent: item.name || 'Unknown',
                        })
                    )
                );
            }

            if (showBadges && item.type) {
                figure.appendChild(el('span', {
                    className: CSS.TYPE_BADGE,
                    textContent: item.type === 'series' ? 'Series' : item.type.charAt(0).toUpperCase() + item.type.slice(1),
                }));
            }

            const info = el('div', { className: 'p-3' },
                el('h3', {
                    className: 'font-semibold text-sm line-clamp-1 group-hover:text-w-cyan transition-colors',
                    textContent: item.name || 'Unknown',
                })
            );

            if (item.releaseInfo || item.year) {
                info.appendChild(el('span', {
                    className: 'text-xs text-w-muted mt-1 block',
                    textContent: item.releaseInfo || item.year || '',
                }));
            }

            const card = el('div', {
                className: 'group cursor-pointer',
                'data-item-id': item.id || '',
                'data-item-type': item.type || '',
            },
                el('div', { className: CSS.CARD }, figure, info)
            );

            this.els.grid.appendChild(card);
        }
    }

    showLoadMore(show) {
        this.els.loadMore.classList.toggle('hidden', !show);
    }

    renderModalHeader(content, title, poster, subtitleText) {
        const header = el('div', { className: 'flex gap-4 mb-4' });

        if (poster) {
            header.appendChild(el('img', {
                src: poster,
                alt: title || '',
                className: 'w-20 h-28 object-cover rounded-lg flex-shrink-0',
            }));
        }

        header.appendChild(
            el('div', { className: 'flex flex-col justify-center min-w-0' },
                el('h3', { className: 'font-bold text-lg line-clamp-2', textContent: title || 'Unknown' }),
                el('p', { className: 'text-sm text-w-muted mt-1', textContent: subtitleText || '' })
            )
        );

        content.appendChild(header);
    }

    showEpisodePicker(title, poster, meta, onEpisodeSelect) {
        const content = this.els.streamContent;
        content.innerHTML = '';

        this.renderModalHeader(content, title, poster, 'Select an episode');

        const videos = meta?.videos || [];
        if (!videos.length) {
            content.appendChild(el('p', {
                className: 'text-w-muted text-sm text-center py-6',
                textContent: 'No episodes found.',
            }));
            this.els.streamModal.showModal();
            return;
        }

        // Group by season
        const seasons = {};
        for (const v of videos) {
            const s = v.season != null ? v.season : 0;
            if (!seasons[s]) seasons[s] = [];
            seasons[s].push(v);
        }
        const seasonNums = Object.keys(seasons).map(Number).sort((a, b) => {
            if (a === 0) return 1;
            if (b === 0) return -1;
            return a - b;
        });

        const episodeContainer = el('div', { className: 'max-h-[350px] overflow-y-auto' });

        const renderEpisodes = (seasonNum) => {
            episodeContainer.innerHTML = '';
            const eps = (seasons[seasonNum] || []).slice().sort((a, b) => (a.episode || 0) - (b.episode || 0));
            const list = el('div', { className: 'flex flex-col gap-1.5' });

            for (const episode of eps) {
                const epInfo = el('div', { className: 'min-w-0 flex-1' },
                    el('div', {
                        className: 'text-sm font-medium line-clamp-1',
                        textContent: episode.title || episode.name || `Episode ${episode.episode || '?'}`,
                    })
                );

                if (episode.released || episode.overview) {
                    epInfo.appendChild(el('div', {
                        className: 'text-xs text-w-muted line-clamp-1',
                        textContent: episode.released
                            ? new Date(episode.released).toLocaleDateString()
                            : (episode.overview || ''),
                    }));
                }

                const row = el('button', {
                    className: CSS.EPISODE_ROW,
                    onClick: () => onEpisodeSelect(episode),
                },
                    el('span', {
                        className: 'flex-shrink-0 w-8 h-8 rounded-full bg-w-cyan/10 flex items-center justify-center text-xs font-bold text-w-cyan',
                        textContent: episode.episode != null ? String(episode.episode) : '?',
                    }),
                    epInfo,
                    el('span', { className: 'text-w-muted flex-shrink-0', innerHTML: ARROW_ICON_SVG })
                );
                list.appendChild(row);
            }
            episodeContainer.appendChild(list);
        };

        // Season tabs
        if (seasonNums.length > 1) {
            const tabsRow = el('div', { className: 'flex gap-1.5 mb-3 flex-wrap' });
            for (let idx = 0; idx < seasonNums.length; idx++) {
                const sn = seasonNums[idx];
                const btn = el('button', {
                    className: idx === 0 ? CSS.CHIP_ACTIVE : CSS.CHIP_INACTIVE,
                    textContent: sn === 0 ? 'Specials' : `S${sn}`,
                    onClick: () => {
                        tabsRow.querySelectorAll('button').forEach(b => { b.className = CSS.CHIP_INACTIVE; });
                        btn.className = CSS.CHIP_ACTIVE;
                        renderEpisodes(sn);
                    },
                });
                tabsRow.appendChild(btn);
            }
            content.appendChild(tabsRow);
        }

        content.appendChild(episodeContainer);
        renderEpisodes(seasonNums[0]);
        this.els.streamModal.showModal();
    }

    showStreamModal(title, poster, streams, subtitle) {
        const content = this.els.streamContent;
        content.innerHTML = '';

        const parsed = streams.map(s => parseStreamName(s.name));
        const subText = subtitle || `${streams.length} stream${streams.length !== 1 ? 's' : ''} found`;
        this.renderModalHeader(content, title, poster, subText);
        const subtitleEl = content.querySelector('.text-sm.text-w-muted');

        if (streams.length === 0) {
            content.appendChild(el('p', {
                className: 'text-w-muted text-sm text-center py-6',
                textContent: subtitle === 'Loading streams...' ? subtitle : 'No streams available for this title.',
            }));
            this.els.streamModal.showModal();
            return;
        }

        const { filterBar, applyFilter } = this._buildStreamFilters(streams, parsed, subtitleEl, subtitle);
        if (filterBar) content.appendChild(filterBar);

        const { list, emptyFilter } = this._buildStreamList(streams, parsed, applyFilter);
        content.appendChild(list);
        content.appendChild(emptyFilter);

        this.els.streamModal.showModal();
    }

    _buildStreamFilters(streams, parsed, subtitleEl, subtitle) {
        // Collect unique sources
        const allSources = [];
        const allLabels = [];
        const seenLabelsLower = {};
        for (const info of parsed) {
            if (!allSources.includes(info.source)) allSources.push(info.source);
            for (const lbl of info.labels) {
                const lower = lbl.toLowerCase();
                if (!seenLabelsLower[lower]) {
                    seenLabelsLower[lower] = true;
                    allLabels.push(lbl);
                }
            }
        }

        // Collect unique languages
        const allLangs = [];
        const seenLangs = {};
        this._streamLangs = [];
        for (const stream of streams) {
            const langs = extractLanguages(stream.title || '');
            this._streamLangs.push(langs.map(l => l.name));
            for (const lang of langs) {
                if (!seenLangs[lang.name]) {
                    seenLangs[lang.name] = true;
                    allLangs.push(lang);
                }
            }
        }

        if (allSources.length <= 1 && allLabels.length === 0 && allLangs.length <= 1) {
            return { filterBar: null, applyFilter: () => {} };
        }

        // Filter state
        const activeSources = {};
        const activeLabels = {};
        let activeLang = null;
        const langChips = [];

        const applyFilter = () => {
            const activeSrcKeys = Object.keys(activeSources);
            const activeLblKeys = Object.keys(activeLabels);
            let visibleCount = 0;

            for (const se of this._streamEls) {
                let show = true;
                if (activeSrcKeys.length > 0 && !activeSources[se.parsed.source]) {
                    show = false;
                }
                if (show && activeLblKeys.length > 0) {
                    const streamLabelsLower = se.parsed.labels.map(l => l.toLowerCase());
                    if (!activeLblKeys.every(k => streamLabelsLower.includes(k.toLowerCase()))) {
                        show = false;
                    }
                }
                if (show && activeLang && !se.langs.includes(activeLang)) {
                    show = false;
                }
                se.el.style.display = show ? '' : 'none';
                if (show) visibleCount++;
            }

            const hasFilters = activeSrcKeys.length > 0 || activeLblKeys.length > 0 || activeLang;
            this._emptyFilter?.classList.toggle('hidden', !(hasFilters && visibleCount === 0));

            if (subtitleEl && !subtitle) {
                subtitleEl.textContent = hasFilters
                    ? `${visibleCount} of ${streams.length} stream${streams.length !== 1 ? 's' : ''}`
                    : `${streams.length} stream${streams.length !== 1 ? 's' : ''} found`;
            }
        };

        const filterBar = el('div', { className: 'flex flex-wrap gap-1.5 mb-3' });

        const createChip = (text, isSource) => {
            const store = isSource ? activeSources : activeLabels;
            const chip = el('button', {
                className: CSS.CHIP_INACTIVE,
                textContent: text,
                'data-filter': text,
                'data-filter-type': isSource ? 'source' : 'label',
                onClick: () => {
                    if (store[text]) {
                        delete store[text];
                        chip.className = CSS.CHIP_INACTIVE;
                    } else {
                        store[text] = true;
                        chip.className = CSS.CHIP_ACTIVE;
                    }
                    applyFilter();
                },
            });
            return chip;
        };

        const createLangChip = (lang) => {
            const chip = el('button', {
                className: CSS.CHIP_INACTIVE,
                textContent: `${lang.flag} ${lang.name}`,
                'data-lang': lang.name,
                onClick: () => {
                    if (activeLang === lang.name) {
                        activeLang = null;
                        chip.className = CSS.CHIP_INACTIVE;
                    } else {
                        for (const lc of langChips) lc.className = CSS.CHIP_INACTIVE;
                        activeLang = lang.name;
                        chip.className = CSS.CHIP_ACTIVE;
                    }
                    applyFilter();
                },
            });
            langChips.push(chip);
            return chip;
        };

        for (const src of allSources) filterBar.appendChild(createChip(src, true));
        for (const lbl of allLabels) filterBar.appendChild(createChip(lbl, false));
        for (const lang of allLangs) filterBar.appendChild(createLangChip(lang));

        return { filterBar, applyFilter };
    }

    _buildStreamList(streams, parsed, applyFilter) {
        const list = el('div', { className: 'flex flex-col gap-2 max-h-[400px] overflow-y-auto' });
        this._streamEls = [];

        for (let i = 0; i < streams.length; i++) {
            const stream = streams[i];
            const info = parsed[i];
            const infoHash = extractInfoHash(stream);

            let row;
            if (infoHash) {
                row = el('a', {
                    href: `/${infoHash}`,
                    'data-async-target': 'main',
                    className: CSS.STREAM_ROW,
                    onClick: () => this.els.streamModal.close(),
                });
            } else {
                row = el('div', { className: `opacity-50 ${CSS.STREAM_ROW}` });
            }

            // Play icon
            row.appendChild(el('div', {
                className: 'flex-shrink-0 w-8 h-8 rounded-full bg-w-cyan/10 flex items-center justify-center',
                innerHTML: PLAY_ICON_SVG,
            }));

            // Stream info
            const nameRow = el('div', { className: 'flex items-center gap-1.5 flex-wrap' },
                el('span', { className: 'text-sm font-medium', textContent: info.source })
            );
            for (const label of info.labels) {
                nameRow.appendChild(el('span', { className: CSS.LABEL_BADGE, textContent: label }));
            }

            const streamInfo = el('div', { className: 'min-w-0 flex-1' }, nameRow);

            if (stream.title) {
                for (const line of stream.title.split('\n')) {
                    if (!line) continue;
                    streamInfo.appendChild(el('div', {
                        className: 'text-xs text-w-sub line-clamp-1',
                        textContent: line,
                    }));
                }
            }

            row.appendChild(streamInfo);

            if (!infoHash) {
                row.appendChild(el('span', {
                    className: 'text-xs text-w-muted flex-shrink-0',
                    textContent: 'No torrent',
                }));
            }

            this._streamEls.push({ el: row, parsed: info, langs: this._streamLangs?.[i] || [] });
            list.appendChild(row);
        }

        const emptyFilter = el('p', {
            className: 'text-w-muted text-sm text-center py-6 hidden',
            textContent: 'No streams match the selected filters.',
        });
        this._emptyFilter = emptyFilter;

        return { list, emptyFilter };
    }
}
