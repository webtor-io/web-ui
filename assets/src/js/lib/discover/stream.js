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
