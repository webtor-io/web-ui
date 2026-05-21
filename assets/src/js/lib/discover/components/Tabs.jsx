import { useMemo, useCallback } from 'preact/hooks';
import { getSearchResultsForType } from './discoverReducer';
import { chipClass } from './discoverUtils';
import { t, tf } from '../i18n';

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
    const handleChange = useCallback((e) => {
        const parts = e.target.value.split('::');
        const match = catalogs.find(c => c.baseUrl === parts[0] && c.id === parts[1] && !c.disabled);
        if (match) onSelect(match);
    }, [catalogs, onSelect]);

    // Group by addon so disabled (unreachable) addons can be visually
    // separated from live ones. The selector renders one <optgroup> per
    // addon when there's more than one addon represented; for a single
    // addon we keep the flat list to match the previous look.
    const groups = useMemo(() => {
        const m = new Map();
        for (const cat of catalogs) {
            const k = cat.baseUrl;
            if (!m.has(k)) m.set(k, { addonName: cat.addonName, baseUrl: cat.baseUrl, disabled: !!cat.disabled, items: [] });
            m.get(k).items.push(cat);
        }
        return [...m.values()];
    }, [catalogs]);

    // Match the legacy guard: nothing to choose when there's a single
    // catalog. The guard intentionally counts even disabled options so
    // the selector still renders when an addon is down — the user needs
    // to see why their previously-available catalogs are greyed out.
    if (catalogs.length <= 1) return null;

    const useGroups = groups.length > 1;

    return (
        <div>
            <select
                class="select select-sm bg-w-surface border-w-line text-w-text w-full sm:w-auto"
                onChange={handleChange}
                value={selectedCatalog ? `${selectedCatalog.baseUrl}::${selectedCatalog.id}` : ''}
            >
                {useGroups
                    ? groups.map(g => (
                        <optgroup
                            key={g.baseUrl}
                            label={g.disabled
                                ? tf('discover.addonGroupUnavailable', g.addonName)
                                : g.addonName}
                        >
                            {g.items.map(cat => (
                                <option
                                    key={`${cat.baseUrl}::${cat.id}`}
                                    value={`${cat.baseUrl}::${cat.id}`}
                                    disabled={cat.disabled || undefined}
                                >
                                    {cat.disabled ? `⚠ ${cat.name}` : cat.name}
                                </option>
                            ))}
                        </optgroup>
                    ))
                    : catalogs.map(cat => (
                        <option
                            key={`${cat.baseUrl}::${cat.id}`}
                            value={`${cat.baseUrl}::${cat.id}`}
                            disabled={cat.disabled || undefined}
                        >
                            {cat.disabled ? `⚠ ${cat.name} (${cat.addonName})` : `${cat.name} (${cat.addonName})`}
                        </option>
                    ))}
            </select>
        </div>
    );
}
