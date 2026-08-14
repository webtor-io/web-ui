package torznab

import (
	"strings"

	"github.com/pkg/errors"
	jackett "github.com/webtor-io/go-jackett"
)

// Endpoint is one user-configured Torznab feed: the URL exactly as the user
// pasted it (Jackett's "Copy Torznab Feed", a Prowlarr indexer URL, a
// tracker's own feed) plus the API key we split out of it at add time.
type Endpoint struct {
	URL    string
	APIKey string
	// Name is only used for logging and for the stream label.
	Name string
}

// Result is one item of a search response. Aliased rather than copied: the
// library already normalises the spelling differences between Jackett,
// Prowlarr and bare tracker feeds, and it carries fields (languages, imdb
// id, season/episode) we have no use for yet but would have to re-derive if
// we flattened it now.
type Result = jackett.Result

// SearchType is the Torznab `t` parameter.
type SearchType string

const (
	SearchTypeSearch SearchType = "search"
	SearchTypeMovie  SearchType = "movie"
	SearchTypeTV     SearchType = "tvsearch"
)

// Query is a single Torznab search, in the terms this service thinks in.
// It is deliberately narrower than go-jackett's builder API: the stream
// layer only ever searches for a movie or an episode, and keeping the
// translation in one place means a library change lands in one file.
type Query struct {
	Type    SearchType
	Q       string
	IMDBID  string
	Season  *int
	Episode *int
	Cats    []int
}

// fetchRequest translates the query into go-jackett's request model.
func (q Query) fetchRequest() (*jackett.FetchRequest, error) {
	cats := make([]uint, 0, len(q.Cats))
	for _, c := range q.Cats {
		if c > 0 {
			cats = append(cats, uint(c))
		}
	}

	switch q.Type {
	case SearchTypeMovie:
		s := jackett.NewMovieSearch().WithCategories(cats...)
		if q.Q != "" {
			s = s.WithQuery(q.Q)
		}
		if id := normalizeIMDBID(q.IMDBID); id != "" {
			s = s.WithIMDBID(id)
		}
		return s.Build(), nil
	case SearchTypeTV:
		s := jackett.NewTVSearch().WithCategories(cats...)
		if q.Q != "" {
			s = s.WithQuery(q.Q)
		}
		if id := normalizeIMDBID(q.IMDBID); id != "" {
			s = s.WithIMDBID(id)
		}
		if q.Season != nil && *q.Season > 0 {
			s = s.WithSeason(uint(*q.Season))
		}
		if q.Episode != nil && *q.Episode > 0 {
			s = s.WithEpisode(uint(*q.Episode))
		}
		return s.Build(), nil
	case SearchTypeSearch:
		s := jackett.NewRawSearch().WithCategories(cats...)
		if q.Q != "" {
			s = s.WithQuery(q.Q)
		}
		return s.Build(), nil
	}
	return nil, errors.Errorf("unsupported search type %q", q.Type)
}

// normalizeIMDBID strips the "tt" prefix. The Newznab spec defines imdbid as
// the bare numeric id; Jackett tolerates both spellings but several bare
// tracker feeds do not.
func normalizeIMDBID(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "tt")
}
