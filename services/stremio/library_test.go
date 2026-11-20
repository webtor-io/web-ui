package stremio

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/urfave/cli"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/services/api"
)

func TestHumanizeBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1503238553, "1.40 GB"}, // ~1.4 GiB
		{12 * 1024 * 1024 * 1024, "12.00 GB"},
		{int64(13_217_000_000), "12.31 GB"},
		{150 * 1024 * 1024, "150 MB"}, // ≥100 → no decimals
	}
	for _, c := range cases {
		if got := humanizeBytes(c.n); got != c.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestLanguageFlags(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"The.Big.Bang.Theory.S05E14.1080p.BluRay.Rus.Eng.TeamHD.mkv", "🇷🇺 / 🇬🇧"}, // first-seen order
		{"Project.Hail.Mary.2026.1080p.WEB-DL.mkv", ""},                           // no lang token
		{"Some.Movie.RUS.RUS.mkv", "🇷🇺"},                                          // de-duplicated
		{"Film.Eng.Ita.Fre.mkv", "🇬🇧 / 🇮🇹 / 🇫🇷"},
	}
	for _, c := range cases {
		if got := languageFlags(c.name); got != c.want {
			t.Errorf("languageFlags(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func i16(v int16) *int16 { return &v }
func i64(v int64) *int64 { return &v }

func TestMakeStreamTitle(t *testing.T) {
	cases := []struct {
		name       string
		displayTtl string
		ct         string
		args       *Args
		md         map[string]any
		size       *int64
		filename   string
		year       *int16
		want       string
	}{
		{
			name:       "series full attributes",
			displayTtl: "The Big Bang Theory",
			ct:         "series",
			args:       &Args{Season: 5, Episode: 14},
			md:         map[string]any{"year": float64(2012), "quality": "BluRay", "resolution": "1080p"},
			size:       i64(1503238553),
			filename:   "The.Big.Bang.Theory.S05E14.1080p.BluRay.Rus.Eng.TeamHD.mkv",
			want:       "The Big Bang Theory · S05E14 [2012 BluRay 1080p]\n💾 1.40 GB  ⚙️ Library\n🇷🇺 / 🇬🇧",
		},
		{
			name:       "movie full attributes",
			displayTtl: "Project Hail Mary",
			ct:         "movie",
			args:       &Args{},
			md:         map[string]any{"quality": "WEB-DL", "resolution": "1080p"},
			size:       i64(int64(13_217_000_000)),
			filename:   "Project.Hail.Mary.2026.1080p.WEB-DL.Rus.Eng.Ger.mkv",
			year:       i16(2026),
			want:       "Project Hail Mary [2026 WEB-DL 1080p]\n💾 12.31 GB  ⚙️ Library\n🇷🇺 / 🇬🇧 / 🇩🇪",
		},
		{
			name:       "no metadata at all",
			displayTtl: "Mystery File",
			ct:         "movie",
			args:       &Args{},
			md:         nil,
			size:       nil,
			filename:   "mystery.mkv",
			want:       "Mystery File\n⚙️ Library",
		},
		{
			name:       "resolution only, no size, no lang",
			displayTtl: "Some Show",
			ct:         "series",
			args:       &Args{Season: 1, Episode: 3},
			md:         map[string]any{"resolution": "720p"},
			size:       nil,
			filename:   "some.show.s01e03.720p.mkv",
			want:       "Some Show · S01E03 [720p]\n⚙️ Library",
		},
		{
			name:       "year from content fallback when absent in md",
			displayTtl: "Old Movie",
			ct:         "movie",
			args:       &Args{},
			md:         map[string]any{"quality": "BluRay"},
			size:       i64(700 * 1024 * 1024),
			filename:   "old.movie.bluray.mkv",
			year:       i16(1999),
			want:       "Old Movie [1999 BluRay]\n💾 700 MB  ⚙️ Library",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := makeStreamTitle(c.displayTtl, c.ct, c.args, c.md, c.size, c.filename, c.year)
			if got != c.want {
				t.Errorf("makeStreamTitle:\n got = %q\nwant = %q", got, c.want)
			}
		})
	}
}

// When FileIdx was persisted at enrich time, resolveFileItem must return it
// verbatim with the filename derived from the path basename — and must NOT
// touch rest-api (sapi is nil here, so any list call would panic). This is
// the fast path that removes per-stream rest-api round-trips from /stream.
func TestLibrary_resolveFileItem_PersistedIdxSkipsRestAPI(t *testing.T) {
	l := NewLibrary("https://webtor.io", nil, nil, nil, nil)

	idx := 100
	p := "/The.Big.Bang.Theory/S05/The.Big.Bang.Theory.S05E14.1080p.mkv"

	gotIdx, gotName, err := l.resolveFileItem(context.Background(), "deadbeef", p, &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIdx != 100 {
		t.Errorf("idx = %d, want 100", gotIdx)
	}
	if gotName != "The.Big.Bang.Theory.S05E14.1080p.mkv" {
		t.Errorf("name = %q, want basename of path", gotName)
	}
}

// FileIdx == 0 is a valid index (the first file in the torrent), not an
// "unset" sentinel — a non-nil pointer to 0 must still take the fast path.
func TestLibrary_resolveFileItem_ZeroIdxIsValid(t *testing.T) {
	l := NewLibrary("https://webtor.io", nil, nil, nil, nil)

	idx := 0
	gotIdx, gotName, err := l.resolveFileItem(context.Background(), "deadbeef", "/movie.mkv", &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotIdx != 0 {
		t.Errorf("idx = %d, want 0", gotIdx)
	}
	if gotName != "movie.mkv" {
		t.Errorf("name = %q, want movie.mkv", gotName)
	}
}

// Other addons' IDs reach our meta and stream handlers too. Only a numeric
// tail is a season/episode; everything else is a whole series ID the library
// does not have — reading "series" from "tmdb:series:76747" as a season was a
// 500 on ~130 meta requests a day.
func TestParseLibraryID(t *testing.T) {
	cases := []struct {
		ct, id string
		want   Args
	}{
		{"movie", "tt0133093", Args{ID: "tt0133093"}},
		{"movie", "tt0133093:1:2", Args{ID: "tt0133093:1:2"}},
		{"series", "tt0944947", Args{ID: "tt0944947"}},
		{"series", "tt0944947:1:2", Args{ID: "tt0944947", Season: 1, Episode: 2}},
		{"series", "kp306084:3:10", Args{ID: "kp306084", Season: 3, Episode: 10}},
		{"series", "tmdb1399:2:5", Args{ID: "tmdb1399", Season: 2, Episode: 5}},
		{"series", "wt-5b0c1c7e-2a4f-4bd3-9e7a-6f1b2c3d4e5f:1:1", Args{ID: "wt-5b0c1c7e-2a4f-4bd3-9e7a-6f1b2c3d4e5f", Season: 1, Episode: 1}},
		// Foreign IDs: whole series IDs, never an error.
		{"series", "tmdb:series:76747", Args{ID: "tmdb:series:76747"}},
		{"series", "mx:2226969025052033872:batman-caped-crusader-Sxx7RwAxvE2", Args{ID: "mx:2226969025052033872:batman-caped-crusader-Sxx7RwAxvE2"}},
		// A foreign episode ID keeps its own colons in the series part.
		{"series", "tmdb:series:76747:1:3", Args{ID: "tmdb:series:76747", Season: 1, Episode: 3}},
		// Kitsu episodes are "kitsu:<id>:<episode>" with no season; the
		// numeric tail reads as season/episode and the series part is "kitsu"
		// — no library item has that ID, so the answer is "not found", which
		// is right: the library never holds Kitsu IDs.
		{"series", "kitsu:12345:7", Args{ID: "kitsu", Season: 12345, Episode: 7}},
	}
	for _, c := range cases {
		got := parseLibraryID(c.ct, c.id)
		if *got != c.want {
			t.Errorf("%s %q: got %+v, want %+v", c.ct, c.id, *got, c.want)
		}
	}
}

// An upload's files change order without changing size or count (its live
// copy lists the directory walk, the Vault copy sorts by path), so a stored
// index may name another file: an upload's file is found by its path in the
// current listing, and a path that is not there gives no stream.
func TestLibrary_resolveFileItem_UploadGoesByPath(t *testing.T) {
	const id = "9c5db5752b7c0123456789abcdef0123456789ab"
	zero := 0
	two := []ra.ListItem{
		{Type: ra.ListTypeFile, MediaFormat: ra.Video, Name: "a.mkv", PathStr: "/Name/a.mkv", Index: 0},
		{Type: ra.ListTypeFile, MediaFormat: ra.Video, Name: "b.mkv", PathStr: "/Name/b.mkv", Index: 1},
	}
	listing := func(t *testing.T, page func(n int) (int, []byte)) (*api.Api, func() int) {
		t.Helper()
		var mu sync.Mutex
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/resource/"+id+"/list" {
				t.Errorf("unexpected %v %v", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusTeapot)
				return
			}
			mu.Lock()
			requests++
			n := requests
			mu.Unlock()
			status, body := page(n)
			w.WriteHeader(status)
			_, _ = w.Write(body)
		}))
		t.Cleanup(srv.Close)
		u, _ := url.Parse(srv.URL)
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		for _, f := range api.RegisterFlags(nil) {
			f.Apply(fs)
		}
		if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
			"--webtor-secret=test", "--rapidapi-host="}); err != nil {
			t.Fatal(err)
		}
		return api.New(cli.NewContext(cli.NewApp(), fs, nil), srv.Client()), func() int {
			mu.Lock()
			defer mu.Unlock()
			return requests
		}
	}
	marshal := func(items []ra.ListItem, count int) []byte {
		b, err := json.Marshal(ra.ListResponse{Items: items, Count: count})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	ctx := context.Background()

	t.Run("the stored index is not used", func(t *testing.T) {
		a, requests := listing(t, func(int) (int, []byte) { return http.StatusOK, marshal(two, 2) })
		l := NewLibrary("https://example.com", nil, nil, a, api.InternalClaims())
		idx, name, err := l.resolveFileItem(ctx, id, "/Name/b.mkv", &zero)
		if err != nil || idx != 1 || name != "b.mkv" {
			t.Errorf("got %d, %q, %v; want 1, b.mkv, nil", idx, name, err)
		}
		if n := requests(); n != 1 {
			t.Errorf("%d list requests, want 1", n)
		}
	})

	t.Run("a path that is gone gives no stream", func(t *testing.T) {
		a, _ := listing(t, func(int) (int, []byte) { return http.StatusOK, marshal(two, 2) })
		l := NewLibrary("https://example.com", nil, nil, a, api.InternalClaims())
		_, name, err := l.resolveFileItem(ctx, id, "/Name/gone.mkv", &zero)
		if err != nil || name != "" {
			t.Errorf("got %q, %v; want no name, no error", name, err)
		}
	})

	t.Run("the listing shrinks while it is walked", func(t *testing.T) {
		page := make([]ra.ListItem, 100)
		for i := range page {
			page[i] = ra.ListItem{Type: ra.ListTypeFile, Name: fmt.Sprintf("f%d.mkv", i), PathStr: fmt.Sprintf("/Name/f%d.mkv", i), Index: i}
		}
		a, requests := listing(t, func(n int) (int, []byte) {
			switch {
			case n == 1:
				return http.StatusOK, marshal(page, 250)
			case n <= 3:
				return http.StatusOK, marshal(nil, 50)
			}
			return http.StatusInternalServerError, nil
		})
		l := NewLibrary("https://example.com", nil, nil, a, api.InternalClaims())
		_, name, err := l.resolveFileItem(ctx, id, "/Name/gone.mkv", &zero)
		if err != nil || name != "" {
			t.Errorf("got %q, %v; want no name, no error", name, err)
		}
		if n := requests(); n != 2 {
			t.Errorf("%d list requests, want 2", n)
		}
	})
}
