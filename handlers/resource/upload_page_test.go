package resource

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/golang-jwt/jwt/v5"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
)

const (
	uploadU  = "9c5db5752b7c0123456789abcdef0123456789ab"
	torrentT = "0123456789abcdef0123456789abcdef01234567"
)

func uploadID(n int) string {
	return fmt.Sprintf("9c5db5752b7c0%027x", n)
}

// uploadPageAPI is rest-api for the upload page: /resource/<id> and its /list
// from the files set for that ID. It keeps the claims of every /list request
// whose token verifies with the test secret and is internal: enrichment's.
// The page's own requests are signed by the router's api, which has no
// secret, and do not verify.
type uploadPageAPI struct {
	*httptest.Server
	name     string
	mu       sync.Mutex
	files    map[string][]ra.ListItem
	public   map[string]bool
	internal []jwt.MapClaims
}

func newUploadPageAPI(t *testing.T, name string) *uploadPageAPI {
	t.Helper()
	f := &uploadPageAPI{name: name, files: map[string][]ra.ListItem{}, public: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve(t)))
	t.Cleanup(f.Close)
	return f
}

func (f *uploadPageAPI) serve(t *testing.T) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		id, list := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/resource/"), "/list")
		f.mu.Lock()
		defer f.mu.Unlock()
		items, ok := f.files[id]
		if !ok {
			t.Errorf("unexpected rest-api call %s", r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if list {
			p, err := jwt.Parse(r.Header.Get("X-Token"), func(*jwt.Token) (interface{}, error) { return []byte("test"), nil })
			if err == nil && p.Valid {
				if c := p.Claims.(jwt.MapClaims); c["internal"] == true {
					f.internal = append(f.internal, c)
				}
			}
			b, _ := json.Marshal(ra.ListResponse{Items: items, Count: len(items)})
			_, _ = w.Write(b)
			return
		}
		var size int64
		for _, it := range items {
			size += it.Size
		}
		res := map[string]any{"id": id, "name": f.name, "multi_file": true, "size": size,
			"files_count": len(items), "private": !f.public[id]}
		if !f.public[id] {
			res["reserved"] = size
		}
		b, _ := json.Marshal(res)
		_, _ = w.Write(b)
	}
}

// set gives id these files, and nothing else.
func (f *uploadPageAPI) set(id string, items ...ra.ListItem) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[id] = append([]ra.ListItem{}, items...)
}

// enrichment returns the claims of enrichment's /list requests since the
// last call.
func (f *uploadPageAPI) enrichment() []jwt.MapClaims {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.internal
	f.internal = nil
	return c
}

func videoItem(p string, idx int, size int64) ra.ListItem {
	return ra.ListItem{Type: ra.ListTypeFile, MediaFormat: ra.Video, Name: p[strings.LastIndex(p, "/")+1:], PathStr: p, Index: idx, Size: size}
}

// enrichEnv is an enricher on its own api against the fake rest-api: each
// api keeps a minute's cache of listings, so a shared one would serve a step
// the listing of the step before.
func enrichEnv(t *testing.T, restAPI *httptest.Server, pgCS *cs.PG, mappers []enrich.MetadataMapper) *enrich.Enricher {
	t.Helper()
	u, err := url.Parse(restAPI.URL)
	if err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range api.RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--webtor-rest-api-secure=false", "--webtor-secret=test", "--rapidapi-host=", "--rapidapi-key="}); err != nil {
		t.Fatal(err)
	}
	return enrich.NewEnricher(pgCS, api.New(cli.NewContext(cli.NewApp(), fs, nil), restAPI.Client()), mappers, nil, nil)
}

// fakeMapper is a catalog that knows no title. block, when set, holds every
// lookup until it is closed; panics makes every lookup panic.
type fakeMapper struct {
	calls  atomic.Int32
	block  chan struct{}
	panics bool
}

func (m *fakeMapper) GetName() string { return "fake" }

func (m *fakeMapper) Map(context.Context, *models.VideoContent, models.ContentType, bool) (*models.VideoMetadata, error) {
	m.calls.Add(1)
	if m.panics {
		panic("catalog failure")
	}
	if m.block != nil {
		<-m.block
	}
	return nil, nil
}

