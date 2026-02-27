import { useState, useEffect, useMemo, useCallback, useRef } from 'preact/hooks';

const OFFICIAL_API = 'https://api.strem.io/addonscollection.json';
const COMMUNITY_API = 'https://stremio-addons.net/api/addon_catalog/all/stremio-addons.net.json';
const GITHUB_ISSUES_API = 'https://api.github.com/repos/Stremio-Community/stremio-addons-list/issues';

const SOURCES = {
    official: { label: 'Official Stremio', desc: 'Curated addons from the Stremio team', api: OFFICIAL_API },
    community: { label: 'Community', desc: 'Community-curated addons from stremio-addons.net', api: COMMUNITY_API },
};

const TYPE_LABELS = { movie: 'Movie', series: 'Series', channel: 'Channel', tv: 'TV', anime: 'Anime', other: 'Other' };

async function fetchAddonCatalog(sourceKey) {
    const src = SOURCES[sourceKey];
    const res = await fetch(src.api);
    if (!res.ok) throw new Error(`Failed to fetch ${src.label}`);
    const data = await res.json();
    let addons;
    if (Array.isArray(data)) {
        addons = data;
    } else if (data.addons) {
        addons = data.addons;
    } else {
        throw new Error(`Unexpected format from ${src.label}`);
    }
    return addons
        .filter(a => a.transportUrl && a.manifest)
        .map(a => ({
            transportUrl: a.transportUrl,
            manifest: a.manifest,
            source: sourceKey,
        }));
}

// Fetch community votes from GitHub Issues (sorted by thumbs-up).
// Each issue body starts with "### Addon Manifest URL\n\n<url>".
// Returns { byName: Map<normalizedName, {votes, url}>, byUrl: Map<manifestUrl, votes> }
function normalizeName(name) {
    return (name || '').toLowerCase().replace(/\s*\|.*$/, '').trim();
}

