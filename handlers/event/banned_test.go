package event

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/golang-jwt/jwt/v5"
	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/thumbnail"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
	"github.com/webtor-io/web-ui/services/uploads"
)

const (
	upload0 = "9c5db5752b7c0123456789abcdef0123456789ab" // a user's upload
	upload1 = "9c5db5752b7c1123456789abcdef0123456789ab" // home-hosted, not a user's upload
	torrent = "0123456789abcdef0123456789abcdef01234567"
)

// thpCall is one request the fake proxy got.
type thpCall struct {
	method, path string
	query        url.Values
}

// fakeTHP stands in for torrent-http-proxy and the seeder behind it on
// web-ui's service calls. It answers status, but first, as the seeder does,
// 403 to a token that is not internal, has no expiry or no positive lim.
type fakeTHP struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []thpCall
}

func startTHP(t *testing.T, status int) *fakeTHP {
	t.Helper()
	f := &fakeTHP{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, thpCall{r.Method, r.URL.Path, r.URL.Query()})
		f.mu.Unlock()
		mc := jwt.MapClaims{}
		_, err := jwt.ParseWithClaims(r.URL.Query().Get("token"), mc, func(*jwt.Token) (any, error) { return []byte("test"), nil })
		_, exp := mc["exp"]
		lim, _ := mc["lim"].(float64)
		if err != nil || mc["internal"] != true || !exp || lim <= 0 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTHP) requests() []thpCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// flags is a context of the api and uploads flags. They read the environment
// on Apply: every one that matters is given.
func flags(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, fl := range uploads.RegisterFlags(api.RegisterFlags(nil)) {
		fl.Apply(fs)
	}
	if err := fs.Parse(append([]string{"--webtor-secret=test", "--rapidapi-host=", "--rapidapi-key="}, args...)); err != nil {
		t.Fatal(err)
	}
	return cli.NewContext(cli.NewApp(), fs, nil)
}

// uploadsTo is the uploads service sending to f, and its api.
func uploadsTo(t *testing.T, pgCS *cs.PG, f *fakeTHP) (*uploads.Service, *api.Api) {
	t.Helper()
	c := flags(t, "--upload-url="+f.srv.URL, "--webtor-key=k", "--use-internal-torrent-http-proxy=false")
	a := api.New(c, f.srv.Client())
	s := uploads.New(c, pgCS, a)
	if s == nil {
		t.Fatal("uploads are off")
	}
	return s, a
}

// pictures writes a thumbnail row of id for the content hash h, the blob of
// that content and one render of it.
func pictures(t *testing.T, db *pg.DB, store *thumbtest.Store, id, h string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO thumbnail (resource_id, path, offset_sec, source_kind, hash, format, size) VALUES (?, '/a.mkv', 300, ?, ?, 'jpg', 1)`,
		id, models.ThumbnailSourceFFmpegFrame, h); err != nil {
		t.Fatal(err)
	}
	store.Put("thumbs", h+".jpg", []byte("x"))
	store.Put("renders", thumbnail.RenderPrefix(h)+"240.jpg", []byte("x"))
}

// expectObjects reports the blob and the render of content h where they are
// not there as wanted.
func expectObjects(t *testing.T, store *thumbtest.Store, h string, there bool) {
	t.Helper()
	for _, o := range []struct{ bucket, key string }{
		{"thumbs", h + ".jpg"},
		{"renders", thumbnail.RenderPrefix(h) + "240.jpg"},
	} {
		if got := slices.Contains(store.Keys(o.bucket), o.key); got != there {
			t.Errorf("%s/%s there %v, want %v", o.bucket, o.key, got, there)
		}
	}
}

func rows(t *testing.T, db *pg.DB, table, id string) int {
	t.Helper()
	var n int
	if _, err := db.QueryOne(pg.Scan(&n), `SELECT count(*) FROM `+table+` WHERE resource_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	return n
}

// expectRows reports every table where id's rows are not there as wanted.
func expectRows(t *testing.T, db *pg.DB, id string, there bool, tables ...string) {
	t.Helper()
	for _, table := range tables {
		if n := rows(t, db, table, id); (n > 0) != there {
			t.Errorf("%s of %s: %d rows, want rows %v", table, id, n, there)
		}
	}
}

// fakeAcker records how a message was answered.
type fakeAcker struct{ calls []string }

func (f *fakeAcker) Ack(...nats.AckOpt) error {
	f.calls = append(f.calls, "Ack")
	return nil
}

func (f *fakeAcker) Nak(...nats.AckOpt) error {
	f.calls = append(f.calls, "Nak")
	return nil
}

func (f *fakeAcker) NakWithDelay(d time.Duration, _ ...nats.AckOpt) error {
	f.calls = append(f.calls, "NakWithDelay "+d.String())
	return nil
}

