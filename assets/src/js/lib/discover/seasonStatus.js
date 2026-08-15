// "Is this season still going" — the client-side half of the rule the
// server enforces on write.
//
// The server ends a season subscription only when every episode has aired
// AND the series is out of production (services/release_subscription:
// seasonIsOver). This is the same question asked in the other direction,
// from the metadata Discover already holds: the addon meta's videos, each
// with a `released` date, and the series status when the addon supplies one.
//
// Kept as a plain module — no JSX, no Preact — so the rule can be tested
// directly. StreamModal only draws what it returns.

// Statuses an addon may use to say "more episodes are coming". Cinemeta
// says "Continuing", TMDB-backed addons pass TMDB's own wording through.
const AIRING_STATUSES = ['continuing', 'returning series', 'in production', 'airing'];

export function isAiringStatus(status) {
    if (!status) return false;
    return AIRING_STATUSES.includes(String(status).trim().toLowerCase());
}

// seasonsOf groups meta videos by season, dropping specials (season 0) and
// entries with no season at all — neither can be subscribed to.
export function seasonsOf(videos) {
    const out = new Map();
    for (const v of videos || []) {
        const season = Number(v.season);
        if (!Number.isFinite(season) || season <= 0) continue;
        if (!out.has(season)) out.set(season, []);
        out.get(season).push(v);
    }
    return out;
}

// isSeasonUnfinished answers whether a subscription to this season has a
// future to wait for.
//
// Two ways it can be true, mirroring the two halves of the server's rule:
//
//  1. an episode of the season has a release date still ahead — the season
//     is mid-run, whatever the series status says;
//  2. it is the latest season of a series still in production, and its
//     dates simply have not been published yet. Without this a subscription
//     would be refused in exactly the gap where someone following the show
//     wants one.
//
// An episode with no date at all counts as "unknown", not as "aired": a
// season the addon knows only partially must not read as finished.
export function isSeasonUnfinished(videos, season, { now = Date.now(), status = '' } = {}) {
    const wanted = Number(season);
    if (!Number.isFinite(wanted) || wanted <= 0) return false;

    const seasons = seasonsOf(videos);
    const episodes = seasons.get(wanted);
    if (!episodes || episodes.length === 0) return false;

    for (const e of episodes) {
        if (!e.released) return true;
        const at = Date.parse(e.released);
        if (Number.isNaN(at)) return true;
        if (at > now) return true;
    }

    if (!isAiringStatus(status)) return false;
    const latest = Math.max(...seasons.keys());
    return wanted === latest;
}

// unfinishedSeasons lists every season worth offering a subscription for.
export function unfinishedSeasons(videos, opts) {
    const out = [];
    for (const season of seasonsOf(videos).keys()) {
        if (isSeasonUnfinished(videos, season, opts)) out.push(season);
    }
    return out.sort((a, b) => a - b);
}

// subscriptionTargetFor reads the subscribe target out of what the streams
// modal knows: the content type, the id it was opened with, and the series
// meta when the episodes view already fetched it.
//
// An episode id (tt…:S:E) means the season; a movie means the film. Anything
// else offers nothing — a series opened without an episode has no season to
// name, and a library id (wt-…) is not something an external source could
// ever be asked about.
//
// With the meta in hand a finished season is not offered at all. Without it
// the offer stands: the write path re-checks, and its refusal explains why.
export function subscriptionTargetFor(type, id, metaId, meta) {
    if (!metaId || !metaId.startsWith('tt')) return null;
    const parts = String(id).split(':');
    if (parts.length >= 3) {
        const season = Number(parts[1]);
        if (!Number.isFinite(season) || season <= 0) return null;
        if (meta?.videos?.length && !isSeasonUnfinished(meta.videos, season, { status: meta.status })) {
            return null;
        }
        return { kind: 'season', videoId: metaId, season };
    }
    if (type === 'movie') return { kind: 'movie', videoId: metaId };
    return null;
}
