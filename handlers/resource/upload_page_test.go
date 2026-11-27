package resource

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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

	"github.com/webtor-io/web-ui/handlers/index"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/uploads"
	"github.com/webtor-io/web-ui/services/vault"
	w "github.com/webtor-io/web-ui/services/web"
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

// fakePics records the IDs Erase is called with and answers err.
type fakePics struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (p *fakePics) Erase(_ context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, id)
	return p.err
}

func (p *fakePics) called() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

// A frame may show a file that is no longer in the upload: the owner's page
// erases the upload's pictures when its files changed, and only then.
func TestUploadPageErasesStalePictures(t *testing.T) {
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	ctx := context.Background()
	u := newUploadPageUser(t, db)
	user := &models.User{UserID: u}
	srv := newUploadPageAPI(t, "Example Movie")
	old := log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
	hook := logtest.NewGlobal()
	out := log.StandardLogger().Out
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		log.StandardLogger().ReplaceHooks(old)
		log.SetOutput(out)
	})

	const mib int64 = 1 << 20
	another := "/Example Movie/Another.Title.2008.mkv"
	cat := &fakeMapper{}
	pics := &fakePics{}
	page := func(t *testing.T, id string, pics uploads.PictureEraser) {
		t.Helper()
		en := enrichEnv(t, srv.Server, pgCS, []enrich.MetadataMapper{cat})
		getUploadPage(t, newResourceRouterPics(t, srv.Server, nil, user, pgCS, en, pics), id)
	}
	wantCalls := func(t *testing.T, want ...string) {
		t.Helper()
		if got := pics.called(); !slices.Equal(got, want) {
			t.Errorf("Erase calls %v, want %v", got, want)
		}
	}
	if err := models.CreateUpload(ctx, db, u, uploadU, "Example Movie"); err != nil {
		t.Fatal(err)
	}

	t.Run("files came", func(t *testing.T) {
		srv.set(uploadU, videoItem("/Example Movie/Example.Movie.2015.1080p.mkv", 0, 700*mib))
		page(t, uploadU, pics)
		wantCalls(t, uploadU)
	})
	t.Run("the same page again", func(t *testing.T) {
		page(t, uploadU, pics)
		wantCalls(t, uploadU)
	})
	t.Run("the file replaced", func(t *testing.T) {
		srv.set(uploadU, videoItem(another, 0, 900*mib))
		page(t, uploadU, pics)
		wantCalls(t, uploadU, uploadU)
		var paths []string
		if _, err := db.Query(&paths, `SELECT path FROM movie WHERE resource_id = ?`, uploadU); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(paths, []string{another}) {
			t.Errorf("movie rows of %v, want the new file only", paths)
		}
	})
	t.Run("the bucket fails", func(t *testing.T) {
		srv.set(uploadU, videoItem(another, 0, 901*mib))
		pics.mu.Lock()
		pics.err = errors.New("s3 down")
		pics.mu.Unlock()
		srv.enrichment()
		hook.Reset()
		page(t, uploadU, pics)
		warned := false
		for _, e := range hook.AllEntries() {
			warned = warned || e.Level == log.WarnLevel && e.Message == "failed to erase the upload's stale pictures"
		}
		if !warned {
			t.Error("no Warn failed to erase the upload's stale pictures in the log")
		}
		if len(srv.enrichment()) == 0 {
			t.Error("no enrichment request: the run did not start")
		}
	})
	t.Run("no bucket", func(t *testing.T) {
		id := uploadID(2)
		if err := models.CreateUpload(ctx, db, u, id, "Example Movie"); err != nil {
			t.Fatal(err)
		}
		srv.set(id, videoItem("/Example Movie/Example.Movie.2015.1080p.mkv", 0, 700*mib))
		page(t, id, nil)
	})
	t.Run("a torrent's page", func(t *testing.T) {
		before := len(pics.called())
		srv.mu.Lock()
		srv.public[torrentT] = true
		srv.mu.Unlock()
		srv.set(torrentT, videoItem("/Example Movie/Example.Movie.2015.1080p.mkv", 0, 700*mib))
		page(t, torrentT, pics)
		if n := len(pics.called()); n != before {
			t.Errorf("%d Erase calls on a torrent's page", n-before)
		}
	})
}

