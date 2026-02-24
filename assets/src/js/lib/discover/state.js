// Discover page state management

function sortByPriority(types, priority) {
    return types.sort((a, b) => {
        const ai = priority.indexOf(a);
        const bi = priority.indexOf(b);
        if (ai !== -1 && bi !== -1) return ai - bi;
        if (ai !== -1) return -1;
        if (bi !== -1) return 1;
        return a.localeCompare(b);
    });
}

const TYPE_PRIORITY = ['movie', 'series'];

export class DiscoverState {
    constructor() {
        this.manifests = [];
        this.catalogs = [];
        this.selectedType = null;
        this.selectedCatalog = null;
        this.items = [];
        this.skip = 0;
        this.hasMore = true;
        this.searchQuery = '';
        this.searchResults = [];
        this.isSearchMode = false;
        this.searchCatalogs = [];
    }

    buildSearchCatalogs(client) {
        this.searchCatalogs = client.getSearchCatalogs();
    }

    getSearchResultsForType(type) {
        return this.searchResults.filter(item => item.type === type);
    }

    getSearchTypes() {
        const types = [...new Set(this.searchResults.map(r => r.type))];
        return sortByPriority(types, TYPE_PRIORITY);
    }

    resetSearch() {
        this.searchQuery = '';
        this.searchResults = [];
        this.isSearchMode = false;
    }

    buildCatalogs() {
        this.catalogs = [];
        for (const m of this.manifests) {
            for (const cat of (m.manifest.catalogs || [])) {
                this.catalogs.push({
                    id: cat.id,
                    type: cat.type,
                    name: cat.name || cat.id,
                    addonName: m.manifest.name || 'Unknown',
                    baseUrl: m.baseUrl,
                });
            }
        }
    }

    getTypes() {
        const types = [...new Set(this.catalogs.map(c => c.type))];
        return sortByPriority(types, TYPE_PRIORITY);
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
