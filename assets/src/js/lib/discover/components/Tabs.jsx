import { useMemo, useCallback } from 'preact/hooks';
import { getSearchResultsForType } from './discoverReducer';
import { chipClass } from './discoverUtils';

export function TypeTabs({ types, selectedType, onSelect }) {
    if (!types.length) return null;
    return (
        <div class="flex gap-2 mb-4 flex-wrap">
            {types.map(type => (
                <button
                    key={type}
                    class={chipClass(type === selectedType)}
                    onClick={() => onSelect(type)}
                >
                    {type.charAt(0).toUpperCase() + type.slice(1)}
                </button>
            ))}
        </div>
    );
}

export function SearchTabs({ searchResults, searchTypes, searchType, onSelect }) {
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
                    class={chipClass(tab.key === searchType)}
                    onClick={() => onSelect(tab.key)}
                >
                    {tab.label} ({tab.count})
                </button>
            ))}
        </div>
    );
}

export function CatalogSelector({ catalogs, selectedCatalog, onSelect }) {
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
