package torznab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testClient() *Client {
	// httptest listens on 127.0.0.1, which the default egress guard blocks
	// on purpose — see TestPrivateNetworkGuard for the negative control.
	return NewWithOptions(Options{AllowPrivateNetwork: true, Timeout: 5 * time.Second})
}

func intPtr(i int) *int { return &i }

func TestQueryFetchRequest(t *testing.T) {
	for _, tt := range []struct {
		name     string
		query    Query
		wantVals map[string]string
		wantGone []string
	}{
		{
			name:  "movie search by imdb id drops the tt prefix",
			query: Query{Type: SearchTypeMovie, IMDBID: "tt0133093"},
			wantVals: map[string]string{
				"t":      "movie",
				"imdbid": "0133093",
			},
			wantGone: []string{"q"},
		},
		{
			name:  "tv search carries season, episode and category",
			query: Query{Type: SearchTypeTV, IMDBID: "tt0898266", Season: intPtr(5), Episode: intPtr(14), Cats: []int{5000}},
			wantVals: map[string]string{
				"t":      "tvsearch",
				"imdbid": "0898266",
				"season": "5",
				"ep":     "14",
				"cat":    "5000",
			},
		},
		{
			name:  "plain search carries only the query",
			query: Query{Type: SearchTypeSearch, Q: "the matrix 1999"},
			wantVals: map[string]string{
				"t": "search",
				"q": "the matrix 1999",
			},
			wantGone: []string{"imdbid", "season", "ep"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fr, err := tt.query.fetchRequest()
			if err != nil {
				t.Fatalf("fetchRequest() error = %v", err)
			}
			v, err := fr.Values()
			if err != nil {
				t.Fatalf("Values() error = %v", err)
			}
			for k, want := range tt.wantVals {
				if got := v.Get(k); got != want {
					t.Errorf("param %s = %q, want %q (all: %v)", k, got, want, v)
				}
			}
			for _, k := range tt.wantGone {
				if v.Has(k) {
					t.Errorf("param %s should not be set (all: %v)", k, v)
				}
			}
		})
	}
}

func TestQueryFetchRequestRejectsUnknownType(t *testing.T) {
	if _, err := (Query{Type: "caps"}).fetchRequest(); err == nil {
		t.Error("fetchRequest() accepted a search type the stream layer never issues")
	}
}

func TestSearchRejectsNonHTTPEndpoint(t *testing.T) {
	c := testClient()
	for _, raw := range []string{"ftp://example.com/feed", "file:///etc/passwd", "://nope"} {
		if _, err := c.Search(context.Background(), Endpoint{URL: raw}, Query{Type: SearchTypeSearch, Q: "x"}); err == nil {
			t.Errorf("Search(%q) succeeded, want error", raw)
		}
	}
}

const jackettFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
  <item>
    <title>The.Matrix.1999.1080p.BluRay.x264</title>
    <guid>https://jackett.example.com/api/v2.0/indexers/rutracker/results/torznab/123</guid>
    <link>https://jackett.example.com/dl/rutracker/?jackett_apikey=k&amp;path=abc</link>
    <pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate>
    <size>2147483648</size>
    <torznab:attr name="seeders" value="120"/>
    <torznab:attr name="peers" value="135"/>
    <torznab:attr name="infohash" value="8C4ADBF9EBDC2C31E4B3D01A9E9C5C0F2A1B3C4D"/>
    <torznab:attr name="category" value="2040"/>
  </item>
  <item>
    <title>The.Matrix.1999.720p.WEB</title>
    <link>magnet:?xt=urn:btih:aaaabbbbccccddddeeeeffff0000111122223333&amp;dn=x</link>
    <enclosure url="magnet:?xt=urn:btih:aaaabbbbccccddddeeeeffff0000111122223333" type="application/x-bittorrent" length="734003200"/>
    <newznab:attr xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/" name="seeders" value="7"/>
  </item>