async function fetchPopularityData() {
    const byName = new Map();
    const byUrl = new Map();
    try {
        const pages = await Promise.allSettled([1, 2, 3].map(page =>
            fetch(`${GITHUB_ISSUES_API}?sort=reactions-%2B1&direction=desc&per_page=100&page=${page}&state=open`)
                .then(r => r.ok ? r.json() : [])
        ));
        for (const p of pages) {
            if (p.status !== 'fulfilled') continue;
            for (const issue of p.value) {
                const ups = issue.reactions?.['+1'] || 0;
                if (ups === 0) continue;
                const match = issue.body?.match(/### Addon Manifest URL\s+(\S+)/);
                if (!match) continue;
                const url = match[1].replace(/[?#].*$/, '').trim();
                const name = normalizeName(issue.title);
                byUrl.set(url, ups);
                // Keep highest vote count per normalized name
                if (!byName.has(name) || byName.get(name).votes < ups) {
                    byName.set(name, { votes: ups, url, title: issue.title, labels: issue.labels?.map(l => l.name) || [] });
                }
            }
        }
    } catch (e) {
        // Non-critical — fallback to catalog order
    }
    return { byName, byUrl };
}

function hasStreams(addon) {
    const resources = addon.manifest.resources || [];
    return resources.some(r =>
        (typeof r === 'string' && r === 'stream') ||
        (r && r.name === 'stream')
    );
}

const TORRENT_KEYWORDS = /torrent|debrid|real.?debrid|premiumize|all.?debrid|easydebrid|offcloud|put\.io|magnet|p2p|jackettio|mediafusion|comet|stremio.?jackett|annatar|orion/i;

function isTorrentAddon(addon) {
    const m = addon.manifest;
    const text = `${m.id || ''} ${m.name || ''} ${m.description || ''}`;
    return TORRENT_KEYWORDS.test(text);
}

function isAdult(addon) {
    return addon.manifest.behaviorHints?.adult === true;
}

// Normalize manifest ID: strip random instance suffixes (e.g. "stremio.comet.fast.twaD" → "stremio.comet.fast")
function normalizeId(id) {
    // Strip trailing dot + 3-5 char random suffix (e.g. ".twaD", ".VUwl", ".aKyc", ".iofD")
    return (id || '').replace(/\.[A-Za-z0-9]{3,5}$/, '');
}

function dedup(addons) {
    const seenById = new Map();
    const seenByName = new Map();
    const result = [];
    for (const a of addons) {
        const id = a.manifest.id;
        if (!id) continue;
        const normId = normalizeId(id);
        const name = normalizeName(a.manifest.name);

        // Check both normalized ID and normalized name to catch duplicates
        const existingById = seenById.get(normId);
        const existingByName = name ? seenByName.get(name) : null;
        const existing = existingById || existingByName;

        if (!existing) {
            seenById.set(normId, a);
            if (name) seenByName.set(name, a);
            result.push(a);
        } else {
            // Replace if this one is from official source or has more votes
            const dominated = (a.source === 'official' && existing.source !== 'official')
                || ((a.votes || 0) > (existing.votes || 0));
            if (dominated) {
                const idx = result.indexOf(existing);
                if (idx !== -1) result[idx] = a;
                seenById.set(normId, a);
                if (name) seenByName.set(name, a);
            }
        }
    }
    return result;
}

export function AddonWizard({ onComplete, onSkip }) {
    const dialogRef = useRef(null);
    const [step, setStep] = useState(1);
    const [sources, setSources] = useState({ official: false, community: false });
    const [addons, setAddons] = useState([]);
    const [selected, setSelected] = useState(new Set());
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState(null);
    const [search, setSearch] = useState('');
    const [showAdult, setShowAdult] = useState(false);
    const [torrentOnly, setTorrentOnly] = useState(true);
    useEffect(() => {
        const dialog = dialogRef.current;
        if (dialog && !dialog.open) dialog.showModal();
        window.umami?.track('discover-wizard-opened');
    }, []);

    const anySources = sources.official || sources.community;
    const allSources = sources.official && sources.community;

    const toggleSource = useCallback((key) => {
        setSources(prev => ({ ...prev, [key]: !prev[key] }));
    }, []);

    const toggleAllSources = useCallback(() => {
        const next = !allSources;
        setSources({ official: next, community: next });
    }, [allSources]);

    // Step 2: fetch addons + popularity in parallel
    const goToStep2 = useCallback(async () => {
        setStep(2);
        setLoading(true);
        setError(null);
        setAddons([]);
        setSelected(new Set());
        setSearch('');

        const keys = Object.keys(SOURCES).filter(k => sources[k]);
        // Fetch catalogs + popularity in parallel
        const [catalogResults, popularity] = await Promise.all([
            Promise.allSettled(keys.map(k => fetchAddonCatalog(k))),
            fetchPopularityData(),
        ]);

        const merged = [];
        const errors = [];
        catalogResults.forEach((r, i) => {
            if (r.status === 'fulfilled') {
                merged.push(...r.value);
            } else {
                errors.push(`${SOURCES[keys[i]].label}: ${r.reason.message}`);
            }
        });

        const unique = dedup(merged).filter(hasStreams);

        // Attach vote counts — match by URL first, then by normalized name
        const matchedNames = new Set();
        for (const a of unique) {
            // Try exact URL match
            a.votes = popularity.byUrl.get(a.transportUrl) || 0;
            if (a.votes === 0) {
                const base = a.transportUrl.replace(/\/manifest\.json$/, '');
                for (const [url, v] of popularity.byUrl) {
                    if (url.replace(/\/manifest\.json$/, '') === base) {
                        a.votes = v;
                        break;
                    }
                }
            }
            // Try name match
            if (a.votes === 0) {
                const name = normalizeName(a.manifest.name);
                const entry = popularity.byName.get(name);
                if (entry) {
                    a.votes = entry.votes;
                    matchedNames.add(name);
                }
            } else {
                matchedNames.add(normalizeName(a.manifest.name));
            }
        }

        // Inject popular addons missing from catalogs (e.g. Torrentio — configurable, not in catalogs)
        const existingIds = new Set(unique.map(a => a.manifest.id));
        for (const [name, entry] of popularity.byName) {
            if (matchedNames.has(name)) continue;
            if (entry.votes < 10) continue; // only inject actually popular ones
            // Check if it's a stream addon by labels
            const isTorrentLabel = entry.labels.some(l => ['torrents', 'debrid support'].includes(l));
            if (!isTorrentLabel && !TORRENT_KEYWORDS.test(entry.title + ' ' + (entry.labels.join(' ')))) continue;
            // Fetch manifest to get addon details
            try {
                const manifestUrl = entry.url.endsWith('/manifest.json') ? entry.url : entry.url + '/manifest.json';
                const res = await fetch(manifestUrl);
                if (!res.ok) continue;
                const manifest = await res.json();
                if (!manifest.id || existingIds.has(manifest.id)) continue;
                if (!hasStreams({ manifest })) continue;
                existingIds.add(manifest.id);
                unique.push({
                    transportUrl: manifestUrl,
                    manifest,
                    source: 'community',
                    votes: entry.votes,
                });
            } catch (e) {
                // Skip unavailable addons
            }
        }

        // Sort: torrent first, then by votes desc
        unique.sort((a, b) => {
            const at = isTorrentAddon(a) ? 0 : 1;
            const bt = isTorrentAddon(b) ? 0 : 1;
            if (at !== bt) return at - bt;
            return b.votes - a.votes;
        });
        setAddons(unique);
        setLoading(false);
        if (errors.length > 0) {
            setError(errors.join('; '));
        }
    }, [sources]);

    // Filter addons for display
    const filteredAddons = useMemo(() => {
        let list = addons;
        if (torrentOnly) list = list.filter(isTorrentAddon);
        if (!showAdult) list = list.filter(a => !isAdult(a));
        if (search.trim()) {
            const q = search.trim().toLowerCase();
            list = list.filter(a =>
                (a.manifest.name || '').toLowerCase().includes(q) ||
                (a.manifest.description || '').toLowerCase().includes(q)
            );
        }
        return list;
    }, [addons, torrentOnly, showAdult, search]);

    const toggleAddon = useCallback((url) => {
        setSelected(prev => {
            const next = new Set(prev);
            if (next.has(url)) next.delete(url);
            else next.add(url);
            return next;
        });
    }, []);

    const handleAdultToggle = useCallback(() => {
        if (!showAdult) {
            if (window.confirm('Are you 18 or older?')) {
                setShowAdult(true);
            }
        } else {
            setShowAdult(false);
            // Remove any selected adult addons
            setSelected(prev => {
                const next = new Set(prev);
                for (const a of addons) {
                    if (isAdult(a)) next.delete(a.transportUrl);
                }
                return next;
            });
        }
    }, [showAdult, addons]);

    const install = useCallback(async () => {
        const urls = [...selected];
        if (urls.length === 0) return;
        setSaving(true);
        setError(null);
        try {
            const res = await fetch('/stremio/addon-url/batch-add', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-TOKEN': window._CSRF,
                },
                body: JSON.stringify({ urls }),
            });
            if (!res.ok) throw new Error('Failed to save addons');
            const data = await res.json();
            window.umami?.track('discover-wizard-installed', { count: urls.length });
            onComplete(urls, data);
        } catch (e) {
            setError(e.message);
            setSaving(false);
        }
    }, [selected, onComplete]);

    // Prevent backdrop click from closing
    const handleDialogClick = useCallback((e) => {
        // Only close via explicit skip button
        e.preventDefault();
    }, []);

    const adultCount = useMemo(() => addons.filter(a => isAdult(a)).length, [addons]);

    return (
        <dialog ref={dialogRef} class="modal" onClose={handleDialogClick}>
            <div class="modal-box bg-w-card border border-w-line/50 rounded-2xl max-w-2xl">
                {step === 1 ? (
                    <Step1
                        sources={sources}
                        onToggle={toggleSource}
                        onToggleAll={toggleAllSources}
                        allSources={allSources}
                        anySources={anySources}
                        onNext={goToStep2}
                        onSkip={onSkip}
                    />
                ) : (
                    <Step2
                        addons={filteredAddons}
                        selected={selected}
                        loading={loading}
                        saving={saving}
                        error={error}
                        search={search}
                        onSearchChange={setSearch}
                        onToggle={toggleAddon}
                        showAdult={showAdult}
                        adultCount={adultCount}
                        onAdultToggle={handleAdultToggle}
                        torrentOnly={torrentOnly}
                        onTorrentOnlyToggle={() => setTorrentOnly(p => !p)}
                        onInstall={install}
                        onBack={() => setStep(1)}
                        onSkip={onSkip}
                    />
                )}
            </div>
        </dialog>
    );
}

function Step1({ sources, onToggle, onToggleAll, allSources, anySources, onNext, onSkip }) {
    return (
        <div>
            <button
                class="btn btn-sm btn-circle btn-ghost absolute right-2 top-2 text-w-muted hover:text-base-content"
                onClick={onSkip}
            >&#10005;</button>
            <h3 class="text-lg font-bold mb-1">Set up your addons</h3>
            <p class="text-sm text-w-sub mb-5">Choose addon sources to browse</p>

            <label class="flex items-center gap-2 mb-3 cursor-pointer text-sm text-w-sub">
                <input
                    type="checkbox"
                    class="checkbox checkbox-sm rounded-sm border-w-line hover:border-w-cyan/40 checked:border-w-cyan/50 checked:bg-w-cyan/20 [--chkfg:theme(colors.w-cyan)]"
                    checked={allSources}
                    onChange={onToggleAll}
                />
                Select all
            </label>

            <div class="flex flex-col gap-3 mb-6">
                <SourceCard
                    label={SOURCES.official.label}
                    desc={SOURCES.official.desc}
                    checked={sources.official}
                    onChange={() => onToggle('official')}
                />
                <SourceCard
                    label={SOURCES.community.label}
                    desc={SOURCES.community.desc}
                    checked={sources.community}
                    onChange={() => onToggle('community')}
                />
            </div>

            <div class="flex justify-end">
                <button class="btn btn-ghost btn-sm border border-w-line hover:border-w-cyan/30 hover:text-w-cyan px-6" disabled={!anySources} onClick={onNext}>Next</button>
            </div>

            <Disclaimer />
        </div>
    );
}

function SourceCard({ label, desc, checked, onChange }) {
    return (
        <label class={`flex items-center gap-3 p-4 rounded-xl border cursor-pointer transition-all ${checked ? 'border-w-cyan/40 bg-w-cyan/5' : 'border-w-line hover:border-w-cyan/20'}`}>
            <input
                type="checkbox"
                class="checkbox checkbox-sm rounded-sm border-w-line hover:border-w-cyan/40 checked:border-w-cyan/50 checked:bg-w-cyan/20 [--chkfg:theme(colors.w-cyan)]"
                checked={checked}
                onChange={onChange}
            />
            <div class="min-w-0 flex-1">
                <div class="font-medium text-sm">{label}</div>
                <div class="text-xs text-w-sub">{desc}</div>
            </div>
        </label>
    );
}

function Step2({ addons, selected, loading, saving, error, search, onSearchChange, onToggle, showAdult, adultCount, onAdultToggle, torrentOnly, onTorrentOnlyToggle, onInstall, onBack, onSkip }) {
    const selectedCount = selected.size;

    return (
        <div>
            <button
                class="btn btn-sm btn-ghost absolute left-2 top-2 text-w-muted hover:text-w-cyan gap-1 px-2"
                onClick={onBack}
            >
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M15 18l-6-6 6-6"/>
                </svg>
                Sources
            </button>
            <button
                class="btn btn-sm btn-circle btn-ghost absolute right-2 top-2 text-w-muted hover:text-base-content"
                onClick={onSkip}
            >&#10005;</button>
            <div class="pt-8">

            {/* Search */}
            <div class="relative mb-3">
                <svg class="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-w-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <circle cx="11" cy="11" r="8" />
                    <path stroke-linecap="round" d="m21 21-4.3-4.3" />
                </svg>
                <input
                    type="text"
                    class="input input-sm input-bordered w-full pl-9 bg-w-surface border-w-line focus:border-w-cyan focus:outline-none"
                    placeholder="Search addons..."
                    value={search}
                    onInput={(e) => onSearchChange(e.target.value)}
                />
            </div>

            {/* Controls row */}
            <div class="flex items-center justify-between mb-3">
                <div class="flex items-center gap-3">
                    <label class="flex items-center gap-2 cursor-pointer text-sm text-w-sub">
                        <input
                            type="checkbox"
                            class="checkbox checkbox-xs rounded-sm border-w-line hover:border-w-cyan/40 checked:border-w-cyan/50 checked:bg-w-cyan/20 [--chkfg:theme(colors.w-cyan)]"
                            checked={torrentOnly}
                            onChange={onTorrentOnlyToggle}
                        />
                        Torrent only
                    </label>
                    {adultCount > 0 && (
                        <label class="flex items-center gap-2 cursor-pointer text-sm text-w-muted">
                            <input
                                type="checkbox"
                                class="checkbox checkbox-xs rounded-sm hover:border-w-cyan/40"
                                checked={showAdult}
                                onChange={onAdultToggle}
                            />
                            18+ ({adultCount})
                        </label>
                    )}
                </div>
                <span class="text-xs text-w-muted">{selectedCount} of {addons.length} selected</span>
            </div>

            {error && <p class="text-xs text-error mb-2">{error}</p>}

            {loading ? (
                <div class="text-center py-12">
                    <span class="loading loading-spinner loading-md text-w-cyan"></span>
                    <p class="text-w-sub text-sm mt-3">Loading addon catalogs...</p>
                </div>
            ) : (
                <div class="flex flex-col gap-1.5 max-h-[350px] overflow-y-auto mb-4">
                    {addons.length === 0 && !loading && (
                        <p class="text-w-muted text-sm text-center py-8">No addons found.</p>
                    )}
                    {addons.map(addon => (
                        <AddonRow
                            key={addon.transportUrl}
                            addon={addon}
                            checked={selected.has(addon.transportUrl)}
                            onToggle={() => onToggle(addon.transportUrl)}
                        />
                    ))}
                </div>
            )}

            <div class="flex justify-end">
                <button
                    class="btn btn-ghost btn-sm border border-w-line hover:border-w-cyan/30 hover:text-w-cyan px-6"
                    disabled={selectedCount === 0 || saving}
                    onClick={onInstall}
                >
                    {saving ? (
                        <span class="loading loading-spinner loading-xs"></span>
                    ) : (
                        `Install (${selectedCount})`
                    )}
                </button>
            </div>

            <Disclaimer />
            </div>
        </div>
    );
}

function Disclaimer() {
    return (
        <p class="text-[10px] text-w-muted/60 text-center mt-4 leading-relaxed">
            All addon sources and addons listed here are third-party services not affiliated with Webtor.
        </p>
    );
}

function AddonRow({ addon, checked, onToggle }) {
    const [imgError, setImgError] = useState(false);
    const m = addon.manifest;
    const types = (m.types || []).slice(0, 4);
    const sourceLabel = addon.source === 'official' ? 'Official' : 'Community';

    return (
        <label class={`flex items-center gap-3 p-2.5 rounded-lg border cursor-pointer transition-all ${checked ? 'border-w-cyan/30 bg-w-cyan/5' : 'border-w-line hover:border-w-cyan/20'}`}>
            <input
                type="checkbox"
                class="checkbox checkbox-sm rounded-sm border-w-line hover:border-w-cyan/40 checked:border-w-cyan/50 checked:bg-w-cyan/20 [--chkfg:theme(colors.w-cyan)] flex-shrink-0"
                checked={checked}
                onChange={onToggle}
            />
            {m.logo && !imgError ? (
                <img
                    src={m.logo}
                    alt=""
                    class="w-8 h-8 rounded object-contain flex-shrink-0"
                    onError={() => setImgError(true)}
                />
            ) : (
                <div class="w-8 h-8 rounded flex-shrink-0 bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 flex items-center justify-center">
                    <span class="text-[10px] font-bold text-w-purpleL/60">{(m.name || '?')[0]}</span>
                </div>
            )}
            <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1.5">
                    <span class="text-sm font-medium line-clamp-1">{m.name || 'Unknown'}</span>
                    <span class={`text-[10px] px-1.5 py-0.5 rounded font-medium flex-shrink-0 ${addon.source === 'official' ? 'bg-w-cyan/10 text-w-cyan' : 'bg-w-purple/10 text-w-purpleL'}`}>
                        {sourceLabel}
                    </span>
                    {addon.votes > 0 && (
                        <span class="text-[10px] text-w-muted flex-shrink-0" title="Community votes">
                            👍 {addon.votes >= 1000 ? `${(addon.votes / 1000).toFixed(1)}k` : addon.votes}
                        </span>
                    )}
                </div>
                <div class="text-xs text-w-sub line-clamp-1">{m.description || ''}</div>
                {types.length > 0 && (
                    <div class="flex gap-1 mt-0.5">
                        {types.map(t => (
                            <span key={t} class="text-[9px] px-1 py-px rounded bg-w-surface text-w-muted">{TYPE_LABELS[t] || t}</span>
                        ))}
                    </div>
                )}
            </div>
        </label>
    );
}
