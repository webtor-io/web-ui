// Discover page state reducer

export const TYPE_PRIORITY = ['movie', 'series'];

export function sortByPriority(types, priority) {
    return [...types].sort((a, b) => {
        const ai = priority.indexOf(a);
        const bi = priority.indexOf(b);
        if (ai !== -1 && bi !== -1) return ai - bi;
        if (ai !== -1) return -1;
        if (bi !== -1) return 1;
        return a.localeCompare(b);
    });
}

export function buildCatalogs(manifests) {
    const catalogs = [];
    for (const m of manifests) {
        for (const cat of (m.manifest.catalogs || [])) {
            catalogs.push({
                id: cat.id,
                type: cat.type,
                name: cat.name || cat.id,
                addonName: m.manifest.name || 'Unknown',
                baseUrl: m.baseUrl,
            });
        }
    }
    return catalogs;
}

export function getCatalogsForType(catalogs, type) {
    return catalogs.filter(c => c.type === type);
}

export function getTypes(catalogs) {
    const types = [...new Set(catalogs.map(c => c.type))];
    return sortByPriority(types, TYPE_PRIORITY);
}

export function getSearchTypes(searchResults) {
    const types = [...new Set(searchResults.map(r => r.type))];
    return sortByPriority(types, TYPE_PRIORITY);
}

export function getSearchResultsForType(searchResults, type) {
    return searchResults.filter(item => item.type === type);
}

export const initialState = {
    phase: 'loading', // 'loading' | 'ready' | 'error' | 'no-addons' | 'no-catalogs'
    errorMessage: '',
    manifests: [],
    catalogs: [],
    selectedType: null,
    selectedCatalog: null,
    items: [],
    skip: 0,
    hasMore: true,
    catalogLoading: false,
    // Search
    isSearchMode: false,
    searchQuery: '',
    searchResults: [],
    searchType: 'all',
    searchLoading: false,
    // Modal
    modal: null, // { view: 'loading'|'streams'|'episodes', title, poster, ... }
};

export function discoverReducer(state, action) {
    switch (action.type) {
        case 'INIT_SUCCESS': {
            const { manifests, catalogs, selectedType, selectedCatalog } = action;
            return { ...state, phase: 'ready', manifests, catalogs, selectedType, selectedCatalog };
        }
        case 'INIT_ERROR':
            return { ...state, phase: 'error', errorMessage: action.message };
        case 'SET_PHASE':
            return { ...state, phase: action.phase, errorMessage: action.message || '' };
        case 'SELECT_TYPE': {
            const selectedCatalog = getCatalogsForType(state.catalogs, action.selectedType)[0] || null;
            return { ...state, selectedType: action.selectedType, selectedCatalog, items: [], skip: 0, hasMore: true };
        }
        case 'SELECT_CATALOG':
            return { ...state, selectedCatalog: action.catalog, items: [], skip: 0, hasMore: true };
        case 'CATALOG_LOADING':
            return { ...state, catalogLoading: true };
        case 'CATALOG_LOADED': {
            const newItems = action.append ? [...state.items, ...action.items] : action.items;
            return { ...state, catalogLoading: false, items: newItems, hasMore: action.hasMore, skip: newItems.length };
        }
        case 'CATALOG_ERROR':
            return state.items.length > 0
                ? { ...state, catalogLoading: false }
                : { ...state, catalogLoading: false, phase: 'error', errorMessage: action.message };
        case 'SEARCH_START':
            return { ...state, isSearchMode: true, searchQuery: action.query, searchResults: [], searchLoading: true, searchType: 'all' };
        case 'SEARCH_RESULTS':
            return { ...state, searchLoading: false, searchResults: action.results };
        case 'SELECT_SEARCH_TYPE':
            return { ...state, searchType: action.searchType };
        case 'EXIT_SEARCH': {
            const types = getTypes(state.catalogs);
            const selectedType = types[0] || null;
            const selectedCatalog = selectedType ? getCatalogsForType(state.catalogs, selectedType)[0] : null;
            return {
                ...state,
                isSearchMode: false, searchQuery: '', searchResults: [], searchType: 'all', searchLoading: false,
                selectedType, selectedCatalog, items: [], skip: 0, hasMore: true,
            };
        }
        case 'SHOW_MODAL':
            return { ...state, modal: action.modal };
        case 'CLOSE_MODAL':
            return { ...state, modal: null };
        default:
            return state;
    }
}
