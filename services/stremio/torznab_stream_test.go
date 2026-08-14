package stremio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	tn "github.com/webtor-io/web-ui/services/torznab"
)

type fakeTitles struct {
	title *tn.Title
	err   error

	mu    sync.Mutex
	calls int
}

func (f *fakeTitles) Resolve(_ context.Context, _, _ string) (*tn.Title, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.title, nil
}

func (f *fakeTitles) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newTestStream(t *testing.T, srvURL string, caps *models.TorznabCaps, titles tn.TitleResolver) *TorznabStream {
	t.Helper()
	return NewTorznabStream(
		tn.NewWithOptions(tn.Options{AllowPrivateNetwork: true, Timeout: 5 * time.Second}),
		models.TorznabIndexer{
			ID:   uuid.NewV4(),
			Url:  srvURL,
			Name: strp("My Jackett"),
			Caps: caps,
		},
		titles,
		lazymap.New[*StreamsResponse](&lazymap.Config{Expire: time.Minute}),
	)
}

func strp(s string) *string { return &s }

func intp(i int) *int { return &i }

const torznabFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
  <item>
    <title>Person.of.Interest.S05E14.1080p.WEB.rus.eng</title>
    <guid>https://jackett.example.com/api/v2.0/indexers/rutracker/results/torznab/1</guid>
    <size>2147483648</size>
    <torznab:attr name="seeders" value="42"/>
    <torznab:attr name="infohash" value="8C4ADBF9EBDC2C31E4B3D01A9E9C5C0F2A1B3C4D"/>
  </item>
  <item>
    <title>Person.of.Interest.S05E14.720p.WEB</title>
    <link>magnet:?xt=urn:btih:aaaabbbbccccddddeeeeffff0000111122223333</link>
    <torznab:attr name="seeders" value="9"/>
  </item>
  <item>
    <title>Person.of.Interest.S05E14.CAM.no-hash</title>
    <torznab:attr name="seeders" value="3"/>
  </item>
