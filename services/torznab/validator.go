package torznab

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	jackett "github.com/webtor-io/go-jackett"
	"github.com/webtor-io/web-ui/models"
)

// Torznab category ids for the two content types Webtor streams. Queries are
// narrowed to these when the indexer advertises them, which keeps music and
// ebook releases out of a movie's stream list.
const (
	CategoryMovies = 2000
	CategoryTV     = 5000
)

// apiKeyParams are the spellings of the API key parameter seen in the wild:
// Torznab standardised on apikey, Jackett's UI hands out links with
// apikey, and its /dl/ links use jackett_apikey.
var apiKeyParams = []string{"apikey", "apiKey", "api_key", "jackett_apikey"}

// ParseFeedURL splits a pasted feed URL into the URL we store and the API
// key we store beside it.
//
// Users paste whatever their indexer's UI gave them — Jackett's "Copy
// Torznab Feed" includes the key, Prowlarr's does too — so accepting the
// key inline and lifting it out is the only flow that does not ask people
// to hand-edit a URL. An explicitly supplied key wins over an inline one.
func ParseFeedURL(raw, apiKey string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("no indexer URL provided")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", errors.New("invalid URL format")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", errors.New("URL must use http or https protocol")
	}
	if u.Host == "" {
		return "", "", errors.New("URL has no host")
	}

	v := u.Query()
	inlineKey := ""
	for _, p := range apiKeyParams {
		if val := v.Get(p); val != "" && inlineKey == "" {
			inlineKey = val
		}
		v.Del(p)
	}
	// t is per-query — keeping a pasted t=search would be harmless but
	// keeping t=caps would make every search return capabilities.
	v.Del("t")
	u.RawQuery = v.Encode()

	key := strings.TrimSpace(apiKey)
	if key == "" {
		key = inlineKey
	}
	return u.String(), key, nil
}

// Validator probes a Torznab endpoint at add time and turns its caps
// document into the snapshot we persist. It is the Torznab counterpart of
// stremio.AddonValidator.
type Validator struct {
	client *Client
}

func NewValidator(client *Client) *Validator {
	return &Validator{client: client}
}

// ValidateAndFetch verifies the endpoint answers a caps query and returns
// its display name plus the persisted snapshot.
func (v *Validator) ValidateAndFetch(ctx context.Context, ep Endpoint) (string, *models.TorznabCaps, error) {
	var caps *jackett.IndexerCaps
	caps, err := v.client.Caps(ctx, ep)
	if err != nil {
		return "", nil, err
	}
	name := strings.TrimSpace(caps.Server.Title)
	if name == "" {
		name = hostOf(ep.URL)
	}
	snapshot := &models.TorznabCaps{
		Server:       strings.TrimSpace(caps.Server.Title),
		SearchParams: parseParams(caps.Searching.Search.Available, caps.Searching.Search.SupportedParams),
		MovieParams:  parseParams(caps.Searching.MovieSearch.Available, caps.Searching.MovieSearch.SupportedParams),
		TVParams:     parseParams(caps.Searching.TVSearch.Available, caps.Searching.TVSearch.SupportedParams),
		Limit:        atoiOrZero(caps.Limits.Max),
	}
	for _, cat := range caps.Categories.Categories {
		if id, err := strconv.Atoi(strings.TrimSpace(cat.ID)); err == nil {
			snapshot.Categories = append(snapshot.Categories, id)
		}
	}
	if len(snapshot.SearchParams) == 0 && len(snapshot.MovieParams) == 0 && len(snapshot.TVParams) == 0 {
		return "", nil, errors.New("indexer advertises no usable search mode")
	}
	return name, snapshot, nil
}

// parseParams returns the supported parameter list for a search mode, or nil
// when the mode is unavailable.
//
// An available mode with an empty supportedParams still counts as usable:
// the Torznab spec makes the attribute optional and several bare tracker
// feeds omit it while accepting q just fine, so "q" is assumed.
func parseParams(available, supportedParams string) []string {
	if !isAvailable(available) {
		return nil
	}
	raw := strings.Split(supportedParams, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = append(out, "q")
	}
	return out
}

func isAvailable(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "true", "1":
		return true
	}
	return false
}

// atoiOrZero parses go-jackett's string-typed caps limits, which are
// attributes several feeds omit entirely.
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Hostname()
	}
	return raw
}

// CategoriesFor picks the category filter for a content type, returning nil
// when the indexer never advertised categories (in which case an unfiltered
// query is the only option).
func CategoriesFor(caps *models.TorznabCaps, contentType string) []int {
	if caps == nil || len(caps.Categories) == 0 {
		return nil
	}
	want := CategoryMovies
	if contentType == "series" {
		want = CategoryTV
	}
	for _, c := range caps.Categories {
		if c == want {
			return []int{want}
		}
	}
	return nil
}