// A stranger and an anonymous visitor are refused an upload in its own words:
// no .torrent of theirs opens it, and no Vault copy plays for them. A private
// torrent's refusal is as before.
func TestUploadRefusalPage(t *testing.T) {
	user := &models.User{UserID: uuid.NewV4()}
	refused := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d, want 403 (Location %q)", rec.Code, rec.Header().Get("Location"))
		}
		if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("Cache-Control %q, want private, no-store", got)
		}
		if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
			t.Errorf("X-Robots-Tag %q, want noindex", got)
		}
		return rec.Body.String()
	}
	get := func(r http.Handler, id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+id, nil))
		return rec
	}

	t.Run("anonymous", func(t *testing.T) {
		srv, answer := privateRestAPI(t, uploadU)
		answer(http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"sign_in"}`)
		body := refused(t, get(newResourceRouterAs(t, srv, nil, nil), uploadU))
		if want := "[private][key=error.upload_private_sign_in][signin=true][upload=false][vault=false][target=main]"; body != want {
			t.Errorf("body %q, want %q", body, want)
		}
	})

	t.Run("signed in, kept in Vault", func(t *testing.T) {
		db := pgtest.Start(t)
		v := vault.New(cli.NewContext(cli.NewApp(), flag.NewFlagSet("v", flag.ContinueOnError), nil), &vault.Api{}, nil, nil, pgtest.CS(t, db), nil)
		for _, c := range []struct{ id, want string }{
			{uploadU, "[key=error.upload_private_no_record][signin=false][upload=false][vault=false]"},
			{torrentT, "[key=error.private_no_record][signin=false][upload=true][vault=true]"},
		} {
			if _, err := db.Model(&vaultModels.Resource{ResourceID: c.id, RequiredVP: 1, Name: "probe", Vaulted: true}).Insert(); err != nil {
				t.Fatal(err)
			}
			srv, answer := privateRestAPI(t, c.id)
			answer(http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"no_record"}`)
			if body := refused(t, get(newResourceRouterAs(t, srv, v, user), c.id)); !strings.Contains(body, c.want) {
				t.Errorf("%s: body %q, want %s", c.id, body, c.want)
			}
		}
	})

	// The card says upload, not torrent, in its heading and the page title.
	t.Run("the card", func(t *testing.T) {
		funcs := intentFuncs(t)
		tpl, err := template.New("private.html").Funcs(funcs).ParseFiles(
			"../../templates/views/resource/private.html",
			"../../templates/partials/private/refusal.html",
		)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		r := w.NewPrivateRefusal(&api.PrivateError{Status: 403, Code: api.CodePrivateTorrent, Reason: api.ReasonNoRecord}, true, uploadU)
		r.Target = "main"
		ctx := &w.Context{Lang: "en", Path: "/" + uploadU, Data: r}
		if title := strings.TrimSpace(execute(t, tpl, "title", ctx)); title != "Private files" {
			t.Errorf("title %q, want Private files", title)
		}
		out := execute(t, tpl, "main", ctx)
		if !strings.Contains(out, ">Private files</h3>") {
			t.Errorf("no Private files heading:\n%s", out)
		}
		// rest-api's code rides along as the event's property; the words
		// say upload.
		if text := strings.ReplaceAll(out, `="private_torrent"`, ""); strings.Contains(strings.ToLower(text), "torrent") {
			t.Errorf("an upload's card names a torrent:\n%s", out)
		}
	})
}

// seedTraces gives id what Forget erases: its enrichment rows, dropped first
// so that a row can follow one that kept them.
func seedTraces(t *testing.T, db *pg.DB, id string) {
	t.Helper()
	for _, q := range []string{
		`DELETE FROM media_info WHERE resource_id = ?0`,
		`DELETE FROM ai_enrich.query WHERE resource_id = ?0`,
		`INSERT INTO media_info (resource_id, status) VALUES (?0, ?1)`,
		`INSERT INTO movie (resource_id, title, path) VALUES (?0, 'n', '/a.mkv')`,
		`INSERT INTO ai_enrich.query (parsed_title, content_type, resource_id, hint_path) VALUES (?0, 1, ?0, '/a.mkv')`,
	} {
		if _, err := db.Exec(q, id, models.MediaInfoStatusDone); err != nil {
			t.Fatal(err)
		}
	}
}