</channel>
</rss>`

func TestTorznabStreamGetStreams(t *testing.T) {
	var mu sync.Mutex
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		mu.Unlock()
		_, _ = w.Write([]byte(torznabFeed))
	}))
	defer srv.Close()

	caps := &models.TorznabCaps{
		TVParams:   []string{"q", "season", "ep", "imdbid"},
		Categories: []int{5000},
	}
	titles := &fakeTitles{title: &tn.Title{Name: "Person of Interest", Year: "2011"}}
	s := newTestStream(t, srv.URL, caps, titles)

	resp, err := s.GetStreams(context.Background(), "series", "tt1839578:5:14")
	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}

	// The third item carries no infohash, no magnet and no download link,
	// so it cannot be turned into a playable stream and must be dropped
	// rather than shown as a dead row.
	if len(resp.Streams) != 2 {
		t.Fatalf("got %d streams, want 2", len(resp.Streams))
	}

	first := resp.Streams[0]
	if first.InfoHash != "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d" {
		t.Errorf("infohash = %q, want the feed's value lowercased", first.InfoHash)
	}
	// PreferredStream parses the resolution out of Name, so the resolution
	// token has to be there for the user's preferences to apply.
	if !strings.Contains(first.Name, "1080p") {
		t.Errorf("name = %q, want a resolution token", first.Name)
	}
	if !strings.HasPrefix(first.Name, "My Jackett") {
		t.Errorf("name = %q, want the indexer name on the first line", first.Name)
	}
	// LangFilterStream reads languages out of Title, so the raw release
	// name has to survive into it.
	if !strings.HasPrefix(first.Title, "Person.of.Interest.S05E14.1080p.WEB.rus.eng") {
		t.Errorf("title = %q, want the release name first", first.Title)
	}
	if !strings.Contains(first.Title, "42") {
		t.Errorf("title = %q, want the seeder count", first.Title)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 1 {
		t.Fatalf("made %d requests, want 1 — the id query answered", len(queries))
	}
	q := queries[0]
	if q.Get("t") != "tvsearch" || q.Get("imdbid") != "1839578" || q.Get("season") != "5" || q.Get("ep") != "14" {
		t.Errorf("query = %v, want a tvsearch by imdbid+season+ep", q)
	}
	if q.Get("cat") != "5000" {
		t.Errorf("cat = %q, want the TV category", q.Get("cat"))
	}
	if titles.callCount() != 0 {
		t.Errorf("resolved a title %d times, want 0 — the id query was enough", titles.callCount())
	}
}

func TestTorznabStreamFallsBackToTitleQuery(t *testing.T) {
	var mu sync.Mutex
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		n := len(queries)
		mu.Unlock()
		if n == 1 {
			// Indexers that advertise imdbid still return nothing for
			// titles they have not tagged — by far the most common way an
			// id query comes back empty.
			_, _ = w.Write([]byte(`<rss version="2.0"><channel></channel></rss>`))
			return
		}
		_, _ = w.Write([]byte(torznabFeed))
	}))
	defer srv.Close()

	caps := &models.TorznabCaps{TVParams: []string{"q", "season", "ep", "imdbid"}}
	titles := &fakeTitles{title: &tn.Title{Name: "Person of Interest", Year: "2011"}}
	s := newTestStream(t, srv.URL, caps, titles)

	resp, err := s.GetStreams(context.Background(), "series", "tt1839578:5:14")
	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}
	if len(resp.Streams) != 2 {
		t.Fatalf("got %d streams, want 2 from the fallback query", len(resp.Streams))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 2 {
		t.Fatalf("made %d requests, want 2", len(queries))
	}
	// Measured against a live Jackett: "Title S05E14" in q returned zero
	// results where title + structured season/ep returned eight. The
	// episode markers belong in the parameters whenever the indexer takes
	// them.
	if got := queries[1].Get("q"); got != "Person of Interest" {
		t.Errorf("fallback q = %q, want the bare title", got)
	}
	if got, want := queries[1].Get("season"), "5"; got != want {
		t.Errorf("fallback season = %q, want %q", got, want)
	}
	if got, want := queries[1].Get("ep"), "14"; got != want {
		t.Errorf("fallback ep = %q, want %q", got, want)
	}
}

func TestTorznabStreamBuildQueries(t *testing.T) {
	titles := &fakeTitles{title: &tn.Title{Name: "The Matrix", Year: "1999"}}

	for _, tt := range []struct {
		name           string
		caps           *models.TorznabCaps
		contentType    string
		contentID      string
		wantIDType     tn.SearchType // "" = no id query
		wantIDQuery    bool
		wantTitleT     tn.SearchType
		wantTitleQ     string
		wantStructured bool
	}{
		{
			name:        "movie with imdbid support queries by id first",
			caps:        &models.TorznabCaps{MovieParams: []string{"q", "imdbid"}},
			contentType: "movie",
			contentID:   "tt0133093",
			wantIDQuery: true,
			wantIDType:  tn.SearchTypeMovie,
			wantTitleT:  tn.SearchTypeMovie,
			wantTitleQ:  "The Matrix 1999",
		},
		{
			name:        "movie without imdbid support has no id query",
			caps:        &models.TorznabCaps{MovieParams: []string{"q"}},
			contentType: "movie",
			contentID:   "tt0133093",
			wantTitleT:  tn.SearchTypeMovie,
			wantTitleQ:  "The Matrix 1999",
		},
		{
			name:        "indexer with only a plain search mode uses t=search",
			caps:        &models.TorznabCaps{SearchParams: []string{"q"}},
			contentType: "movie",
			contentID:   "tt0133093",
			wantTitleT:  tn.SearchTypeSearch,
			wantTitleQ:  "The Matrix 1999",
		},
		{
			name:        "unknown caps tries the id query anyway",
			caps:        nil,
			contentType: "movie",
			contentID:   "tt0133093",
			wantIDQuery: true,
			wantIDType:  tn.SearchTypeMovie,
			wantTitleT:  tn.SearchTypeMovie,
			wantTitleQ:  "The Matrix 1999",
		},
		{
			// No structured season/ep: the episode marker has to ride in
			// the query text, because that is all the indexer understands.
			name:        "series without season/ep support falls back to SxxEyy in the text",
			caps:        &models.TorznabCaps{TVParams: []string{"q", "imdbid"}},
			contentType: "series",
			contentID:   "tt1839578:5:14",
			wantTitleT:  tn.SearchTypeTV,
			wantTitleQ:  "The Matrix S05E14",
		},
		{
			name:           "series with season/ep support sends them as parameters",
			caps:           &models.TorznabCaps{TVParams: []string{"q", "imdbid", "season", "ep"}},
			contentType:    "series",
			contentID:      "tt1839578:5:14",
			wantIDQuery:    true,
			wantIDType:     tn.SearchTypeTV,
			wantTitleT:     tn.SearchTypeTV,
			wantTitleQ:     "The Matrix",
			wantStructured: true,
		},
		{
			// The real-world shape that started this: rutracker advertises
			// tv-search with q,season,ep and no imdbid, so everything runs
			// through the title query — which must still carry the episode.
			name:           "no imdbid but season/ep: title query carries the episode",
			caps:           &models.TorznabCaps{TVParams: []string{"q", "season", "ep"}},
			contentType:    "series",
			contentID:      "tt1839578:5:14",
			wantTitleT:     tn.SearchTypeTV,
			wantTitleQ:     "The Matrix",
			wantStructured: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStream(t, "https://example.com/torznab", tt.caps, titles)
			imdbID, season, ep := parseContentID(tt.contentID)

			idQuery := s.buildIDQuery(tt.contentType, imdbID, season, ep)
			if tt.wantIDQuery {
				if idQuery == nil {
					t.Fatalf("buildIDQuery() = nil, want a %q query", tt.wantIDType)
				}
				if idQuery.Type != tt.wantIDType {
					t.Errorf("id query type = %q, want %q", idQuery.Type, tt.wantIDType)
				}
				if idQuery.IMDBID != imdbID {
					t.Errorf("id query imdbid = %q, want %q", idQuery.IMDBID, imdbID)
				}
			} else if idQuery != nil {
				t.Errorf("buildIDQuery() = %+v, want nil", idQuery)
			}

			titleQuery := s.buildTitleQuery(context.Background(), tt.contentType, imdbID, season, ep)
			if titleQuery == nil {
				t.Fatalf("buildTitleQuery() = nil, want a %q query", tt.wantTitleT)
			}
			if titleQuery.Type != tt.wantTitleT {
				t.Errorf("title query type = %q, want %q", titleQuery.Type, tt.wantTitleT)
			}
			if titleQuery.Q != tt.wantTitleQ {
				t.Errorf("title query q = %q, want %q", titleQuery.Q, tt.wantTitleQ)
			}
			hasStructured := titleQuery.Season != nil && titleQuery.Episode != nil
			if hasStructured != tt.wantStructured {
				t.Errorf("title query structured season/ep = %v, want %v", hasStructured, tt.wantStructured)
			}
		})
	}
}

func TestTorznabStreamBuildQueriesWithoutTitle(t *testing.T) {
	// When the metadata lookup fails there is no title query to fall back
	// to; the id query must still be attempted rather than the whole
	// indexer dropping out.
	titles := &fakeTitles{err: context.DeadlineExceeded}
	s := newTestStream(t, "https://example.com/torznab", &models.TorznabCaps{MovieParams: []string{"q", "imdbid"}}, titles)

	if q := s.buildIDQuery("movie", "tt0133093", nil, nil); q == nil || q.IMDBID != "tt0133093" {
		t.Errorf("buildIDQuery() = %+v, want the id query", q)
	}
	if q := s.buildTitleQuery(context.Background(), "movie", "tt0133093", nil, nil); q != nil {
		t.Errorf("buildTitleQuery() = %+v, want nil when the title cannot be resolved", q)
	}
}

func TestParseContentID(t *testing.T) {
	for _, tt := range []struct {
		in         string
		wantID     string
		wantSeason *int
		wantEp     *int
	}{
		{in: "tt0133093", wantID: "tt0133093"},
		{in: "tt1839578:5:14", wantID: "tt1839578", wantSeason: intp(5), wantEp: intp(14)},
		// Library items carry internal ids no external indexer can resolve.
		{in: "wt-6f1b0f3c-0000-0000-0000-000000000000", wantID: ""},
		{in: "", wantID: ""},
		{in: "tt1839578:x:14", wantID: "tt1839578"},
	} {
		id, season, ep := parseContentID(tt.in)
		if id != tt.wantID {
			t.Errorf("parseContentID(%q) id = %q, want %q", tt.in, id, tt.wantID)
		}
		if (season == nil) != (tt.wantSeason == nil) || (season != nil && *season != *tt.wantSeason) {
			t.Errorf("parseContentID(%q) season = %v, want %v", tt.in, season, tt.wantSeason)
		}
		if (ep == nil) != (tt.wantEp == nil) || (ep != nil && *ep != *tt.wantEp) {
			t.Errorf("parseContentID(%q) episode = %v, want %v", tt.in, ep, tt.wantEp)
		}
	}
}

func TestTorznabStreamSkipsLibraryIDs(t *testing.T) {
	// A library id must never reach the indexer: it would be a wasted
	// request, and a search for "wt-<uuid>" returns junk at best.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("indexer was queried with a library id")
		_, _ = w.Write([]byte(torznabFeed))
	}))
	defer srv.Close()

	s := newTestStream(t, srv.URL, nil, &fakeTitles{title: &tn.Title{Name: "x"}})
	resp, err := s.GetStreams(context.Background(), "movie", "wt-6f1b0f3c-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}
	if len(resp.Streams) != 0 {
		t.Errorf("got %d streams, want 0", len(resp.Streams))
	}
}

// TestTorznabStreamNameSurvivesResolutionFilter is the seam test between the
// two halves of the feature: TorznabStream decides what goes into Name, and
// PreferredStream — written for addon streams — parses the resolution back
// out of it. A name shape that looks fine on its own but buckets as "other"
// would silently drop every indexer result for users who disabled "other",
// and nothing in either unit test would notice.
func TestTorznabStreamNameSurvivesResolutionFilter(t *testing.T) {
	s := newTestStream(t, "https://example.com/torznab", nil, &fakeTitles{title: &tn.Title{Name: "x"}})

	for _, tt := range []struct {
		release string
		want    string
	}{
		{"Person.of.Interest.S05E14.1080p.WEB.rus.eng", "1080p"},
		{"The.Matrix.1999.2160p.UHD.BluRay", "4k"},
		{"The.Matrix.1999.720p.BluRay.x264", "720p"},
		{"Some.Release.Without.A.Resolution.Token", "other"},
	} {
		item := StreamItem{
			Name:  s.makeStreamName(tn.Result{Title: tt.release, Tracker: "rutracker"}),
			Title: s.makeStreamTitle(tn.Result{Title: tt.release, Seeders: 10}),
		}
		ps := NewPreferredStream(nil, nil, nil, nil)
		got, err := ps.filterByPreferredResolutions([]StreamItem{item},
			[]models.ResolutionSetting{{Resolution: tt.want, Enabled: true}})
		if err != nil {
			t.Fatalf("filterByPreferredResolutions() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("release %q did not survive a filter that keeps only %q (name was %q)",
				tt.release, tt.want, item.Name)
			continue
		}
		// And the negative control: with that bucket disabled it must go.
		got, err = ps.filterByPreferredResolutions([]StreamItem{item},
			[]models.ResolutionSetting{{Resolution: tt.want, Enabled: false}})
		if err != nil {
			t.Fatalf("filterByPreferredResolutions() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("release %q survived with bucket %q disabled — it is not being bucketed there at all",
				tt.release, tt.want)
		}
	}
}

// TestTorznabStreamsCarryNoFileIndex is the guard for the playback bug this
// feature would otherwise ship: a Torznab result names a torrent, not a file
// inside it, so the stream must not claim file 0. Index 0 is a real index for
// every other source, which is why the "unknown" case needs its own flag —
// and why /stremio/resolve leaves the index out of the token and picks the
// file at click time instead.
func TestTorznabStreamsCarryNoFileIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(torznabFeed))
	}))
	defer srv.Close()

	s := newTestStream(t, srv.URL, nil, &fakeTitles{title: &tn.Title{Name: "x"}})
	resp, err := s.GetStreams(context.Background(), "movie", "tt0133093")
	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}
	if len(resp.Streams) == 0 {
		t.Fatal("got no streams")
	}
	for _, st := range resp.Streams {
		if !st.FileIdxUnknown {
			t.Errorf("stream %q claims to know its file index", st.Title)
		}
	}
}

// TestEnrichOmitsUnknownFileIndexFromToken checks the other half: the
// playback token must not carry idx=0 for those streams, because the resolve
// handler cannot tell a deliberate 0 from a default one.
func TestEnrichOmitsUnknownFileIndexFromToken(t *testing.T) {
	es := NewEnrichStream(nil, nil, nil, nil, "https://webtor.io", "tok", "secret")

	for _, tt := range []struct {
		name     string
		stream   StreamItem
		wantIdx  bool
		wantIdxV float64
	}{
		{
			name:     "a source that knows its file keeps the claim",
			stream:   StreamItem{InfoHash: "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d", FileIdx: 3},
			wantIdx:  true,
			wantIdxV: 3,
		},
		{
			name:    "an indexer stream carries no index",
			stream:  StreamItem{InfoHash: "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d", FileIdxUnknown: true},
			wantIdx: false,
		},
		{
			// The case the flag exists for: file 0 of a torrent is a real
			// answer, so a bare 0 must still be sent.
			name:     "file zero is still a real index",
			stream:   StreamItem{InfoHash: "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d", FileIdx: 0},
			wantIdx:  true,
			wantIdxV: 0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			url := es.generateRedirectURL(&tt.stream, "tt0133093")
			parts := strings.Split(url, "/")
			tokenStr := parts[len(parts)-1]
			token, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
			if err != nil {
				t.Fatalf("failed to parse the minted token: %v", err)
			}
			claims := token.Claims.(jwt.MapClaims)
			raw, ok := claims["idx"]
			if ok != tt.wantIdx {
				t.Fatalf("idx present = %v, want %v (claims: %v)", ok, tt.wantIdx, claims)
			}
			if ok && raw.(float64) != tt.wantIdxV {
				t.Errorf("idx = %v, want %v", raw, tt.wantIdxV)
			}
		})
	}
}

// TestTorznabStreamBoundsTorrentDownloads: a feed that omits both infohash
// and magnet forces a .torrent download per result, at a URL the feed
// chooses. Without a budget one stream request becomes MaxResults fetches of
// up to 8 MiB from a target the user's own indexer picks.
func TestTorznabStreamBoundsTorrentDownloads(t *testing.T) {
	var mu sync.Mutex
	var downloads int
	dl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		downloads++
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	}))
	defer dl.Close()

	var feed strings.Builder
	feed.WriteString(`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>`)
	for i := 0; i < 30; i++ {
		feed.WriteString(`<item><title>Release.` + strconv.Itoa(i) + `.1080p</title><link>` + dl.URL + `/t` + strconv.Itoa(i) + `</link></item>`)
	}
	feed.WriteString(`</channel></rss>`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed.String()))
	}))
	defer srv.Close()

	s := newTestStream(t, srv.URL, nil, &fakeTitles{title: &tn.Title{Name: "x"}})
	if _, err := s.GetStreams(context.Background(), "movie", "tt0133093"); err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if downloads > maxHashDownloads {
		t.Errorf("made %d downloads for one query, want at most %d", downloads, maxHashDownloads)
	}
	if downloads == 0 {
		t.Error("made no downloads at all — the budget is not the thing being measured")
	}
}

func TestMakeStreamNameDoesNotRepeatTheTracker(t *testing.T) {
	// The indexer label already ends with the tracker id when the feed URL
	// names one, and an aggregate feed's results name theirs per item. Both
	// have to end up with exactly one tracker in the label.
	perTracker := NewTorznabStream(nil, models.TorznabIndexer{
		Name: strp("Jackett"),
		Url:  "https://jackett.example.com/api/v2.0/indexers/rutracker-ru/results/torznab",
	}, nil, nil)
	if got := perTracker.makeStreamName(tn.Result{Title: "Release.1080p", Tracker: "rutracker-ru"}); got != "Jackett · rutracker-ru\n1080p" {
		t.Errorf("name = %q, want the tracker exactly once", got)
	}

	aggregate := NewTorznabStream(nil, models.TorznabIndexer{
		Name: strp("Jackett"),
		Url:  "https://jackett.example.com/api/v2.0/indexers/all/results/torznab",
	}, nil, nil)
	if got := aggregate.makeStreamName(tn.Result{Title: "Release.1080p", Tracker: "nnmclub"}); got != "Jackett · nnmclub\n1080p" {
		t.Errorf("name = %q, want the per-result tracker appended", got)
	}
}

// TestEnrichCarriesTheEpisodeForIndexerStreams: an indexer answers an
// episode query with season packs as often as with single episodes, so the
// token has to say which episode was asked for — otherwise /stremio/resolve
// can only fall back to the largest file, i.e. episode one of the pack.
func TestEnrichCarriesTheEpisodeForIndexerStreams(t *testing.T) {
	es := NewEnrichStream(nil, nil, nil, nil, "https://webtor.io", "tok", "secret")
	hash := "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d"

	claimsFor := func(stream StreamItem, contentID string) jwt.MapClaims {
		t.Helper()
		url := es.generateRedirectURL(&stream, contentID)
		parts := strings.Split(url, "/")
		token, _, err := jwt.NewParser().ParseUnverified(parts[len(parts)-1], jwt.MapClaims{})
		if err != nil {
			t.Fatalf("failed to parse the minted token: %v", err)
		}
		return token.Claims.(jwt.MapClaims)
	}

	c := claimsFor(StreamItem{InfoHash: hash, FileIdxUnknown: true}, "tt0903747:1:5")
	if c["s"] != float64(1) || c["e"] != float64(5) {
		t.Errorf("claims = %v, want season 1 episode 5", c)
	}

	// A movie has no episode to carry.
	c = claimsFor(StreamItem{InfoHash: hash, FileIdxUnknown: true}, "tt0133093")
	if _, ok := c["s"]; ok {
		t.Errorf("claims = %v, want no episode for a movie", c)
	}

	// A source that named its file needs neither: idx alone is exact.
	c = claimsFor(StreamItem{InfoHash: hash, FileIdx: 3}, "tt0903747:1:5")
	if _, ok := c["s"]; ok {
		t.Errorf("claims = %v, want no episode when the file index is known", c)
	}
	if c["idx"] != float64(3) {
		t.Errorf("claims = %v, want idx 3", c)
	}
}

// TestTorznabStreamDropsOtherSeasons: an indexer whose caps advertise
// season/ep may still ignore them and answer with whatever the keywords
// matched — which is how a request for season 3 comes back as season 1.
func TestTorznabStreamDropsOtherSeasons(t *testing.T) {
	feed := `<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
	  <item><title>Укрытие / Silo / Сезон: 1 / Серии: 1-10 из 10 [2023 WEB-DL 1080p]</title>
	    <link>magnet:?xt=urn:btih:aaaabbbbccccddddeeeeffff0000111122223333</link>
	    <torznab:attr name="seeders" value="100"/></item>
	  <item><title>Укрытие / Silo / Сезон: 3 / Серии: 1-4 из 10 [2026 WEB-DL 1080p]</title>
	    <link>magnet:?xt=urn:btih:bbbbccccddddeeeeffff00001111222233334444</link>
	    <torznab:attr name="seeders" value="10"/></item>
	  <item><title>Silo 1080p WEB-DL</title>
	    <link>magnet:?xt=urn:btih:ccccddddeeeeffff000011112222333344445555</link>
	    <torznab:attr name="seeders" value="5"/></item>
	</channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()

	caps := &models.TorznabCaps{TVParams: []string{"q", "season", "ep"}}
	s := newTestStream(t, srv.URL, caps, &fakeTitles{title: &tn.Title{Name: "Silo"}})

	resp, err := s.GetStreams(context.Background(), "series", "tt14688458:3:1")
	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}
	titles := make([]string, 0, len(resp.Streams))
	for _, st := range resp.Streams {
		titles = append(titles, strings.Split(st.Title, "\n")[0])
	}
	if len(titles) != 2 {
		t.Fatalf("got %d streams (%v), want 2: season 3 and the untitled one", len(titles), titles)
	}
	for _, tl := range titles {
		if strings.Contains(tl, "Сезон: 1") {
			t.Errorf("a season 1 release survived a season 3 request: %q", tl)
		}
	}
}
