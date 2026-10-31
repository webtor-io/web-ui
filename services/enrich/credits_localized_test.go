package enrich

import (
	"context"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	tm "github.com/webtor-io/web-ui/models/tmdb"
	"github.com/webtor-io/web-ui/services/tmdb"
)

func TestLocRefresher(t *testing.T) {
	ran := make(chan locKey, 2*locRefreshQueue)
	release := make(chan struct{})
	r := newLocRefresher(func(k locKey) {
		ran <- k
		<-release
	})
	key := func(i int) locKey { return locKey{id: i, typ: tmdb.TmdbTypeMovie, lang: "ru"} }

	// One key in flight per worker, both held there.
	for i := range locRefreshWorkers {
		if !r.enqueue(key(i)) {
			t.Fatalf("key %d not queued", i)
		}
		<-ran
	}
	if r.enqueue(key(0)) {
		t.Error("a key in flight was queued again")
	}
	for i := locRefreshWorkers; i < locRefreshWorkers+locRefreshQueue; i++ {
		if !r.enqueue(key(i)) {
			t.Fatalf("key %d not queued with room in the queue", i)
		}
	}
	if r.enqueue(key(locRefreshWorkers)) {
		t.Error("a queued key was queued again")
	}
	full := key(locRefreshWorkers + locRefreshQueue)
	done := make(chan bool)
	go func() { done <- r.enqueue(full) }()
	select {
	case queued := <-done:
		if queued {
			t.Error("a full queue took one more key")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("enqueue blocked on a full queue")
	}

	close(release)
	seen := map[locKey]int{}
	for range locRefreshQueue {
		seen[<-ran]++
	}
	for k, n := range seen {
		if n != 1 || k.id < locRefreshWorkers || k == full {
			t.Errorf("%+v ran %d times", k, n)
		}
	}
	if len(seen) != locRefreshQueue {
		t.Errorf("%d keys ran after the first ones, want %d", len(seen), locRefreshQueue)
	}
	// Dropped, not remembered: the next read queues it.
	if !r.enqueue(full) {
		t.Error("a dropped key could not be queued later")
	}
	// Done, not remembered either: a key that ran can be queued again.
	for deadline := time.Now().Add(5 * time.Second); !r.enqueue(key(0)); {
		if time.Now().After(deadline) {
			t.Fatal("a key that ran could not be queued again")
		}
		time.Sleep(time.Millisecond)
	}
}

// fakeTMDB answers /3/{movie|tv}/{id} with what details returns (nil: 404),
// or 500 while fail is set, and records the calls.
type fakeTMDB struct {
	mu      sync.Mutex
	queries []string
	fail    bool
	details func(path string) map[string]any
}

func (f *fakeTMDB) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

// tmdbOnPostgres is a TMDB mapper on a disposable Postgres (with this repo's
// migrations) and a fake TMDB API. Its refresher runs no workers, so what a
// call queues stays in refresher.jobs for the test to read.
func tmdbOnPostgres(t *testing.T, fake *fakeTMDB) (*TMDB, *pg.DB) {
	t.Helper()
	db := pgtest.Start(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.queries = append(fake.queries, r.URL.Path+"?"+r.URL.Query().Encode())
		fail := fake.fail
		fake.mu.Unlock()
		if fail {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		var body map[string]any
		if fake.details != nil {
			body = fake.details(r.URL.Path + "?" + r.URL.Query().Get("language"))
		}
		if body == nil {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	apiHost, apiPort, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	o := db.Options()
	pgHost, pgPort, _ := net.SplitHostPort(o.Addr)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range append(tmdb.RegisterFlags(nil), cs.RegisterPGFlags(nil)...) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{
		"--tmdb-api-host", apiHost, "--tmdb-api-port", apiPort, "--tmdb-api-secure=false", "--tmdb-api-key", "k",
		"--postgres-host", pgHost, "--postgres-port", pgPort, "--postgres-user", o.User,
		"--postgres-password", o.Password, "--postgres-database", o.Database,
	}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	csPG := cs.NewPG(c)
	t.Cleanup(csPG.Close)
	s := NewTMDB(csPG, tmdb.New(c, srv.Client()))
	s.refresher.once.Do(func() {})
	return s, db
}

func queued(s *TMDB) []locKey {
	var out []locKey
	for len(s.refresher.jobs) > 0 {
		k := <-s.refresher.jobs
		out = append(out, k)
		delete(s.refresher.pending, k)
	}
	return out
}

func seedInfo(t *testing.T, db *pg.DB, id int, typ tm.TmdbType, md map[string]any) {
	t.Helper()
	if _, err := tm.UpsertInfo(context.Background(), db, id, typ, md); err != nil {
		t.Fatal(err)
	}
}

// insertOldLocalized writes a tmdb.localized row as it was before the names
// column: NULL names.
func insertOldLocalized(t *testing.T, db *pg.DB, id int, lang, title string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO tmdb.localized (tmdb_id, lang, title, plot) VALUES (?, ?, ?, 'old plot')`, id, lang, title); err != nil {
		t.Fatal(err)
	}
}

// Credits serves the hash page, whose render must not wait on TMDB: whatever
// the language and whatever tmdb.localized holds, no API call.
func TestCreditsLocalizedNeverCallsTMDB(t *testing.T) {
	fake := &fakeTMDB{}
	s, db := tmdbOnPostgres(t, fake)
	ctx := context.Background()
	movie := func(imdb string) map[string]any {
		return map[string]any{"title": "Movie A", "imdb_id": imdb, "credits": map[string]any{
			"crew": []any{withID(10, person("Director A", "job", "Director"))},
			"cast": []any{withID(20, person("Actor A")), withID(21, person("Actor B"))},
		}}
	}
	seedInfo(t, db, 1, tm.TmdbTypeMovie, movie("tt1"))
	seedInfo(t, db, 2, tm.TmdbTypeSeries, map[string]any{"name": "Series A", "imdb_id": "tt2",
		"created_by": []any{withID(30, person("Creator A"))},
		"credits":    map[string]any{"cast": []any{withID(20, person("Actor A"))}},
	})
	seedInfo(t, db, 3, tm.TmdbTypeMovie, movie("tt3"))
	seedInfo(t, db, 5, tm.TmdbTypeMovie, movie("tt5"))
	seedInfo(t, db, 6, tm.TmdbTypeMovie, map[string]any{"title": "Movie F", "imdb_id": "tt6", "credits": map[string]any{}})
	if _, err := tm.UpsertLocalized(ctx, db, 1, "ru", "Фильм А", "", map[string]string{"10": "Режиссёр А", "20": "Актёр А"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.UpsertLocalized(ctx, db, 1, "pt-BR", "Filme A", "", map[string]string{"20": "Ator A"}); err != nil {
		t.Fatal(err)
	}
	insertOldLocalized(t, db, 2, "ru", "Сериал А")
	if _, err := tm.UpsertLocalized(ctx, db, 5, "ru", "Фильм Д", "", map[string]string{}); err != nil {
		t.Fatal(err)
	}

	english := &Credits{Directors: []string{"Director A"}, Cast: []CastMember{{Name: "Actor A"}, {Name: "Actor B"}}}
	cases := []struct {
		name, id, lang string
		ct             models.ContentType
		want           *Credits
		queued         []locKey
	}{
		{"English", "tt1", "en", models.ContentTypeMovie, english, nil},
		{"no language", "tt1", "", models.ContentTypeMovie, english, nil},
		{"row with names: one by one, English where it has none", "tt1", "ru", models.ContentTypeMovie,
			&Credits{Directors: []string{"Режиссёр А"}, Cast: []CastMember{{Name: "Актёр А"}, {Name: "Actor B"}}}, nil},
		{"pt reads TMDB's pt-BR", "tt1", "pt", models.ContentTypeMovie,
			&Credits{Directors: []string{"Director A"}, Cast: []CastMember{{Name: "Ator A"}, {Name: "Actor B"}}}, nil},
		{"row from before names: English, one refresh", "tt2", "ru", models.ContentTypeSeries,
			&Credits{Creators: []string{"Creator A"}, Cast: []CastMember{{Name: "Actor A"}}},
			[]locKey{{id: 2, typ: tmdb.TmdbTypeTV, lang: "ru"}}},
		{"no row: English, one refresh", "tt3", "ru", models.ContentTypeMovie, english,
			[]locKey{{id: 3, typ: tmdb.TmdbTypeMovie, lang: "ru"}}},
		{"tmdbN of the other type: nothing, no refresh", "tmdb3", "ru", models.ContentTypeSeries, &Credits{}, nil},
		{"row with names '{}': fetched, English, no refresh", "tt5", "ru", models.ContentTypeMovie, english, nil},
		{"nobody to rename: nothing read, no refresh", "tt6", "ru", models.ContentTypeMovie, &Credits{}, nil},
	}
	for _, c := range cases {
		got, err := s.Credits(ctx, c.id, c.ct, c.lang)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
		if q := queued(s); !reflect.DeepEqual(q, c.queued) {
			t.Errorf("%s: queued %+v, want %+v", c.name, q, c.queued)
		}
	}

	// The Enricher hands the language through on both paths.
	en := NewEnricher(nil, nil, []MetadataMapper{s}, nil, nil)
	want := cases[2].want
	if got := en.CachedCredits(ctx, "tt1", models.ContentTypeMovie, "ru"); !reflect.DeepEqual(got, want) {
		t.Errorf("CachedCredits: got %+v", got)
	}
	if got, err := en.CreditsByID(ctx, "tt1", models.ContentTypeMovie, "ru"); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("CreditsByID: got %+v, %v", got, err)
	}

	if calls := fake.calls(); len(calls) != 0 {
		t.Errorf("TMDB called: %v", calls)
	}
}

func TestLocalizeStoresNames(t *testing.T) {
	details := readDetails(t, "localized_movie_ru.json")
	details["title"], details["overview"] = "Фильм А", "Описание А"
	fake := &fakeTMDB{details: func(path string) map[string]any {
		switch path {
		case "/3/movie/1?ru":
			return details
		case "/3/movie/4?ru":
			return map[string]any{"title": "Фильм Б"}
		}
		return nil
	}}
	s, db := tmdbOnPostgres(t, fake)
	ctx := context.Background()
	for id := 1; id <= 5; id++ {
		seedInfo(t, db, id, tm.TmdbTypeMovie, map[string]any{"title": "Movie", "imdb_id": "tt" + strconv.Itoa(id)})
	}
	insertOldLocalized(t, db, 2, "ru", "Старый А")
	insertOldLocalized(t, db, 3, "ru", "Старый Б")

	// A miss: one call, asking for credits, and the row stored with names.
	title, plot, err := s.Localize(ctx, "tt1", "ru")
	if err != nil || title != "Фильм А" || plot != "Описание А" {
		t.Fatalf("got %q, %q, %v", title, plot, err)
	}
	if calls := fake.calls(); len(calls) != 1 || calls[0] != "/3/movie/1?api_key=k&append_to_response=credits&language=ru" {
		t.Errorf("calls %v", calls)
	}
	row, err := tm.GetLocalized(ctx, db, 1, "ru")
	if err != nil || row == nil {
		t.Fatal(row, err)
	}
	if want := localizedNames(details); len(want) != 21 || !reflect.DeepEqual(row.Names, want) {
		t.Errorf("stored names %v, want %v", row.Names, want)
	}
	// A response with no credits stores '{}': fetched, nothing to store.
	if _, _, err := s.Localize(ctx, "tt4", "ru"); err != nil {
		t.Fatal(err)
	}
	if row, err := tm.GetLocalized(ctx, db, 4, "ru"); err != nil || row == nil || row.Names == nil || len(row.Names) != 0 {
		t.Errorf("no credits stored %+v, %v; want names '{}'", row, err)
	}
	// Read back from the DB, '{}' is fetched: nothing queued, no call.
	s.locCache.Drop(locCacheKey(4, "ru"))
	if title, _, err := s.Localize(ctx, "tt4", "ru"); err != nil || title != "Фильм Б" {
		t.Fatalf("got %q, %v", title, err)
	}
	if q := queued(s); len(q) != 0 {
		t.Errorf("Localize on '{}' queued %+v", q)
	}
	// The in-memory copy keeps title and plot only: Credits reads the names
	// from the DB.
	for _, id := range []int{1, 4} {
		v, _ := s.locCache.Get(locCacheKey(id, "ru"), func() (*localizedText, error) {
			t.Errorf("%d: not cached", id)
			return nil, nil
		})
		if v == nil || v.Names != nil {
			t.Errorf("%d: cached %+v, want no names", id, v)
		}
	}

	// Nothing usable: a failed call stores nothing, and a 404 stores a row
	// with no title or plot and names '{}', so it is not asked again.
	fake.mu.Lock()
	fake.fail = true
	fake.mu.Unlock()
	if _, _, err := s.Localize(ctx, "tt5", "ru"); err == nil {
		t.Error("a failed call answered")
	}
	if row, _ := tm.GetLocalized(ctx, db, 5, "ru"); row != nil {
		t.Errorf("a failed call stored %+v", row)
	}
	fake.mu.Lock()
	fake.fail = false
	fake.mu.Unlock()
	for range 2 {
		s.locCache.Drop(locCacheKey(5, "ru"))
		if title, plot, err := s.Localize(ctx, "tt5", "ru"); err != nil || title != "" || plot != "" {
			t.Errorf("a 404: got %q, %q, %v", title, plot, err)
		}
	}
	if row, err := tm.GetLocalized(ctx, db, 5, "ru"); err != nil || row == nil || row.Title != "" || row.Plot != "" || row.Names == nil || len(row.Names) != 0 {
		t.Errorf("a 404 stored %+v, %v; want an empty row with names '{}'", row, err)
	}
	if q := queued(s); len(q) != 0 {
		t.Errorf("queued %+v", q)
	}

	// A row from before names: served as it is, and one refresh queued.
	if title, _, err := s.Localize(ctx, "tt2", "ru"); err != nil || title != "Старый А" {
		t.Fatalf("got %q, %v", title, err)
	}
	if q := queued(s); !reflect.DeepEqual(q, []locKey{{id: 2, typ: tmdb.TmdbTypeMovie, lang: "ru"}}) {
		t.Errorf("queued %+v", q)
	}
	// Nor does it wait on a full queue: the key is dropped.
	for i := range locRefreshQueue {
		s.refresher.enqueue(locKey{id: 100 + i})
	}
	done := make(chan string)
	go func() {
		title, _, _ := s.Localize(ctx, "tt3", "ru")
		done <- title
	}()
	select {
	case title := <-done:
		if title != "Старый Б" {
			t.Errorf("got %q", title)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Localize blocked on a full refresh queue")
	}
	if n := len(fake.calls()); n != 4 {
		t.Errorf("%d TMDB calls, want 4 (three misses, one of them failed and asked again)", n)
	}
}

func TestRefreshLocalized(t *testing.T) {
	details := readDetails(t, "localized_series_ru.json")
	details["name"], details["overview"] = "Новый А", "Новое описание"
	fake := &fakeTMDB{fail: true, details: func(path string) map[string]any {
		if path == "/3/tv/2?ru" {
			return details
		}
		return nil
	}}
	s, db := tmdbOnPostgres(t, fake)
	ctx := context.Background()
	seedInfo(t, db, 2, tm.TmdbTypeSeries, map[string]any{"name": "Series", "imdb_id": "tt2"})
	insertOldLocalized(t, db, 2, "ru", "Старый А")
	for id := 3; id <= 4; id++ {
		seedInfo(t, db, id, tm.TmdbTypeMovie, map[string]any{"title": "Movie", "imdb_id": "tt" + strconv.Itoa(id),
			"credits": map[string]any{"cast": []any{withID(20, person("Actor A"))}}})
	}
	insertOldLocalized(t, db, 4, "ru", "Старый Б")

	// Cached in memory as the old row.
	if title, _, _ := s.Localize(ctx, "tt2", "ru"); title != "Старый А" {
		t.Fatalf("got %q", title)
	}
	queued(s)

	// A failed call leaves the row as it was.
	s.refreshLocalized(locKey{id: 2, typ: tmdb.TmdbTypeTV, lang: "ru"})
	if row, _ := tm.GetLocalized(ctx, db, 2, "ru"); row.Names != nil || row.Title != "Старый А" {
		t.Errorf("after a failure: %+v", row)
	}
	if _, ok := s.locCache.Status(locCacheKey(2, "ru")); !ok {
		t.Error("a failure dropped the in-memory copy")
	}

	fake.mu.Lock()
	fake.fail = false
	fake.mu.Unlock()
	s.refreshLocalized(locKey{id: 2, typ: tmdb.TmdbTypeTV, lang: "ru"})
	row, err := tm.GetLocalized(ctx, db, 2, "ru")
	if err != nil || row.Title != "Новый А" || row.Plot != "Новое описание" || row.Names["66633"] != "Винс Гиллиган" || len(row.Names) != 9 {
		t.Errorf("after the refresh: %+v, %v", row, err)
	}
	// The in-memory copy is gone: the next Localize reads the new row, and
	// that row has names, so nothing more is queued.
	if title, _, _ := s.Localize(ctx, "tt2", "ru"); title != "Новый А" {
		t.Errorf("Localize after the refresh: %q", title)
	}
	if q := queued(s); len(q) != 0 {
		t.Errorf("queued %+v", q)
	}

	// A 404 stores a row with names '{}' (a row already there keeps its
	// title and plot), so the next credits read queues nothing.
	for id := 3; id <= 4; id++ {
		s.refreshLocalized(locKey{id: id, typ: tmdb.TmdbTypeMovie, lang: "ru"})
	}
	for id, want := range map[int]string{3: "", 4: "Старый Б"} {
		if row, err := tm.GetLocalized(ctx, db, id, "ru"); err != nil || row == nil || row.Title != want || row.Names == nil || len(row.Names) != 0 {
			t.Errorf("%d after a 404: %+v, %v; want title %q, names '{}'", id, row, err, want)
		}
		if _, err := s.Credits(ctx, "tt"+strconv.Itoa(id), models.ContentTypeMovie, "ru"); err != nil {
			t.Fatal(err)
		}
	}
	if q := queued(s); len(q) != 0 {
		t.Errorf("credits after a 404 queued %+v", q)
	}
	if n := len(fake.calls()); n != 4 {
		t.Errorf("%d TMDB calls, want 4 (the refreshes)", n)
	}
}