// rowsOf counts the rows of id in each table.
func rowsOf(t *testing.T, db *pg.DB, id string, tables ...string) map[string]int {
	t.Helper()
	n := map[string]int{}
	for _, table := range tables {
		var c int
		if _, err := db.QueryOne(pg.Scan(&c), `SELECT count(*) FROM `+table+` WHERE resource_id = ?`, id); err != nil {
			t.Fatal(err)
		}
		n[table] = c
	}
	return n
}

// The owner of an upload rest-api no longer has is told it is gone, on the
// home page's 404, and the page erases what can come back -- its enrichment
// and pictures -- but not the owner's watch history or subtitle bindings:
// rest-api's 404 or no_record may be wrong while the upload's home node is
// away. Nobody else sets that off.
func TestUploadGonePage(t *testing.T) {
	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	ctx := context.Background()
	owner, stranger := newUploadPageUser(t, db), newUploadPageUser(t, db)
	for _, id := range []string{uploadU, torrentT} {
		if err := models.CreateUpload(ctx, db, owner, id, "n"); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{
			`INSERT INTO watch_history (user_id, resource_id, path) VALUES (?, ?, '/a.mkv')`,
			`INSERT INTO user_subtitle (user_id, resource_id, path, hash, original_name, format, size) VALUES (?, ?, '/a.mkv', 'h', 'a.srt', 'srt', 1)`,
		} {
			if _, err := db.Exec(q, owner, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	old := log.StandardLogger().ReplaceHooks(make(log.LevelHooks))
	hook := logtest.NewGlobal()
	out := log.StandardLogger().Out
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		log.StandardLogger().ReplaceHooks(old)
		log.SetOutput(out)
	})

	const (
		notFound = `{"error":"not found"}`
		noRecord = `{"error":"x","code":"private_torrent","reason":"no_record"}`
	)
	traces := []string{"media_info", "movie", "ai_enrich.query"}
	kept := []string{"watch_history", "user_subtitle", "library", "torrent_resource"}
	for _, c := range []struct {
		name    string
		viewer  *uuid.UUID
		id      string
		status  int
		body    string
		picsErr error
		code    int
		want    string
		erased  bool
	}{
		{"owner, 404", &owner, uploadU, http.StatusNotFound, notFound, nil, http.StatusNotFound, "[home][err=upload.gone]", true},
		{"owner, 403 no_record", &owner, uploadU, http.StatusForbidden, noRecord, nil, http.StatusNotFound, "[home][err=upload.gone]", true},
		{"stranger, 404", &stranger, uploadU, http.StatusNotFound, notFound, nil, http.StatusNotFound, "[home][err=error.not_found]", false},
		{"stranger, 403 no_record", &stranger, uploadU, http.StatusForbidden, noRecord, nil, http.StatusForbidden, "[private][key=error.upload_private_no_record]", false},
		{"anonymous, 404", nil, uploadU, http.StatusNotFound, notFound, nil, http.StatusNotFound, "[home][err=error.not_found]", false},
		{"owner, 404, a torrent", &owner, torrentT, http.StatusNotFound, notFound, nil, http.StatusNotFound, "[home][err=error.not_found]", false},
		{"owner, 404, the bucket fails", &owner, uploadU, http.StatusNotFound, notFound, errors.New("s3 down"), http.StatusNotFound, "[home][err=upload.gone]", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			seedTraces(t, db, c.id)
			srv, answer := privateRestAPI(t, c.id)
			answer(c.status, c.body)
			var user *models.User
			if c.viewer != nil {
				user = &models.User{UserID: *c.viewer}
			}
			pics := &fakePics{err: c.picsErr}
			hook.Reset()
			rec := httptest.NewRecorder()
			newResourceRouterPics(t, srv, nil, user, pgCS, nil, pics).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+c.id, nil))
			if rec.Code != c.code || !strings.HasPrefix(rec.Body.String(), c.want) {
				t.Errorf("%d %q, want %d %s", rec.Code, rec.Body.String(), c.code, c.want)
			}
			if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Errorf("Cache-Control %q, want private, no-store", got)
			}
			for table, n := range rowsOf(t, db, c.id, traces...) {
				if (n == 0) != c.erased {
					t.Errorf("%s: %d rows, want erased %v", table, n, c.erased)
				}
			}
			if c.viewer == &owner {
				for table, n := range rowsOf(t, db, c.id, kept...) {
					if n == 0 {
						t.Errorf("%s: no rows, want the owner's kept", table)
					}
				}
			}
			var wantCalls []string
			if c.erased {
				wantCalls = []string{c.id}
			}
			if got := pics.called(); !slices.Equal(got, wantCalls) {
				t.Errorf("Erase calls %v, want %v", got, wantCalls)
			}
			warned := false
			for _, e := range hook.AllEntries() {
				warned = warned || e.Level == log.WarnLevel && e.Message == "failed to forget an upload that is gone"
			}
			if warned != (c.picsErr != nil) {
				t.Errorf("Warn failed to forget an upload that is gone: %v, want %v", warned, c.picsErr != nil)
			}
		})
	}
}