// A ban of an upload deletes its files on the seeder and erases its pictures
// before the rows go; until both are done the ban is retried, not before
// half a minute. A ban of anything else only clears the rows, as before.
func TestResourceBannedUpload(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		for _, c := range []struct {
			err   error
			delay time.Duration
			want  string
		}{
			{errors.New("thp 500"), resourceBannedRetry, "NakWithDelay 30s"},
			{nil, resourceBannedRetry, "Ack"},
			{errors.New("x"), 0, "Nak"},
		} {
			f := &fakeAcker{}
			if err := settle(f, c.err, c.delay); err != nil {
				t.Errorf("settle: %v", err)
			}
			if !slices.Equal(f.calls, []string{c.want}) {
				t.Errorf("error %v, delay %v: calls %v, want [%s]", c.err, c.delay, f.calls, c.want)
			}
		}
	})

	db := pgtest.Start(t)
	pgCS := pgtest.CS(t, db)
	s3, store := thumbtest.Start(t)
	hook := logtest.NewGlobal()
	t.Cleanup(hook.Reset)
	const offWarn = "resource.banned for an upload while uploads are off: its files stay on the seeder"

	for i, c := range []struct {
		name   string
		id     string
		status int
		setup  func(t *testing.T, h *Handler, f *fakeTHP)
		fails  bool
		calls  int
		purged bool // torrent_resource and the thumbnail row are gone
		erased bool // the blob and the render are gone
	}{
		{"upload, deleted", upload0, http.StatusNoContent, nil, false, 1, true, true},
		{"upload, 404 from a foreign hop", upload0, http.StatusNotFound, nil, true, 1, false, false},
		{"upload, evicted", upload0, http.StatusGone, nil, false, 1, true, true},
		{"upload, seeder fails", upload0, http.StatusInternalServerError, nil, true, 1, false, false},
		{"upload, proxy unreachable", upload0, http.StatusNoContent, func(_ *testing.T, _ *Handler, f *fakeTHP) { f.srv.Close() }, true, 0, false, false},
		{"upload, storage fails", upload0, http.StatusNoContent, func(t *testing.T, _ *Handler, _ *fakeTHP) {
			store.Fail(http.MethodDelete)
			t.Cleanup(func() { store.Fail("") })
		}, true, 1, false, false},
		{"home-hosted, not an upload", upload1, http.StatusNoContent, nil, false, 0, true, false},
		{"torrent", torrent, http.StatusNoContent, nil, false, 0, true, false},
		{"uploads off", upload0, http.StatusNoContent, func(_ *testing.T, h *Handler, _ *fakeTHP) { h.up = nil }, true, 0, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, q := range []string{`DELETE FROM torrent_resource WHERE resource_id = ?`, `DELETE FROM thumbnail WHERE resource_id = ?`} {
				if _, err := db.Exec(q, c.id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, 'n', 0, 0)`, c.id); err != nil {
				t.Fatal(err)
			}
			hash := fmt.Sprintf("%040x", i+1) // the case's own content
			pictures(t, db, store, c.id, hash)
			f := startTHP(t, c.status)
			up, a := uploadsTo(t, pgCS, f)
			h := &Handler{pg: pgCS, up: up, thumb: thumbtest.Service(t, s3, pgCS, a)}
			if c.setup != nil {
				c.setup(t, h, f)
			}
			hook.Reset()

			err := h.resourceBanned([]byte(`{"infohash":"` + c.id + `"}`))
			if (err != nil) != c.fails {
				t.Errorf("err = %v, want an error %v", err, c.fails)
			}
			calls := f.requests()
			if len(calls) != c.calls {
				t.Errorf("%d proxy requests, want %d", len(calls), c.calls)
			}
			for _, call := range calls {
				if call.method != http.MethodDelete || call.path != "/"+upload0+"/" || call.query.Get("api-key") != "k" {
					t.Errorf("%s %s api-key %q, want DELETE /%s/ api-key k", call.method, call.path, call.query.Get("api-key"), upload0)
				}
				mc := jwt.MapClaims{}
				if _, err := jwt.ParseWithClaims(call.query.Get("token"), mc, func(*jwt.Token) (any, error) { return []byte("test"), nil }); err != nil {
					t.Fatalf("token: %v", err)
				}
				_, uid := mc["uid"]
				if mc["internal"] != true || mc["hash"] != upload0 || mc["up"] != "add" || mc["lim"] != 1.0 || uid {
					t.Errorf("token claims %v, want internal, hash %s, up add, lim 1 and no uid", mc, upload0)
				}
			}
			expectRows(t, db, c.id, !c.purged, "torrent_resource", "thumbnail")
			expectObjects(t, store, hash, !c.erased)
			warned := slices.ContainsFunc(hook.AllEntries(), func(e *log.Entry) bool { return e.Level == log.WarnLevel && e.Message == offWarn })
			if want := h.up == nil; warned != want {
				t.Errorf("warned %v, want %v: %v", warned, want, hook.AllEntries())
			}
		})
	}
}
