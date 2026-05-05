import { useMemo, useCallback } from 'preact/hooks';
import { getSearchResultsForType } from './discoverReducer';
import { chipClass } from './discoverUtils';
import { t } from '../i18n';

function typeLabel(type) {
    const key = `discover.type.${type.toLowerCase()}`;
    const translated = t(key);
    if (translated !== key) return translated;
    return type.charAt(0).toUpperCase() + type.slice(1);
}

export function TypeTabs({ types, selectedType, onSelect }) {
    if (!types.length) return null;
    return (
        <div class="flex gap-1.5 sm:gap-2 flex-wrap">
            {types.map(type => (
                <button
                    key={type}
                    class={chipClass(type === selectedType)}
                    onClick={() => onSelect(type)}
                >
                    {typeLabel(type)}
                </button>
            ))}
        </div>
    );
}

export function SearchTabs({ searchResults, searchTypes, searchType, onSelect }) {
    const tabs = useMemo(() => [
        { key: 'all', label: t('discover.allTab'), count: searchResults.length },
        ...searchTypes.map(st => ({
            key: st,
            label: typeLabel(st),
            count: getSearchResultsForType(searchResults, st).length,
        })),
    ], [searchResults, searchTypes]);

    return (
        <div class="flex gap-1.5 sm:gap-2 flex-wrap">
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
        <div class="mt-2">
            <select
                class="select select-sm bg-w-surface border-w-line text-w-text w-full sm:w-auto"
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