// The owner shown "this upload is gone" is counted, by the impression the
// refusal card uses, with no properties of its own; no other home page error
// is.
func TestUploadGoneImpression(t *testing.T) {
	funcs := intentFuncs(t)
	funcs["asset"] = func(p string) template.HTML { return template.HTML("[asset " + p + "]") }
	tpl, err := template.New("index.html").Funcs(funcs).ParseFiles(
		"../../templates/views/index.html",
		"../../templates/partials/load/progress.html",
		"../../templates/partials/job_log.html",
		"../../templates/partials/about/heading.html",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stubTemplates(t, tpl, "hero_backdrop", "hero_wave", "onboarding_checklist", "continue_watching", "discover", "promo", "about", "private/refusal", "upload/log")
	const (
		event  = `upload-gone-shown`
		script = `[asset private_refusal.js]`
	)
	render := func(key string) string {
		return execute(t, tpl, "main", &w.Context{Lang: "en", Data: &index.Data{}, ErrKey: key})
	}
	block := func(t *testing.T, main string) (tag, block string) {
		t.Helper()
		tag = openTag(t, main, `id="index-error"`)
		i := strings.Index(main, tag)
		return tag, main[i : i+strings.Index(main[i:], "</div>")]
	}

	main := render("upload.gone")
	tag, b := block(t, main)
	if !strings.Contains(tag, `data-umami-impression="`+event+`"`) {
		t.Errorf("no impression on the block: %s", tag)
	}
	if strings.Contains(tag, "data-umami-impression-") {
		t.Errorf("the impression has properties: %s", tag)
	}
	if !strings.Contains(b, "These files are gone:") {
		t.Errorf("no text in the block:\n%s", b)
	}
	if n := strings.Count(b, script); n != 1 {
		t.Errorf("%d private_refusal.js in the block, want 1:\n%s", n, b)
	}
	if n := strings.Count(main, event); n != 1 {
		t.Errorf("the event is declared %d times, want once", n)
	}

	main = render("error.not_found")
	block(t, main)
	for _, s := range []string{event, script} {
		if strings.Contains(main, s) {
			t.Errorf("not found: %q is there", s)
		}
	}

	main = render("")
	for _, s := range []string{"index-error", event} {
		if strings.Contains(main, s) {
			t.Errorf("no error: %q is there", s)
		}
	}
}

// between is s from the first from to the first to after it, or "".
func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], to)
	if j < 0 {
		return s[i:]
	}
	return s[i : i+j]
}

