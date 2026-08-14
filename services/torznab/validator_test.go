package torznab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/webtor-io/web-ui/models"
)

const jackettCaps = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="Jackett"/>
  <limits max="100" default="50"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="q,season,ep,imdbid,tvdbid"/>
    <movie-search available="yes" supportedParams="q,imdbid"/>
  </searching>
  <categories>
    <category id="2000" name="Movies"><subcat id="2040" name="HD"/></category>
    <category id="5000" name="TV"/>
  </categories>
</caps>`

func TestParseFeedURL(t *testing.T) {
	for _, tt := range []struct {
		name     string
		raw      string
		explicit string
		wantURL  string
		wantKey  string
		wantErr  bool
	}{
		{
			name:    "api key inside the pasted URL is lifted out",
			raw:     "https://jackett.example.com/api/v2.0/indexers/all/results/torznab?apikey=abc123",
			wantURL: "https://jackett.example.com/api/v2.0/indexers/all/results/torznab",
			wantKey: "abc123",
		},
		{
			name:    "jackett_apikey spelling is recognised too",
			raw:     "https://jackett.example.com/torznab?jackett_apikey=abc123",
			wantURL: "https://jackett.example.com/torznab",
			wantKey: "abc123",
		},
		{
			name:     "explicit key wins over the inline one",
			raw:      "https://jackett.example.com/torznab?apikey=inline",
			explicit: "explicit",
			wantURL:  "https://jackett.example.com/torznab",
			wantKey:  "explicit",
		},
		{
			name:    "a pasted t= does not survive",
			raw:     "https://jackett.example.com/torznab?t=caps&apikey=k",
			wantURL: "https://jackett.example.com/torznab",
			wantKey: "k",
		},
		{name: "empty input", raw: "  ", wantErr: true},
		{name: "wrong scheme", raw: "ftp://jackett.example.com/torznab", wantErr: true},
		{name: "no host", raw: "https:///torznab", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotKey, err := ParseFeedURL(tt.raw, tt.explicit)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseFeedURL(%q) succeeded, want error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFeedURL(%q) error = %v", tt.raw, err)
			}
			if gotURL != tt.wantURL {
				t.Errorf("url = %q, want %q", gotURL, tt.wantURL)
			}
			if gotKey != tt.wantKey {
				t.Errorf("key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

func TestValidateAndFetch(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Encode()
		_, _ = w.Write([]byte(jackettCaps))
	}))
	defer srv.Close()

	v := NewValidator(testClient())
	name, caps, err := v.ValidateAndFetch(context.Background(), Endpoint{URL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("ValidateAndFetch() error = %v", err)
	}
	if name != "Jackett" {
		t.Errorf("name = %q, want Jackett", name)
	}
	if !caps.SupportsMovieIMDB() || !caps.SupportsTVIMDB() || !caps.SupportsSeasonEpisode() {
		t.Errorf("caps = %+v, want imdbid + season/ep support", caps)
	}
	if caps.Limit != 100 {
		t.Errorf("limit = %d, want 100", caps.Limit)
	}
	if len(caps.Categories) != 2 {
		t.Errorf("categories = %v, want the two top-level ids", caps.Categories)
	}
	if want := "apikey=k&t=caps"; gotQuery != want {
		t.Errorf("probe query = %q, want %q", gotQuery, want)
	}
}

func TestValidateAndFetchRejectsNonTorznab(t *testing.T) {
	// Pointing the field at a Jackett dashboard rather than its feed is the
	// most likely user error; it must fail on the form, not silently
	// become an indexer that never returns anything.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>Jackett</body></html>"))
	}))
	defer srv.Close()

	v := NewValidator(testClient())
	if _, _, err := v.ValidateAndFetch(context.Background(), Endpoint{URL: srv.URL}); err == nil {
		t.Fatal("ValidateAndFetch() accepted a non-caps document")
	}
}

func TestValidateAndFetchRejectsCapsWithoutSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<caps><server title="Nothing"/><searching><search available="no"/><tv-search available="no"/><movie-search available="no"/></searching></caps>`))
	}))
	defer srv.Close()

	v := NewValidator(testClient())
	if _, _, err := v.ValidateAndFetch(context.Background(), Endpoint{URL: srv.URL}); err == nil {
		t.Fatal("ValidateAndFetch() accepted an indexer with no search mode")
	}
}

func TestParseParamsAssumesQWhenUnspecified(t *testing.T) {
	// The spec makes supportedParams optional; several bare tracker feeds
	// omit it while accepting q. Treating that as "no params" would mark
	// the indexer unusable.
	got := parseParams("yes", "")
	if len(got) != 1 || got[0] != "q" {
		t.Errorf("parseParams(available, no params) = %v, want [q]", got)
	}
	if got := parseParams("no", "q,imdbid"); got != nil {
		t.Errorf("parseParams(unavailable) = %v, want nil", got)
	}
}

func TestCategoriesFor(t *testing.T) {
	caps := &models.TorznabCaps{Categories: []int{2000, 5000, 3000}}
	if got := CategoriesFor(caps, "movie"); len(got) != 1 || got[0] != CategoryMovies {
		t.Errorf("CategoriesFor(movie) = %v, want [2000]", got)
	}
	if got := CategoriesFor(caps, "series"); len(got) != 1 || got[0] != CategoryTV {
		t.Errorf("CategoriesFor(series) = %v, want [5000]", got)
	}
	// An indexer that never advertised categories must be queried
	// unfiltered — sending cat= it doesn't know would return nothing.
	if got := CategoriesFor(&models.TorznabCaps{}, "movie"); got != nil {
		t.Errorf("CategoriesFor(no categories) = %v, want nil", got)
	}
	if got := CategoriesFor(nil, "movie"); got != nil {
		t.Errorf("CategoriesFor(nil caps) = %v, want nil", got)
	}
}
