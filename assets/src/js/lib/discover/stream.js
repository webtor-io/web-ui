// Stream name parsing and info hash extraction

const RESOLUTION_MAP = {
    '2160p': '4K',
    '1440p': '2K',
    '4k': '4K',
};

export function parseStreamName(name) {
    if (!name) return { source: 'Unknown', labels: [] };
    const lines = name.split('\n');
    const source = (lines[0] || '').trim() || 'Unknown';
    const labels = [];
    const seenLower = new Set();
    for (let i = 1; i < lines.length; i++) {
        const line = lines[i].trim();
        if (!line) continue;
        const tokens = line.split(/[|\s]+/);
        for (const raw of tokens) {
            const token = raw.trim();
            if (!token) continue;
            const lower = token.toLowerCase();
            const normalized = RESOLUTION_MAP[lower] || token;
            const normLower = normalized.toLowerCase();
            if (!seenLower.has(normLower)) {
                seenLower.add(normLower);
                labels.push(normalized);
            }
        }
    }
    return { source, labels };
}

export function extractFileIdx(stream) {
    if (stream.fileIdx != null && stream.fileIdx !== '') return Number(stream.fileIdx);
    return null;
}

// dedupeStreamsByHash keeps one stream per infohash and records the sources
// that also had it on the survivor.
//
// The same torrent commonly comes back from a Stremio addon and from a
// Torznab indexer at once. Showing it twice is noise, and the copy to keep
// is the first one passed in — callers order addons before indexers because
// an addon result carries a file index the indexer cannot know. But dropping
// the duplicate silently loses something the user cares about: whether their
// own indexer found the release at all. So the survivor carries `alsoFrom`,
// and the row renders it.
//
// Streams without a hash are kept as-is — they are already unplayable and
// there is nothing to compare.
export function dedupeStreamsByHash(streams) {
    const byHash = new Map();
    const out = [];
    for (const s of streams || []) {
        const hash = extractInfoHash(s);
        if (!hash) { out.push(s); continue; }
        const kept = byHash.get(hash);
        if (!kept) {
            const copy = { ...s };
            byHash.set(hash, copy);
            out.push(copy);
            continue;
        }
        const name = sourceName(s);
        if (!name || name === sourceName(kept)) continue;
        if (!kept.alsoFrom) kept.alsoFrom = [];
        if (!kept.alsoFrom.includes(name)) kept.alsoFrom.push(name);
    }
    return out;
}

// interleaveBySource round-robins the merged list across its sources.
//
// Merging keeps sources in a fixed order (addons first) because dedup has
// to prefer the addon copy — but that same order buries a source that
// answers with eight results under one that answers with forty. On the
// server the resolution grouping in PreferredStream mixes them back up;
// here nothing did, so an indexer's results sat below the fold and read as
// "it didn't search at all". Each source keeps its own ranking; only the
// cross-source order changes.
export function interleaveBySource(streams) {
    const groups = new Map();
    for (const s of streams || []) {
        const key = sourceName(s) || '';
        if (!groups.has(key)) groups.set(key, []);
        groups.get(key).push(s);
    }
    if (groups.size < 2) return streams || [];
    const lists = [...groups.values()];
    const out = [];
    for (let i = 0; out.length < (streams || []).length; i++) {
        for (const list of lists) {
            if (i < list.length) out.push(list[i]);
        }
    }
    return out;
}

// sourceName is what to call the thing a stream came from: the addon or
// indexer name the client attached, falling back to the first line of the
// stream name, which is where every source puts its own label.
export function sourceName(stream) {
    if (stream?.addonName) return stream.addonName;
    return parseStreamName(stream?.name).source;
}

export function extractInfoHash(stream) {
    if (stream.infoHash) return stream.infoHash.toLowerCase();
    const hashRe = /([0-9a-fA-F]{40})/;
    if (stream.url) {
        const match = stream.url.match(hashRe);
        if (match) return match[1].toLowerCase();
    }
    if (stream.externalUrl) {
        const match = stream.externalUrl.match(hashRe);
        if (match) return match[1].toLowerCase();
    }
    return null;
}