</channel>
</rss>`

func TestSearchParsesFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(jackettFeed))
	}))
	defer srv.Close()

	c := testClient()
	results, err := c.Search(context.Background(), Endpoint{URL: srv.URL}, Query{Type: SearchTypeSearch, Q: "matrix"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// Sorted by seeders, so the 120-seeder item comes first.
	first := results[0]
	if first.Seeders != 120 || first.Peers != 135 {
		t.Errorf("swarm stats = %d/%d, want 120/135", first.Seeders, first.Peers)
	}
	// The library hands the attribute through verbatim; case normalisation
	// (and the base32 spelling) is ResolveInfoHash's job, which is what
	// every downstream consumer actually reads. See TestNormalizeInfoHash.
	if !strings.EqualFold(first.InfoHash, "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d") {
		t.Errorf("infohash = %q, want the feed's value", first.InfoHash)
	}
	if got, err := testClient().ResolveInfoHash(context.Background(), &first); err != nil || got != "8c4adbf9ebdc2c31e4b3d01a9e9c5c0f2a1b3c4d" {
		t.Errorf("ResolveInfoHash() = %q, %v; want the lowercased hex", got, err)
	}
	if first.Size != 2147483648 {
		t.Errorf("size = %d, want 2147483648", first.Size)
	}
	// Tracker carries what the feed said about itself and nothing else. This
	// one tags no <jackettindexer>, and inventing a name from the item's URL
	// would be indistinguishable from a real one to the layer that stores it
	// as the indexer's display name.
	if first.Tracker != "" {
		t.Errorf("tracker = %q, want empty: the feed named none", first.Tracker)
	}
	if first.PublishDate.IsZero() {
		t.Error("pubDate was not parsed")
	}

	// The second item exercises the newznab namespace spelling of attr,
	// the magnet-in-link case, and size from the enclosure length.
	second := results[1]
	if second.Seeders != 7 {
		t.Errorf("newznab:attr seeders = %d, want 7", second.Seeders)
	}
	if second.MagnetURI == "" {
		t.Error("magnet link was not picked up from <link>")
	}
	if second.Size != 734003200 {
		t.Errorf("size = %d, want the enclosure length 734003200", second.Size)
	}
}

func TestSearchSurfacesInBandError(t *testing.T) {
	// Torznab reports a bad API key with HTTP 200 and an error document.
	// Unless that is detected it is indistinguishable from "no results" —
	// go-jackett reads the error attributes off the root element.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><error code="100" description="Incorrect user credentials"/>`))
	}))
	defer srv.Close()

	_, err := testClient().Search(context.Background(), Endpoint{URL: srv.URL}, Query{Type: SearchTypeSearch, Q: "x"})
	if err == nil {
		t.Fatal("Search() succeeded on an error document, want error")
	}
	if !strings.Contains(err.Error(), "Incorrect user credentials") {
		t.Errorf("error = %v, want the indexer's description", err)
	}
}

func TestSearchCapsResultsAtMaxResults(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>`)
	for i := 0; i < 10; i++ {
		sb.WriteString(`<item><title>rel</title><link>magnet:?xt=urn:btih:aaaabbbbccccddddeeeeffff0000111122223333</link></item>`)
	}
	sb.WriteString(`</channel></rss>`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sb.String()))
	}))
	defer srv.Close()

	c := NewWithOptions(Options{AllowPrivateNetwork: true, MaxResults: 3})
	results, err := c.Search(context.Background(), Endpoint{URL: srv.URL}, Query{Type: SearchTypeSearch, Q: "x"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want the 3 the client was capped at", len(results))
	}
}

func TestPrivateNetworkGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(jackettFeed))
	}))
	defer srv.Close()

	// Negative control for the dialer guard: the very same request that
	// succeeds in the tests above must fail with the default settings,
	// because httptest is on loopback.
	c := NewWithOptions(Options{})
	if _, err := c.Search(context.Background(), Endpoint{URL: srv.URL}, Query{Type: SearchTypeSearch, Q: "x"}); err == nil {
		t.Fatal("Search() reached a loopback address with the guard enabled")
	}
}

func TestIsPrivateIP(t *testing.T) {
	for _, tt := range []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"192.168.1.5", true},
		{"172.16.0.1", true},
		{"169.254.169.254", true}, // cloud metadata
		{"::1", true},
		{"fd00::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	} {
		if got := isPrivateIP(mustParseIP(t, tt.ip)); got != tt.want {
			t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestSearchSendsConfiguredUserAgent(t *testing.T) {
	// Trackers throttle by user agent, so the flag has to reach the wire.
	// It travels through go-jackett, which otherwise sets its own.
	var mu sync.Mutex
	var agent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agent = r.UserAgent()
		mu.Unlock()
		_, _ = w.Write([]byte(jackettFeed))
	}))
	defer srv.Close()

	c := NewWithOptions(Options{AllowPrivateNetwork: true, UserAgent: "webtor-test-agent"})
	if _, err := c.Search(context.Background(), Endpoint{URL: srv.URL}, Query{Type: SearchTypeSearch, Q: "x"}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if agent != "webtor-test-agent" {
		t.Errorf("user agent = %q, want the configured one", agent)
	}
}

func TestIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(jackettFeed))
	}))
	defer srv.Close()

	// The real thing: a LAN address blocked by the egress guard. This is
	// what a self-hoster hits when their indexer's hostname resolves to
	// 192.168.x.x, and it must not read as a generic failure.
	_, err := NewWithOptions(Options{}).Caps(context.Background(), Endpoint{URL: srv.URL})
	if err == nil {
		t.Fatal("Caps() reached a loopback address with the guard enabled")
	}
	if !IsUnreachable(err) {
		t.Errorf("IsUnreachable(%v) = false, want true", err)
	}

	// An indexer that answers and rejects the query is a different
	// problem with a different fix, and must not be classified as
	// unreachable.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<error code="100" description="Incorrect user credentials"/>`))
	}))
	defer bad.Close()
	_, err = testClient().Caps(context.Background(), Endpoint{URL: bad.URL})
	if err == nil {
		t.Fatal("Caps() accepted an error document")
	}
	if IsUnreachable(err) {
		t.Errorf("IsUnreachable(%v) = true, want false — the indexer answered", err)
	}

	if IsUnreachable(nil) {
		t.Error("IsUnreachable(nil) = true")
	}
}

