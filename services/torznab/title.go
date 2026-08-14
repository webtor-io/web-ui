package torznab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/lazymap"
)

// CinemetaBaseURL is the public Stremio metadata service. Discover's browser
// client already resolves titles against it (assets/src/js/lib/discover/
// client.js), so using it server-side keeps both surfaces on the same names.
const CinemetaBaseURL = "https://v3-cinemeta.strem.io"

// Title is the minimum a keyword query needs: what the thing is called and
// when it came out.
type Title struct {
	Name string
	Year string
}

// TitleResolver maps an IMDb id to a searchable title. Torznab indexers that
// advertise imdbid never need this; the ones that don't (most bare tracker
// feeds) can only be queried by name.
type TitleResolver interface {
	Resolve(ctx context.Context, contentType, imdbID string) (*Title, error)
}

// CinemetaTitles is the default TitleResolver. Lookups are cached for a day —
// a title's name does not change, and the cache is what keeps an indexer
// without imdbid support from adding a Cinemeta round-trip to every stream
// request.
type CinemetaTitles struct {
	cl    *http.Client
	ua    string
	cache *lazymap.LazyMap[*Title]
}

func NewCinemetaTitles(cl *http.Client, ua string) *CinemetaTitles {
	return &CinemetaTitles{
		cl: cl,
		ua: ua,
		cache: lazymap.New[*Title](&lazymap.Config{
			Expire:      24 * time.Hour,
			ErrorExpire: 1 * time.Minute,
		}),
	}
}

func (s *CinemetaTitles) Resolve(ctx context.Context, contentType, imdbID string) (*Title, error) {
	if imdbID == "" {
		return nil, errors.New("no content id")
	}
	return s.cache.Get(fmt.Sprintf("%s/%s", contentType, imdbID), func() (*Title, error) {
		return s.fetch(ctx, contentType, imdbID)
	})
}

func (s *CinemetaTitles) fetch(ctx context.Context, contentType, imdbID string) (*Title, error) {
	u := fmt.Sprintf("%s/meta/%s/%s.json", CinemetaBaseURL, contentType, imdbID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Accept", "application/json")
	if s.ua != "" {
		req.Header.Set("User-Agent", s.ua)
	}
	resp, err := s.cl.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to reach cinemeta")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("cinemeta returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read cinemeta response")
	}
	var parsed struct {
		Meta struct {
			Name        string `json:"name"`
			ReleaseInfo string `json:"releaseInfo"`
			Year        string `json:"year"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.Wrap(err, "failed to decode cinemeta response")
	}
	name := strings.TrimSpace(parsed.Meta.Name)
	if name == "" {
		return nil, errors.New("cinemeta has no title for this id")
	}
	return &Title{
		Name: name,
		Year: firstYear(parsed.Meta.ReleaseInfo, parsed.Meta.Year),
	}, nil
}

var yearRe = regexp.MustCompile(`\d{4}`)

// firstYear takes the start year out of Cinemeta's releaseInfo, which is a
// bare year for movies and a range ("2008-2013", "2016-") for series.
func firstYear(candidates ...string) string {
	for _, c := range candidates {
		if m := yearRe.FindString(c); m != "" {
			return m
		}
	}
	return ""
}
