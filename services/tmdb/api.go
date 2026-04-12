package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	tmdbApiKeyFlag        = "tmdb-api-key"
	tmdbApiSecureFlag     = "tmdb-api-secure"
	tmdbApiHostFlag       = "tmdb-api-host"
	tmdbApiPortFlag       = "tmdb-api-port"
	tmdbImageBaseURLFlag  = "tmdb-image-base-url"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   tmdbApiHostFlag,
			Usage:  "tmdb api host",
			EnvVar: "TMDB_API_HOST",
			Value:  "api.themoviedb.org",
		},
		cli.IntFlag{
			Name:   tmdbApiPortFlag,
			Usage:  "tmdb api port",
			EnvVar: "TMDB_API_PORT",
			Value:  443,
		},
		cli.BoolTFlag{
			Name:   tmdbApiSecureFlag,
			Usage:  "tmdb api secure (https)",
			EnvVar: "TMDB_API_SECURE",
		},
		cli.StringFlag{
			Name:   tmdbApiKeyFlag,
			Usage:  "tmdb api key (v3 auth)",
			Value:  "",
			EnvVar: "TMDB_API_KEY",
		},
		cli.StringFlag{
			Name:   tmdbImageBaseURLFlag,
			Usage:  "tmdb image base url",
			EnvVar: "TMDB_IMAGE_BASE_URL",
			Value:  "https://image.tmdb.org/t/p",
		},
	)
}

type TmdbType string

const (
	TmdbTypeMovie TmdbType = "movie"
	TmdbTypeTV    TmdbType = "tv"
)

type SearchResult struct {
	ID    int            `json:"id"`
	Title string         `json:"-"`
	Raw   map[string]any `json:"-"`
}

type FindByExternalIDResponse struct {
	MovieResults []struct {
		ID int `json:"id"`
	} `json:"movie_results"`
	TVResults []struct {
		ID int `json:"id"`
	} `json:"tv_results"`
}

type SeasonResponse struct {
	Episodes []SeasonEpisode `json:"episodes"`
	Raw      map[string]any  `json:"-"`
}