func TestIsPrivateIPBlocksTheRangesGoDoesNot(t *testing.T) {
	// Go's IsPrivate covers only RFC1918/RFC4193; these are the ranges an
	// SSRF target actually hides in.
	for _, ip := range []string{
		"127.0.0.1", "10.1.2.3", "192.168.1.5", "172.16.0.1",
		"169.254.169.254", // cloud metadata
		"100.64.0.1",      // CGNAT
		"100.100.100.200", // Alibaba metadata
		"192.0.0.192", "198.18.0.1", "240.0.0.1",
		"239.1.1.1", // multicast
		"::1", "fd00::1", "::", "0.0.0.0",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1",
		"2002:7f00:1::",   // 6to4 wrapping 127.0.0.1
		"64:ff9b::7f00:1", // NAT64 wrapping 127.0.0.1
	} {
		if !isPrivateIP(mustParseIP(t, ip)) {
			t.Errorf("isPrivateIP(%s) = false, want true", ip)
		}
	}
	// And the control: ordinary public addresses must still be reachable,
	// or the feature blocks every real indexer.
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "46.160.255.197", "2606:4700:4700::1111"} {
		if isPrivateIP(mustParseIP(t, ip)) {
			t.Errorf("isPrivateIP(%s) = true, want false", ip)
		}
	}
}

func TestSearchClampsImplausibleSwarmCounts(t *testing.T) {
	// seeders="-1" means "unknown" in several feeds and parses into
	// 2^64-1, which sorts above every real result and, at 30 such items,
	// evicts them all before the cap.
	feed := `<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
	  <item><title>Unknown.Seeders</title><link>magnet:?xt=urn:btih:aaaabbbbccccddddeeeeffff0000111122223333</link>
	    <torznab:attr name="seeders" value="-1"/></item>
	  <item><title>Real.Release.1080p</title><link>magnet:?xt=urn:btih:bbbbccccddddeeeeffff00001111222233334444</link>
	    <torznab:attr name="seeders" value="5000"/></item>
	</channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()

	results, err := testClient().Search(context.Background(), Endpoint{URL: srv.URL}, Query{Type: SearchTypeSearch, Q: "x"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Real.Release.1080p" {
		t.Errorf("first result = %q, want the one with real seeders", results[0].Title)
	}
	if results[1].Seeders != 0 {
		t.Errorf("clamped seeders = %d, want 0", results[1].Seeders)
	}
}

func TestProxiedClientStillRefusesPrivateTargets(t *testing.T) {
	// With a proxy the dialer only ever sees the proxy, so the egress guard
	// stops covering the target. The URL check has to take over, or
	// configuring a proxy would quietly turn the add form into an SSRF
	// primitive again.
	c := NewWithOptions(Options{Proxy: "socks5://127.0.0.1:1080"})
	_, err := c.Caps(context.Background(), Endpoint{URL: "http://localhost:9117/torznab"})
	if err == nil {
		t.Fatal("Caps() accepted a loopback target through a proxy")
	}
	if !strings.Contains(err.Error(), "private address") {
		t.Errorf("error = %v, want the private-address refusal", err)
	}

	// Self-hosted deployments opt out of the guard as a whole.
	c = NewWithOptions(Options{Proxy: "socks5://127.0.0.1:1080", AllowPrivateNetwork: true})
	if err := c.validateEndpointURL("http://localhost:9117/torznab"); err != nil {
		t.Errorf("validateEndpointURL() = %v, want nil when private networks are allowed", err)
	}
}