func newUploadPageUser(t *testing.T, db *pg.DB) uuid.UUID {
	t.Helper()
	var u uuid.UUID
	if _, err := db.QueryOne(pg.Scan(&u), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	return u
}

func getUploadPage(t *testing.T, h http.Handler, id string) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /%s: status %d, body %q", id, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// The upload answered, so it is there: the owner's page clears the gone mark
// and brings back the library row.
func TestUploadPageClearsGone(t *testing.T) {
	db := pgtest.Start(t)
	u := newUploadPageUser(t, db)
	if _, err := db.Exec(`INSERT INTO upload_reserve (user_id, resource_id, gone_at) VALUES (?, ?, now())`, u, uploadU); err != nil {
		t.Fatal(err)
	}
	// rest-api answers multi_file for an upload always, with no files too.
	srv := newUploadPageAPI(t, "Name")
	srv.set(uploadU)

	getUploadPage(t, newResourceRouterPG(t, srv.Server, nil, &models.User{UserID: u}, pgtest.CS(t, db)), uploadU)

	var gone *time.Time
	if _, err := db.QueryOne(pg.Scan(&gone), `SELECT gone_at FROM upload_reserve WHERE user_id = ? AND resource_id = ?`, u, uploadU); err != nil {
		t.Fatal(err)
	}
	if gone != nil {
		t.Errorf("gone_at = %v, want NULL", gone)
	}
	if in, err := models.IsInLibrary(context.Background(), db, u, uploadU); err != nil || !in {
		t.Errorf("library row: %v, %v; want it there", in, err)
	}
}

// The owner's page detects movies and series in an upload as the load job
// does for a torrent, again whenever the upload's files changed.
func TestUploadPageEnriches(t *testing.T) {
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	ctx := context.Background()
	u := newUploadPageUser(t, db)
	user := &models.User{UserID: u}
	srv := newUploadPageAPI(t, "Example Movie")
	// The hook keeps what is logged; the panic's stack and the warnings this
	// test causes stay out of its output.
	hook := logtest.NewGlobal()
	out := log.StandardLogger().Out
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(out) })

	const mib int64 = 1 << 20
	movie := videoItem("/Example Movie/Example.Movie.2015.1080p.mkv", 0, 700*mib)

	page := func(t *testing.T, id string, mappers ...enrich.MetadataMapper) string {
		t.Helper()
		return getUploadPage(t, newResourceRouterEn(t, srv.Server, nil, user, pgCS, enrichEnv(t, srv.Server, pgCS, mappers)), id)
	}
	create := func(t *testing.T, id string) {
		t.Helper()
		if err := models.CreateUpload(ctx, db, u, id, "Example Movie"); err != nil {
			t.Fatal(err)
		}
	}
	status := func(t *testing.T, id string) (models.MediaInfoStatus, bool) {
		t.Helper()
		var s int16
		res, err := db.Query(pg.Scan(&s), `SELECT status FROM media_info WHERE resource_id = ?`, id)
		if err != nil {
			t.Fatal(err)
		}
		return models.MediaInfoStatus(s), res.RowsReturned() == 1
	}
	wantStatus := func(t *testing.T, id string, want models.MediaInfoStatus) {
		t.Helper()
		if s, ok := status(t, id); !ok || s != want {
			t.Errorf("media_info status = %v (row %v), want %v", s, ok, want)
		}
	}
	// movies maps the upload's movie rows, title to file_idx ("NULL" when
	// unset).
	movies := func(t *testing.T, id string) map[string]string {
		t.Helper()
		var rows []struct{ Title, FileIdx string }
		if _, err := db.Query(&rows, `SELECT title, coalesce(file_idx::text, 'NULL') AS file_idx FROM movie WHERE resource_id = ?`, id); err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, r := range rows {
			m[r.Title] = r.FileIdx
		}
		return m
	}
	wantMovie := func(t *testing.T, body, title string) {
		t.Helper()
		if want := "[movie=" + title + "]"; !strings.Contains(body, want) {
			t.Errorf("body %q, want %s", body, want)
		}
	}
	wantEnrichment := func(t *testing.T, want int) {
		t.Helper()
		if got := len(srv.enrichment()); got != want {
			t.Errorf("%d enrichment requests, want %d", got, want)
		}
	}
	logged := func(level log.Level, prefix string) bool {
		for _, e := range hook.AllEntries() {
			if e.Level == level && strings.HasPrefix(e.Message, prefix) {
				return true
			}
		}
		return false
	}
	create(t, uploadU)
	cat := &fakeMapper{}

	t.Run("an empty upload", func(t *testing.T) {
		srv.set(uploadU)
		wantMovie(t, page(t, uploadU, cat), "")
		wantStatus(t, uploadU, models.MediaInfoStatusNoMedia)
		wantEnrichment(t, 1)
		page(t, uploadU, cat)
		wantEnrichment(t, 0)
	})

	t.Run("files came", func(t *testing.T) {
		srv.set(uploadU, movie)
		wantMovie(t, page(t, uploadU, cat), "Example Movie")
		if m := movies(t, uploadU); len(m) != 1 || m["Example Movie"] != "0" {
			t.Errorf("movie rows %v, want Example Movie at index 0", m)
		}
		wantStatus(t, uploadU, models.MediaInfoStatusDone)
		reqs := srv.enrichment()
		if len(reqs) == 0 {
			t.Error("no enrichment request")
		}
		for _, c := range reqs {
			if _, ok := c["uid"]; ok {
				t.Errorf("enrichment listed the upload as a user: %v", c)
			}
		}
	})

	t.Run("the same page again", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE movie SET file_idx = NULL WHERE resource_id = ?`, uploadU); err != nil {
			t.Fatal(err)
		}
		cat.calls.Store(0)
		page(t, uploadU, cat)
		wantEnrichment(t, 0)
		if n := cat.calls.Load(); n != 0 {
			t.Errorf("%d catalog lookups, want 0", n)
		}
		// Enrich would have written the index back (its backfill).
		if m := movies(t, uploadU); m["Example Movie"] != "NULL" {
			t.Errorf("movie rows %v, want Example Movie with file_idx NULL", m)
		}
	})

	t.Run("a file added", func(t *testing.T) {
		srv.set(uploadU, videoItem("/Example Movie/Another.Title.2008.mkv", 0, 900*mib),
			videoItem("/Example Movie/Example.Movie.2015.1080p.mkv", 1, 700*mib))
		page(t, uploadU, cat)
		m := movies(t, uploadU)
		if len(m) != 2 {
			t.Errorf("movie rows %v, want two", m)
		}
		if m["Example Movie"] != "1" {
			t.Errorf("movie rows %v, want Example Movie at index 1", m)
		}
	})

	t.Run("no video left", func(t *testing.T) {
		srv.set(uploadU, ra.ListItem{Type: ra.ListTypeFile, Name: "readme.txt", PathStr: "/Example Movie/readme.txt", Index: 0, Size: 1024})
		wantMovie(t, page(t, uploadU, cat), "")
		if m := movies(t, uploadU); len(m) != 0 {
			t.Errorf("movie rows %v, want none", m)
		}
		wantStatus(t, uploadU, models.MediaInfoStatusNoMedia)
	})

	t.Run("the video back", func(t *testing.T) {
		srv.set(uploadU, movie)
		wantMovie(t, page(t, uploadU, cat), "Example Movie")
		if m := movies(t, uploadU); len(m) != 1 {
			t.Errorf("movie rows %v, want one", m)
		}
		wantStatus(t, uploadU, models.MediaInfoStatusDone)
	})

	t.Run("a slow catalog", func(t *testing.T) {
		id := uploadID(2)
		create(t, id)
		srv.set(id, movie)
		was := uploadEnrichWait
		uploadEnrichWait = 50 * time.Millisecond
		t.Cleanup(func() { uploadEnrichWait = was })
		slow := &fakeMapper{block: make(chan struct{})}
		time.AfterFunc(2*time.Second, func() { close(slow.block) })

		start := time.Now()
		page(t, id, slow)
		if d := time.Since(start); d >= time.Second {
			t.Errorf("the page took %v, want under 1s", d)
		}
		for deadline := time.Now().Add(4 * time.Second); ; time.Sleep(50 * time.Millisecond) {
			if s, ok := status(t, id); ok && s == models.MediaInfoStatusDone {
				break
			}
			if time.Now().After(deadline) {
				s, ok := status(t, id)
				t.Fatalf("media_info status = %v (row %v), want Done: the run did not outlive the request", s, ok)
			}
		}
	})

	t.Run("a catalog panic", func(t *testing.T) {
		id := uploadID(3)
		create(t, id)
		srv.set(id, movie)
		hook.Reset()
		page(t, id, &fakeMapper{panics: true})
		if !logged(log.ErrorLevel, "upload enrichment panic") {
			t.Error("no Error upload enrichment panic in the log")
		}
	})

	t.Run("no catalogs", func(t *testing.T) {
		id := uploadID(4)
		create(t, id)
		srv.set(id, movie)
		srv.enrichment()
		page(t, id)
		wantEnrichment(t, 0)
		if _, ok := status(t, id); ok {
			t.Error("a media_info row without catalogs")
		}
		if m := movies(t, id); len(m) != 0 {
			t.Errorf("movie rows %v without catalogs", m)
		}
		if in, err := models.IsInLibrary(ctx, db, u, id); err != nil || !in {
			t.Errorf("library row: %v, %v; want it there", in, err)
		}
	})

	t.Run("a torrent's page", func(t *testing.T) {
		srv.mu.Lock()
		srv.public[torrentT] = true
		srv.mu.Unlock()
		srv.set(torrentT, movie)
		srv.enrichment()
		page(t, torrentT, cat)
		wantEnrichment(t, 0)
		if in, err := models.IsInLibrary(ctx, db, u, torrentT); err != nil || in {
			t.Errorf("library row: %v, %v; want none", in, err)
		}
		if _, ok := status(t, torrentT); ok {
			t.Error("a media_info row for the torrent")
		}
	})

	t.Run("the library row not written", func(t *testing.T) {
		id := uploadID(5)
		create(t, id)
		srv.set(id, movie)
		if _, err := db.Exec(`ALTER TABLE upload_reserve RENAME TO upload_reserve_off`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := db.Exec(`ALTER TABLE upload_reserve_off RENAME TO upload_reserve`); err != nil {
				t.Error(err)
			}
		})
		srv.enrichment()
		hook.Reset()
		page(t, id, cat)
		if !logged(log.WarnLevel, "failed to keep the upload's library row") {
			t.Error("no Warn failed to keep the upload's library row in the log")
		}
		wantEnrichment(t, 0)
		if _, ok := status(t, id); ok {
			t.Error("a media_info row after the library row failed")
		}
	})
}
