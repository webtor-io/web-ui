// Genre names as addons send them → the slugs of the vocabulary shared with
// the library and torrent pages (services/genre, docs/genres.md). A slug's
// label is the locale key genre.<slug>, so a genre reads the same on every
// page. The names are Cinemeta's IMDb-style options plus the TMDB English
// names that Cinemeta metas with only TMDB data carry, the combined TV
// genres split into their parts. Matching is exact and case-sensitive.
//
// A Map, not an object literal: the names come from third-party addons, and
// "constructor" must not find Object.prototype's.
const SLUGS = new Map(Object.entries({
    'Action': ['action'],
    'Adventure': ['adventure'],
    'Animation': ['animation'],
    'Biography': ['biography'],
    'Comedy': ['comedy'],
    'Crime': ['crime'],
    'Documentary': ['documentary'],
    'Drama': ['drama'],
    'Family': ['family'],
    'Fantasy': ['fantasy'],
    'Film-Noir': ['film_noir'],
    'Game-Show': ['game_show'],
    'History': ['history'],
    'Horror': ['horror'],
    'Music': ['music'],
    'Musical': ['musical'],
    'Mystery': ['mystery'],
    'News': ['news'],
    'Reality-TV': ['reality'],
    'Romance': ['romance'],
    'Sci-Fi': ['scifi'],
    'Short': ['short'],
    'Sport': ['sport'],
    'Talk-Show': ['talk'],
    'Thriller': ['thriller'],
    'War': ['war'],
    'Western': ['western'],
    'Science Fiction': ['scifi'],
    'Action & Adventure': ['action', 'adventure'],
    'Sci-Fi & Fantasy': ['scifi', 'fantasy'],
    'War & Politics': ['war', 'politics'],
    'Reality': ['reality'],
    'Talk': ['talk'],
    'Kids': ['kids'],
    'Soap': ['soap'],
    'TV Movie': ['tv_movie'],
}));

// Every name the map knows (genres.test.js checks each slug has a label).
export const ADDON_GENRE_NAMES = [...SLUGS.keys()];

function names(list) {
    return Array.isArray(list) ? list.filter(n => typeof n === 'string' && n) : [];
}

// genreSlugs: the known slugs of a meta's genre names, deduped, in order of
// first appearance. Unknown names are not slugs and are left out.
export function genreSlugs(list) {
    const out = [];
    for (const n of names(list)) {
        for (const s of SLUGS.get(n) || []) if (!out.includes(s)) out.push(s);
    }
    return out;
}

// genreLabels: what a card or the modal shows for a meta's genre names. A
// known name reads as its slug's label, an unknown one as itself (never as a
// key); deduped by label, so "Sci-Fi" and "Science Fiction" read once.
export function genreLabels(list, t) {
    const out = [];
    for (const n of names(list)) {
        const slugs = SLUGS.get(n);
        for (const label of slugs ? slugs.map(s => t(`genre.${s}`)) : [n]) {
            if (!out.includes(label)) out.push(label);
        }
    }
    return out;
}

// optionLabel: one value of a catalog's genre filter, as the select shows it.
// The value itself stays what is sent: addons match the English option.
export function optionLabel(value, t) {
    return genreLabels([value], t).join(' & ');
}

// catalogFilter reads a manifest catalog's `genre` extra (or the legacy
// extraSupported / extraRequired / genres form) into the filter Discover
// offers for it, or null:
//   genre — optional, with options: a genre select, "All genres" first;
//   year  — required, every option a 4-digit year: Cinemeta's "New"
//           catalogs carry the year in the genre extra. A year select,
//           no "All", the first option (Cinemeta's newest) by default.
// Any other required genre has no default to pick, so that catalog keeps
// being requested without extras, as before the filter existed.
export function catalogFilter(cat) {
    const extra = Array.isArray(cat.extra) ? cat.extra.find(e => e?.name === 'genre') : undefined;
    const legacy = Array.isArray(cat.extraSupported) && cat.extraSupported.includes('genre');
    const options = [...new Set(names(extra ? extra.options : legacy ? cat.genres : null))];
    if (!options.length) return null;
    const required = !!extra?.isRequired || (Array.isArray(cat.extraRequired) && cat.extraRequired.includes('genre'));
    if (!required) return { mode: 'genre', options };
    if (options.every(o => /^\d{4}$/.test(o))) return { mode: 'year', options };
    return null;
}

// genreFor: the value a catalog is shown with — `value` when the catalog
// offers it, else its default (none for a genre filter, the first year for
// a year filter). An unknown value from a link lands on the default.
export function genreFor(catalog, value) {
    const f = catalog?.filter;
    if (!f) return null;
    if (value != null && f.options.includes(value)) return value;
    return f.mode === 'year' ? f.options[0] : null;
}

// filterOptions: the select's options. Genres sort by their label in the
// page's language; years keep the manifest's order (newest first).
export function filterOptions(filter, t, lang) {
    const opts = filter.options.map(value => ({ value, label: optionLabel(value, t) }));
    if (filter.mode === 'genre') opts.sort((a, b) => a.label.localeCompare(b.label, lang));
    return opts;
}
