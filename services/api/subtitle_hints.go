package api

import (
	"net/url"
	"strconv"
)

// SubtitleHints is what web-ui knows about the file beyond its bytes:
// the IMDb id from enrichment and, for a series file, which episode.
// video-info uses them when the OpenSubtitles hash lookup is empty.
type SubtitleHints struct {
	ImdbID  string
	Season  int
	Episode int
}

// WithSubtitleHints appends the hints as query parameters of a
// ~vi/subtitles.json URL. An imdb-id already present (rest-api forwards
// the one an API caller passed explicitly) is kept. Season is only
// meaningful together with episode. Query keys are re-encoded in sorted
// order, which is how url.Values encodes anyway.
func WithSubtitleHints(u string, h SubtitleHints) string {
	if h.ImdbID == "" {
		return u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	if q.Get("imdb-id") == "" {
		q.Set("imdb-id", h.ImdbID)
	}
	if h.Season > 0 && h.Episode > 0 {
		q.Set("season", strconv.Itoa(h.Season))
		q.Set("episode", strconv.Itoa(h.Episode))
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}