type SeasonEpisode struct {
	EpisodeNumber int     `json:"episode_number"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	StillPath     *string `json:"still_path"`
	AirDate       string  `json:"air_date"`
	VoteAverage   float64 `json:"vote_average"`
}

// DiscoverResult is a single film returned by the /discover/movie endpoint.
// We only parse the fields needed by the enrichment pipeline; the full
// metadata (genres with names, overview, poster, etc.) is fetched later
// via GetDetails for films that aren't already in tmdb.info.
type DiscoverResult struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	ReleaseDate string  `json:"release_date"`
	VoteAverage float64 `json:"vote_average"`
	VoteCount   int     `json:"vote_count"`
}

type Api struct {
	url            string
	cl             *http.Client
	prepareRequest func(r *http.Request) (*http.Request, error)
	imageBaseURL   string
}

func New(c *cli.Context, cl *http.Client) *Api {
	host := c.String(tmdbApiHostFlag)
	port := c.Int(tmdbApiPortFlag)
	secure := c.BoolT(tmdbApiSecureFlag)
	key := c.String(tmdbApiKeyFlag)
	if key == "" {
		return nil
	}

	protocol := "http"
	if secure {
		protocol = "https"
	}

	u := fmt.Sprintf("%v://%v:%v", protocol, host, port)
	prepareRequest := func(r *http.Request) (*http.Request, error) {
		q := r.URL.Query()
		q.Set("api_key", key)
		r.URL.RawQuery = q.Encode()
		return r, nil
	}

	log.Infof("tmdb api endpoint %v", u)

	return &Api{
		url:            u,
		cl:             cl,
		prepareRequest: prepareRequest,
		imageBaseURL:   c.String(tmdbImageBaseURLFlag),
	}
}

func (api *Api) SearchMovie(ctx context.Context, title string, year *int16) (*SearchResult, error) {
	return api.search(ctx, title, year, TmdbTypeMovie)
}

func (api *Api) SearchTV(ctx context.Context, title string, year *int16) (*SearchResult, error) {
	return api.search(ctx, title, year, TmdbTypeTV)
}

func (api *Api) Search(ctx context.Context, title string, year *int16, tmdbType TmdbType) (*SearchResult, error) {
	return api.search(ctx, title, year, tmdbType)
}

func (api *Api) search(ctx context.Context, title string, year *int16, tmdbType TmdbType) (*SearchResult, error) {
	u, _ := url.Parse(fmt.Sprintf("%s/3/search/%s", api.url, tmdbType))
	q := u.Query()
	q.Set("query", title)
	q.Set("page", "1")
	if year != nil {
		yearParam := "year"
		if tmdbType == TmdbTypeTV {
			yearParam = "first_air_date_year"
		}
		q.Set(yearParam, strconv.Itoa(int(*year)))
	}
	u.RawQuery = q.Encode()

	raw, err := api.doRequest(ctx, u.String())
	if err != nil {
		return nil, errors.Wrap(err, "tmdb search request")
	}

	results, ok := raw["results"].([]any)
	if !ok || len(results) == 0 {
		return nil, nil
	}

	first, ok := results[0].(map[string]any)
	if !ok {
		return nil, nil
	}

	id, _ := first["id"].(float64)
	var resultTitle string
	if tmdbType == TmdbTypeMovie {
		resultTitle, _ = first["title"].(string)
	} else {
		resultTitle, _ = first["name"].(string)
	}

	return &SearchResult{
		ID:    int(id),
		Title: resultTitle,
		Raw:   first,
	}, nil
}

func (api *Api) GetDetails(ctx context.Context, tmdbID int, tmdbType TmdbType) (map[string]any, error) {
	u, _ := url.Parse(fmt.Sprintf("%s/3/%s/%d", api.url, tmdbType, tmdbID))
	q := u.Query()
	// Append credits so the metadata JSONB includes cast and crew —
	// needed by the AI recommendations prompt to match actor/director
	// queries against fresh releases that postdate Claude's training data.
	q.Set("append_to_response", "credits")
	u.RawQuery = q.Encode()

	raw, err := api.doRequest(ctx, u.String())
	if err != nil {
		return nil, errors.Wrap(err, "tmdb get details")
	}

	return raw, nil
}

func (api *Api) GetExternalIDs(ctx context.Context, tmdbID int, tmdbType TmdbType) (map[string]any, error) {
	u := fmt.Sprintf("%s/3/%s/%d/external_ids", api.url, tmdbType, tmdbID)

	raw, err := api.doRequest(ctx, u)
	if err != nil {
		return nil, errors.Wrap(err, "tmdb get external ids")
	}

	return raw, nil
}

func (api *Api) FindByExternalID(ctx context.Context, externalID string, source string) (*FindByExternalIDResponse, error) {
	u, _ := url.Parse(fmt.Sprintf("%s/3/find/%s", api.url, externalID))
	q := u.Query()
	q.Set("external_source", source)
	u.RawQuery = q.Encode()

	raw, err := api.doRequest(ctx, u.String())
	if err != nil {
		return nil, errors.Wrap(err, "tmdb find by external id")
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.Wrap(err, "marshal find response")
	}

	var resp FindByExternalIDResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, "unmarshal find response")
	}

	return &resp, nil
}

func (api *Api) GetTVSeason(ctx context.Context, tvID int, seasonNumber int) (*SeasonResponse, error) {
	u := fmt.Sprintf("%s/3/tv/%d/season/%d", api.url, tvID, seasonNumber)

	raw, err := api.doRequest(ctx, u)
	if err != nil {
		return nil, errors.Wrap(err, "tmdb get tv season")
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.Wrap(err, "marshal season response")
	}

	var resp SeasonResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, "unmarshal season response")
	}
	resp.Raw = raw

	return &resp, nil
}

func (api *Api) StillURL(stillPath string, size string) string {
	return fmt.Sprintf("%s/%s%s", api.imageBaseURL, size, stillPath)
}

// DiscoverMovies calls the TMDB /discover/movie endpoint to fetch popular
// recent releases. releaseDateGte filters to films released on or after
// that date (YYYY-MM-DD). Results are sorted by popularity descending.
// Returns the results for the requested page and the total number of
// pages available (for pagination).
func (api *Api) DiscoverMovies(ctx context.Context, releaseDateGte string, minVotes int, page int) ([]DiscoverResult, int, error) {
	u, _ := url.Parse(fmt.Sprintf("%s/3/discover/movie", api.url))
	q := u.Query()
	q.Set("sort_by", "popularity.desc")
	q.Set("primary_release_date.gte", releaseDateGte)
	q.Set("vote_count.gte", strconv.Itoa(minVotes))
	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()

	raw, err := api.doRequest(ctx, u.String())
	if err != nil {
		return nil, 0, errors.Wrap(err, "tmdb discover movies")
	}
	if raw == nil {
		return nil, 0, nil
	}

	totalPages := 1
	if tp, ok := raw["total_pages"].(float64); ok {
		totalPages = int(tp)
	}

	results, ok := raw["results"].([]any)
	if !ok || len(results) == 0 {
		return nil, totalPages, nil
	}

	out := make([]DiscoverResult, 0, len(results))
	for _, r := range results {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		dr := DiscoverResult{}
		if id, ok := m["id"].(float64); ok {
			dr.ID = int(id)
		}
		if t, ok := m["title"].(string); ok {
			dr.Title = t
		}
		if rd, ok := m["release_date"].(string); ok {
			dr.ReleaseDate = rd
		}
		if va, ok := m["vote_average"].(float64); ok {
			dr.VoteAverage = va
		}
		if vc, ok := m["vote_count"].(float64); ok {
			dr.VoteCount = int(vc)
		}
		if dr.ID > 0 {
			out = append(out, dr)
		}
	}

	return out, totalPages, nil
}

func (api *Api) PosterURL(posterPath string, size string) string {
	return fmt.Sprintf("%s/%s%s", api.imageBaseURL, size, posterPath)
}

func (api *Api) doRequest(ctx context.Context, rawURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}

	req, err = api.prepareRequest(req)
	if err != nil {
		return nil, errors.Wrap(err, "prepare request")
	}

	resp, err := api.cl.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "request failed")
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}

	return raw, nil
}