// The owner's page of an upload says upload, not torrent, and has none of a
// torrent's tails: no magnet, no .torrent, no archive, no tracker notice, no
// Share. Its state line says saved only by a funded pledge, says nothing when
// Vault could not be read, and tells an unfinished upload how to resume.
func TestUploadPageTemplate(t *testing.T) {
	upload := func(multi bool, up *UploadView) *GetData {
		gd := intentPage("", multi)
		gd.Args.ID, gd.Resource.ID = uploadU, uploadU
		gd.Resource.MagnetURI, gd.Resource.Private = "", true
		gd.Upload = up
		return gd
	}
	torrent := func(multi, private bool) *GetData {
		gd := intentPage("", multi)
		gd.Args.ID, gd.Resource.ID = torrentT, torrentT
		gd.Resource.MagnetURI, gd.Resource.Private = "magnet:?xt=urn:btih:"+torrentT, private
		return gd
	}
	owner := func() *UploadView { return &UploadView{IsOwner: true, CanWrite: true} }
	render := func(t *testing.T, gd *GetData) string {
		t.Helper()
		return execute(t, resourceTemplates(t), "main", pageContext("en", gd))
	}
	enrich := func(gd *GetData) *GetData {
		gd.Movie = &models.Movie{
			VideoContent:  &models.VideoContent{ResourceID: gd.Resource.ID, Title: "Sample Movie"},
			MovieMetadata: &models.MovieMetadata{VideoMetadata: &models.VideoMetadata{VideoID: "tt0000001", Title: "Sample Movie"}},
		}
		return gd
	}

	t.Run("an upload", func(t *testing.T) {
		page := render(t, upload(false, owner()))
		badge := between(page, "badge badge-lg", "</div>")
		if !strings.Contains(badge, "My files") || strings.Contains(badge, "Torrent") {
			t.Errorf("badge %q, want My files", badge)
		}
		checkPage(t, page, nil, []string{`data-magnet-hash`, `download-torrent`, "Webtor uses your tracker account"})
		checkPage(t, render(t, upload(true, owner())), nil, []string{`js-archive-form`, `data-magnet-hash`})
		checkPage(t, render(t, torrent(true, false)), []string{`js-archive-form`, `data-magnet-hash`}, nil)
	})

	t.Run("states", func(t *testing.T) {
		var (
			frozen     = html.EscapeString("Saved to Vault — files can no longer be added or deleted.")
			unsaved    = html.EscapeString("Not saved to Vault: the files may be deleted. You can't add files here any more.")
			temporary  = html.EscapeString("Temporary: files that are not played or downloaded may be deleted within hours.")
			hint       = html.EscapeString("Save them to Vault.")
			incomplete = html.EscapeString("Not every file has finished uploading.")
		)
		for _, c := range []struct {
			name    string
			up      *UploadView
			vault   bool
			in, out []string
		}{
			{"saved", &UploadView{IsOwner: true, Saved: true}, true, []string{frozen}, []string{unsaved, temporary}},
			{"a row without a pledge", &UploadView{IsOwner: true}, true, []string{unsaved, hint}, []string{frozen, temporary}},
			{"temporary", owner(), true, []string{temporary, hint}, []string{frozen, unsaved}},
			{"temporary, no Vault", owner(), false, []string{temporary}, []string{hint}},
			{"unfinished", &UploadView{IsOwner: true, CanWrite: true, Incomplete: true}, true, []string{incomplete}, []string{temporary, hint, frozen, unsaved}},
			{"saved before unfinished", &UploadView{IsOwner: true, Saved: true, Incomplete: true}, true, []string{frozen}, []string{incomplete}},
			{"Vault not read", &UploadView{IsOwner: true, VaultUnknown: true}, true, nil, []string{frozen, unsaved, temporary, hint, incomplete}},
			{"unknown before saved", &UploadView{IsOwner: true, Saved: true, VaultUnknown: true}, true, nil, []string{frozen}},
			{"an unpaid pledge of the owner's", &UploadView{IsOwner: true, UnpaidPledge: true}, true, []string{unsaved}, []string{hint, frozen, temporary}},
		} {
			t.Run(c.name, func(t *testing.T) {
				gd := upload(false, c.up)
				gd.Resource.FilesCount, gd.Vault = 2, c.vault
				page := render(t, gd)
				var line string
				if c.in != nil {
					line = between(page, "data-upload-state", "</p>")
					if line == "" {
						t.Fatal("no state line")
					}
				} else if strings.Contains(page, "data-upload-state") {
					t.Errorf("a state line: %s", between(page, "data-upload-state", "</p>"))
				}
				checkPage(t, line, c.in, nil)
				checkPage(t, page, nil, c.out)
			})
		}
	})

	t.Run("empty", func(t *testing.T) {
		gd := upload(true, &UploadView{IsOwner: true, CanWrite: true, Incomplete: true})
		gd.Resource.FilesCount, gd.List, gd.Item = 0, nil, nil
		page := render(t, gd)
		checkPage(t, between(page, "data-upload-state", "</p>"), []string{html.EscapeString("Not every file has finished uploading.")}, nil)
		checkPage(t, page, nil, []string{html.EscapeString("Temporary:"), `js-archive-form`, `data-magnet-hash`})
	})

	t.Run("share", func(t *testing.T) {
		for _, enriched := range []bool{false, true} {
			for _, c := range []struct {
				name  string
				gd    *GetData
				share bool
			}{
				{"an upload", upload(false, owner()), false},
				{"a torrent", torrent(false, false), true},
				{"a private torrent", torrent(false, true), true},
			} {
				gd := c.gd
				if enriched {
					gd = enrich(gd)
				}
				if got := strings.Contains(render(t, gd), "shareResource("); got != c.share {
					t.Errorf("%s, enriched %v: Share %v, want %v", c.name, enriched, got, c.share)
				}
			}
		}
	})
}

