// Package genre is the site's one genre vocabulary: a fixed list of slugs
// and the TMDB genre ids they are derived from. Labels are locale keys
// (genre.<slug>); see docs/genres.md.
package genre

import "slices"

// Slugs is the vocabulary in display order. A slug is both a locale key and
// a value stored in movie_metadata.genres / series_metadata.genres, so
// renaming one takes a migration.
var Slugs = []string{
	"action", "adventure", "animation", "biography", "comedy", "crime",
	"documentary", "drama", "family", "fantasy", "film_noir", "game_show",
	"history", "horror", "kids", "music", "musical", "mystery", "news",
	"politics", "reality", "romance", "scifi", "short", "soap", "sport",
	"talk", "thriller", "tv_movie", "war", "western",
}

// tmdbIDs maps TMDB genre ids to slugs; film and TV ids share one map. TV's
// combined genres are split, because TMDB never gives a series 28, 12, 14,
// 878 or 10752, and a series must still match an "Action" filter.
var tmdbIDs = map[int][]string{
	28:    {"action"},
	12:    {"adventure"},
	16:    {"animation"},
	35:    {"comedy"},
	80:    {"crime"},
	99:    {"documentary"},
	18:    {"drama"},
	10751: {"family"},
	14:    {"fantasy"},
	36:    {"history"},
	27:    {"horror"},
	10402: {"music"},
	9648:  {"mystery"},
	10749: {"romance"},
	878:   {"scifi"},
	10770: {"tv_movie"},
	53:    {"thriller"},
	10752: {"war"},
	37:    {"western"},
	10759: {"action", "adventure"},
	10762: {"kids"},
	10763: {"news"},
	10764: {"reality"},
	10765: {"scifi", "fantasy"},
	10766: {"soap"},
	10767: {"talk"},
	10768: {"war", "politics"},
}

// FromTMDB reads the "genres" value of a TMDB details response
// ([{"id": N, "name": "..."}] as decoded from JSON) into slugs, deduplicated,
// in TMDB's order. Unknown ids and malformed entries are skipped. Never nil:
// empty means "TMDB lists none" and is stored as '{}', while NULL means
// "not derived yet".
func FromTMDB(raw any) []string {
	out := []string{}
	list, _ := raw.([]any)
	for _, e := range list {
		g, _ := e.(map[string]any)
		id, ok := g["id"].(float64)
		if !ok {
			continue
		}
		for _, s := range tmdbIDs[int(id)] {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// IsSlug reports whether s is in the vocabulary.
func IsSlug(s string) bool {
	return slices.Contains(Slugs, s)
}