// After the pledge only the modal and the Vault button are rendered again:
// the modal's script takes Add files and the × off the owner's page and says
// saved. It runs again in the same document on the next upload saved, so it
// declares nothing. A torrent's modal has none of it.
func TestUploadPledgeSuccessUpdatesThePage(t *testing.T) {
	tpl := vaultModal(t)
	out := renderModal(t, tpl, "en", &GetData{VaultPledgeAddForm: &VaultPledgeAddForm{Status: "success", ResourceID: uploadU, FreezePeriod: freezePeriod}})
	checkPage(t, out, []string{`[data-upload-add], [data-upload-add] + input, [data-upload-delete]`, `[data-upload-state]`, `Saved to Vault — files can no longer`}, nil)
	if i := strings.Index(out, "[data-upload-state]"); i >= 0 {
		script := between(out[strings.LastIndex(out[:i], "<script>"):], "<script>", "</script>")
		checkPage(t, script, nil, []string{"const ", "let "})
	}

	out = renderModal(t, tpl, "en", &GetData{VaultPledgeAddForm: &VaultPledgeAddForm{Status: "success", ResourceID: intentHash, FreezePeriod: freezePeriod}})
	checkPage(t, out, nil, []string{`data-upload-add`})
}

func TestTrackerPrivate(t *testing.T) {
	for _, c := range []struct {
		r    *ra.ResourceResponse
		want bool
	}{
		{&ra.ResourceResponse{ID: uploadU, Private: true}, false},
		{&ra.ResourceResponse{ID: torrentT, Private: true}, true},
		{&ra.ResourceResponse{ID: kind1, Private: false}, false},
	} {
		if got := trackerPrivate(c.r); got != c.want {
			t.Errorf("%s private=%v: %v, want %v", c.r.ID, c.r.Private, got, c.want)
		}
	}
}

// An upload is in My files from the start and leaves the library only as a
// whole: its page has no library button.
func TestLibraryButtonHiddenForAnUpload(t *testing.T) {
	tpl, err := template.New("button.html").Funcs(intentFuncs(t)).ParseFiles("../../templates/partials/library/button.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx := &w.Context{Lang: "en", User: &auth.User{ID: uuid.NewV4()}}
	render := func(id string) string {
		return strings.TrimSpace(execute(t, tpl, "library/button", &w.CtxData{Ctx: ctx, Data: &ExtendedResource{ResourceResponse: &ra.ResourceResponse{ID: id}}}))
	}
	if out := render(uploadU); out != "" {
		t.Errorf("an upload: %q, want nothing", out)
	}
	if out := render(torrentT); !strings.Contains(out, "/lib/add") {
		t.Errorf("a torrent: %q, want /lib/add", out)
	}
}
