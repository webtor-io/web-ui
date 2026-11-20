package upload

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/thumbnail/thumbtest"
	"github.com/webtor-io/web-ui/services/uploads"
	"github.com/webtor-io/web-ui/services/vault"
)

// deleteEnv is POST /upload/delete on one database, against fakes of
// rest-api, the seeder behind torrent-http-proxy and the Vault service. The
// fakes answer by ID, so each case has its own upload.
type deleteEnv struct {
	db   *pg.DB
	pgCS *cs.PG
	api  *api.Api
	up   *uploads.Service
	v    *vault.Vault

	mu           sync.Mutex
	rest         map[string]int   // rest-api's status for /resource/<id>; 200 is the upload, empty
	restCalls    int              // every rest-api request
	seeder       map[string][]int // the seeder's statuses for DELETE /<id>/, in turn; the last repeats
	seederCalls  map[string]int
	vaultDeletes map[string]int
}

func newDeleteEnv(t *testing.T) *deleteEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := &deleteEnv{db: pgtest.Start(t), rest: map[string]int{}, seeder: map[string][]int{},
		seederCalls: map[string]int{}, vaultDeletes: map[string]int{}}
	e.pgCS = pgtest.CS(t, e.db)
	restSrv := httptest.NewServer(http.HandlerFunc(e.serveRest))
	seederSrv := httptest.NewServer(http.HandlerFunc(e.serveSeeder))
	vaultSrv := httptest.NewServer(http.HandlerFunc(e.serveVault))
	for _, s := range []*httptest.Server{restSrv, seederSrv, vaultSrv} {
		t.Cleanup(s.Close)
	}
	ru, _ := url.Parse(restSrv.URL)
	vu, _ := url.Parse(vaultSrv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, fl := range vault.RegisterFlags(vault.RegisterApiFlags(uploads.RegisterFlags(api.RegisterFlags(nil)))) {
		fl.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + ru.Hostname(), "--webtor-rest-api-port=" + ru.Port(),
		"--upload-url=" + seederSrv.URL, "--use-internal-torrent-http-proxy=false",
		"--webtor-secret=test", "--webtor-key=k", "--rapidapi-host=", "--rapidapi-key=",
		"--vault-service-host=" + vu.Hostname(), "--vault-service-port=" + vu.Port()}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	e.api = api.New(c, http.DefaultClient)
	e.up = uploads.New(c, e.pgCS, e.api)
	e.v = vault.New(c, vault.NewApi(c, http.DefaultClient), nil, http.DefaultClient, e.pgCS, e.api)
	if e.up == nil || e.v == nil {
		t.Fatal("uploads or Vault are off")
	}
	return e
}

func (e *deleteEnv) serveRest(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/resource/")
	e.mu.Lock()
	e.restCalls++
	status, ok := e.rest[id]
	e.mu.Unlock()
	if !ok {
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
	switch status {
	case http.StatusOK:
		_, _ = io.WriteString(w, `{"id":"`+id+`","name":"n","multi_file":true,"size":0,"files_count":0,"private":true,"reserved":0}`)
	case http.StatusForbidden:
		_, _ = io.WriteString(w, `{"error":"x","code":"private_torrent","reason":"no_record"}`)
	default:
		_, _ = io.WriteString(w, `{"error":"x"}`)
	}
}

func (e *deleteEnv) serveSeeder(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(r.URL.Path, "/")
	e.mu.Lock()
	defer e.mu.Unlock()
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	e.seederCalls[id]++
	statuses := e.seeder[id]
	if len(statuses) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(statuses[0])
	if len(statuses) > 1 {
		e.seeder[id] = statuses[1:]
	}
}

func (e *deleteEnv) serveVault(w http.ResponseWriter, r *http.Request) {
	id, isResource := strings.CutPrefix(r.URL.Path, "/resource/")
	if r.Method == http.MethodDelete && isResource {
		e.mu.Lock()
		e.vaultDeletes[id]++
		e.mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (e *deleteEnv) answer(id string, status int, seeder ...int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rest[id] = status
	e.seeder[id] = seeder
}

func (e *deleteEnv) calls(id string) (rest, seeder, vaultDeletes int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.restCalls, e.seederCalls[id], e.vaultDeletes[id]
}

// upload is one case's upload U: its owner o and a stranger x, both with
// a library row on it, its pictures in a store of its own, and the route.
type upload struct {
	e     *deleteEnv
	id    string
	h1    string // the hash of its frame
	o, x  uuid.UUID
	store *thumbtest.Store
	r     http.Handler
}

type seedOpts struct {
	owner    uuid.UUID // the owner; a new account if unset
	noVault  bool      // no vault.resource, no pledge
	noPledge bool      // vault.resource without a pledge
	frozenAt string    // the pledge's frozen_at, SQL; now() - 2 days if empty
}

func (e *deleteEnv) user(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if _, err := e.db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *deleteEnv) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := e.db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// seed writes the upload id and what grew around it: the library rows, the
// resource row, the movie found in it with its file path, a catalog query
// that names the file, the owner's live 4 GiB reserve, the owner's funded
// pledge on a funded vault.resource and the frame of its video.
func (e *deleteEnv) seed(t *testing.T, id string, o seedOpts) *upload {
	t.Helper()
	sum := sha1.Sum([]byte(id))
	u := &upload{e: e, id: id, h1: hex.EncodeToString(sum[:]), o: o.owner, x: e.user(t)}
	if u.o == uuid.Nil {
		u.o = e.user(t)
	}
	for _, user := range []uuid.UUID{u.o, u.x} {
		e.exec(t, `INSERT INTO library (user_id, resource_id, name) VALUES (?, ?, 'n')`, user, id)
	}
	e.exec(t, `INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, 'n', 1, 1)`, id)
	e.exec(t, `INSERT INTO media_info (resource_id, status) VALUES (?, ?)`, id, models.MediaInfoStatusDone)
	e.exec(t, `INSERT INTO movie (resource_id, title, path) VALUES (?, 'n', '/n/a.mkv')`, id)
	e.exec(t, `INSERT INTO ai_enrich.query (parsed_title, content_type, resource_id, hint_path) VALUES (?, 1, ?, '/n/a.mkv')`, id, id)
	e.exec(t, `INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until) VALUES (?, ?, ?, now() + interval '1 minute')`, u.o, id, 4*gib)
	e.exec(t, `INSERT INTO vault.user_vp (user_id, total) VALUES (?, 10) ON CONFLICT DO NOTHING`, u.o)
	if !o.noVault {
		funded := !o.noPledge
		e.exec(t, `INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, name) VALUES (?, 1, CASE WHEN ? THEN 1 ELSE 0 END, ?, 'n')`, id, funded, funded)
		if funded {
			frozen := o.frozenAt
			if frozen == "" {
				frozen = "now() - interval '2 days'"
			}
			e.exec(t, `INSERT INTO vault.pledge (resource_id, user_id, amount, funded, frozen_at) VALUES (?, ?, 1, true, `+frozen+`)`, id, u.o)
		}
	}
	e.exec(t, `INSERT INTO thumbnail (resource_id, path, offset_sec, source_kind, hash, format, size) VALUES (?, '/n/a.mkv', 300, ?, ?, 'jpg', 1)`,
		id, models.ThumbnailSourceFFmpegFrame, u.h1)
	s3, store := thumbtest.Start(t)
	store.Put("thumbs", u.h1+".jpg", []byte("x"))
	store.Put("renders", "poster/thumb-"+u.h1+"/240.jpg", []byte("x"))
	u.store = store
	r := gin.New()
	r.Use(func(c *gin.Context) {
		as, _ := uuid.FromString(c.GetHeader("X-Test-User"))
		ctx := context.WithValue(c.Request.Context(), auth.UserContext{}, &models.User{UserID: as})
		ctx = context.WithValue(ctx, api.ClaimsContext{}, &api.Claims{Uid: api.GenerateSessionIDFromUser(&auth.User{ID: as})})
		c.Request = c.Request.WithContext(ctx)
	})
	RegisterHandler(r, e.up, e.api, e.v, e.pgCS, thumbtest.Service(t, s3, e.pgCS, e.api))
	u.r = r
	return u
}

// post sends POST /upload/delete as user with these ids and returns the
// status the redirect carries.
func (u *upload) post(t *testing.T, as uuid.UUID, ids string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/upload/delete", strings.NewReader(url.Values{"resource_ids": {ids}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Return-Url", "/lib/uploads")
	req.Header.Set("X-Test-User", as.String())
	w := httptest.NewRecorder()
	u.r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("%d %s, want a redirect", w.Code, w.Body)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return loc.Query().Get("status")
}

func (u *upload) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if _, err := u.e.db.QueryOne(pg.Scan(&n), q, args...); err != nil {
		t.Fatal(err)
	}
	return n
}

func (u *upload) has(t *testing.T, table string) bool {
	t.Helper()
	return u.count(t, `SELECT count(*) FROM `+table+` WHERE resource_id = ?`, u.id) > 0
}

func (u *upload) inLibrary(t *testing.T, user uuid.UUID) bool {
	t.Helper()
	return u.count(t, `SELECT count(*) FROM library WHERE user_id = ? AND resource_id = ?`, user, u.id) > 0
}

// detected: the rows enrichment wrote, with the file's path in them.
func (u *upload) detected(t *testing.T) bool {
	t.Helper()
	return u.has(t, "media_info") && u.has(t, "movie") && u.has(t, "ai_enrich.query")
}

func (u *upload) reserve(t *testing.T) *models.UploadReserve {
	t.Helper()
	r := &models.UploadReserve{UserID: u.o, ResourceID: u.id}
	if err := u.e.db.Model(r).WherePK().Select(); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	return r
}

func (u *upload) claims(t *testing.T) []int16 {
	t.Helper()
	var ops []int16
	if _, err := u.e.db.Query(&ops, `SELECT op_type FROM vault.tx_log WHERE resource_id = ?`, u.id); err != nil {
		t.Fatal(err)
	}
	return ops
}

// pictures: the thumbnail row, and how many of its blob and render are left.
func (u *upload) pictures(t *testing.T) (row bool, objects int) {
	t.Helper()
	return u.has(t, "thumbnail"), len(u.store.Keys("thumbs")) + len(u.store.Keys("renders"))
}

func (u *upload) used(t *testing.T) int64 {
	t.Helper()
	used, _, err := u.e.up.Quota(context.Background(), &auth.User{ID: u.o}, tier(0), nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	return used
}

// rename takes table away, as off in its schema, until the test ends.
func (e *deleteEnv) rename(t *testing.T, table, off string) {
	t.Helper()
	e.exec(t, `ALTER TABLE `+table+` RENAME TO `+off)
	moved, name := off, table
	if schema, n, ok := strings.Cut(table, "."); ok {
		moved, name = schema+"."+off, n
	}
	t.Cleanup(func() { e.exec(t, `ALTER TABLE `+moved+` RENAME TO `+name) })
}

func TestDeleteUploads(t *testing.T) {
	e := newDeleteEnv(t)
	n := 0
	next := func() string { n++; return uid(0x300 + n) }
	const ok = http.StatusOK

	t.Run("stranger", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, http.StatusForbidden)
		if st := u.post(t, u.x, u.id); st != "success" {
			t.Errorf("status %q", st)
		}
		if u.inLibrary(t, u.x) || !u.inLibrary(t, u.o) {
			t.Errorf("library: stranger %v, owner %v; want only the stranger's gone", u.inLibrary(t, u.x), u.inLibrary(t, u.o))
		}
		if !u.has(t, "vault.resource") || !u.has(t, "vault.pledge") || !u.has(t, "torrent_resource") || !u.detected(t) {
			t.Error("the owner's rows are touched")
		}
		if _, seeder, vd := e.calls(u.id); seeder+vd != 0 {
			t.Errorf("seeder %d, vault %d calls", seeder, vd)
		}
	})

	t.Run("stranger, gone", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, http.StatusNotFound)
		if st := u.post(t, u.x, u.id); st != "success" {
			t.Errorf("status %q", st)
		}
		if u.inLibrary(t, u.x) || !u.inLibrary(t, u.o) || !u.has(t, "torrent_resource") || !u.detected(t) {
			t.Error("want only the stranger's library row gone")
		}
	})

	t.Run("stranger, gone, pictures", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, http.StatusNotFound)
		u.post(t, u.x, u.id)
		if row, objects := u.pictures(t); !row || objects != 2 {
			t.Errorf("thumbnail row %v, %d objects; want both kept", row, objects)
		}
	})

	var saved *upload
	t.Run("owner, saved", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		saved = u
		e.answer(u.id, ok, http.StatusNoContent)
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("status %q", st)
		}
		if u.has(t, "vault.pledge") || u.has(t, "vault.resource") {
			t.Error("the pledge or vault.resource is left")
		}
		if ops := u.claims(t); len(ops) != 1 || ops[0] != int16(vaultModels.OpTypeClaim) {
			t.Errorf("tx_log %v, want one claim", ops)
		}
		if _, seeder, vd := e.calls(u.id); seeder != 1 || vd != 1 {
			t.Errorf("seeder %d, vault %d calls; want 1, 1", seeder, vd)
		}
		if u.has(t, "torrent_resource") || u.has(t, "library") {
			t.Error("the resource or a library row is left")
		}
		if r := u.reserve(t); r.GoneAt == nil || r.WriteUntil != nil || r.ReservedBytes != 4*gib {
			t.Errorf("reserve %+v, want gone, no token, 4 GiB", r)
		}
	})

	t.Run("owner, reserve after delete", func(t *testing.T) {
		if saved == nil {
			t.Skip("owner, saved did not run")
		}
		if used := saved.used(t); used != 0 {
			t.Errorf("used %d, want 0", used)
		}
	})

	t.Run("rest-api 503", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, http.StatusServiceUnavailable, http.StatusNoContent)
		if st := u.post(t, u.o, u.id); st != "error" {
			t.Errorf("status %q", st)
		}
		if !u.has(t, "vault.pledge") || !u.has(t, "vault.resource") || !u.inLibrary(t, u.o) || !u.has(t, "torrent_resource") {
			t.Error("rows are touched")
		}
		if _, seeder, _ := e.calls(u.id); seeder != 0 {
			t.Errorf("seeder %d calls", seeder)
		}
	})

	t.Run("owner, fresh pledge", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{frozenAt: "now()"})
		p, err := vaultModels.GetUserResourcePledge(context.Background(), e.db, u.o, u.id)
		if err != nil || p == nil {
			t.Fatalf("pledge %v, %v", p, err)
		}
		if frozen, err := e.v.IsPledgeFrozen(context.Background(), p); err != nil || !frozen {
			t.Fatalf("frozen %v, %v; want a frozen pledge", frozen, err)
		}
		e.answer(u.id, ok, http.StatusNoContent)
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("status %q", st)
		}
		if u.has(t, "vault.pledge") || u.has(t, "vault.resource") || u.has(t, "torrent_resource") {
			t.Error("rows are left")
		}
		var balance float64
		if _, err := e.db.QueryOne(pg.Scan(&balance), `SELECT balance FROM vault.tx_log WHERE resource_id = ? AND op_type = ?`, u.id, vaultModels.OpTypeClaim); err != nil || balance != 1 {
			t.Errorf("claim of %v, %v; want 1", balance, err)
		}
		if _, seeder, vd := e.calls(u.id); seeder != 1 || vd != 1 {
			t.Errorf("seeder %d, vault %d calls; want 1, 1", seeder, vd)
		}
	})

	var evicted *upload
	var token *models.UploadReserve
	t.Run("evicted", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		evicted, token = u, u.reserve(t)
		e.answer(u.id, ok, http.StatusGone)
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("status %q", st)
		}
		if u.has(t, "torrent_resource") || u.has(t, "library") {
			t.Error("the resource or a library row is left")
		}
	})

	t.Run("evicted, live token", func(t *testing.T) {
		if evicted == nil {
			t.Skip("evicted did not run")
		}
		u := evicted
		if r := u.reserve(t); r.GoneAt == nil || r.WriteUntil == nil || !r.WriteUntil.Equal(*token.WriteUntil) {
			t.Errorf("reserve %+v, want gone with its token as it was, %v", r, token.WriteUntil)
		}
		if used := u.used(t); used != 4*gib {
			t.Errorf("used %d, want %d", used, 4*gib)
		}
		e.exec(t, `UPDATE upload_reserve SET write_until = now() - interval '6 minutes' WHERE user_id = ? AND resource_id = ?`, u.o, u.id)
		if used := u.used(t); used != 0 {
			t.Errorf("after the token: used %d, want 0", used)
		}
	})

	t.Run("gone", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		before := u.reserve(t)
		e.answer(u.id, http.StatusNotFound)
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("status %q", st)
		}
		if u.has(t, "library") || u.has(t, "torrent_resource") || u.has(t, "media_info") || u.has(t, "movie") || u.has(t, "ai_enrich.query") {
			t.Error("rows are left")
		}
		if u.has(t, "vault.pledge") || u.has(t, "vault.resource") {
			t.Error("the pledge or vault.resource is left")
		}
		if ops := u.claims(t); len(ops) != 1 || ops[0] != int16(vaultModels.OpTypeClaim) {
			t.Errorf("tx_log %v, want one claim", ops)
		}
		if _, seeder, vd := e.calls(u.id); seeder != 0 || vd != 1 {
			t.Errorf("seeder %d, vault %d calls; want 0, 1", seeder, vd)
		}
		if r := u.reserve(t); r.GoneAt != nil || !r.WriteUntil.Equal(*before.WriteUntil) || r.ReservedBytes != before.ReservedBytes {
			t.Errorf("reserve %+v, was %+v", r, before)
		}
	})

	t.Run("gone, pictures", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, http.StatusNotFound)
		u.post(t, u.o, u.id)
		if row, objects := u.pictures(t); row || objects != 0 {
			t.Errorf("thumbnail row %v, %d objects; want none", row, objects)
		}
	})

	t.Run("gone, reserve read fails", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{noVault: true})
		e.answer(u.id, http.StatusNotFound)
		t.Run("off", func(t *testing.T) {
			e.rename(t, "upload_reserve", "upload_reserve_off")
			if st := u.post(t, u.o, u.id); st != "error" {
				t.Errorf("status %q", st)
			}
			if !u.inLibrary(t, u.o) || !u.detected(t) {
				t.Error("rows are touched")
			}
		})
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("retry: status %q", st)
		}
		if u.has(t, "library") || u.has(t, "movie") {
			t.Error("retry: rows are left")
		}
	})

	t.Run("gone, release fails", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, http.StatusNotFound)
		t.Run("off", func(t *testing.T) {
			e.rename(t, "vault.pledge", "pledge_off")
			if st := u.post(t, u.o, u.id); st != "error" {
				t.Errorf("status %q", st)
			}
			if !u.inLibrary(t, u.o) {
				t.Error("the owner's library row is gone")
			}
		})
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("retry: status %q", st)
		}
		if u.inLibrary(t, u.o) || u.has(t, "vault.pledge") || u.has(t, "vault.resource") {
			t.Error("retry: rows are left")
		}
		if _, seeder, _ := e.calls(u.id); seeder != 0 {
			t.Errorf("seeder %d calls", seeder)
		}
	})

	t.Run("gone, no pledge", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{noVault: true})
		e.answer(u.id, http.StatusNotFound)
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("status %q", st)
		}
		if u.inLibrary(t, u.o) {
			t.Error("the owner's library row is left")
		}
		if _, seeder, vd := e.calls(u.id); seeder+vd != 0 {
			t.Errorf("seeder %d, vault %d calls", seeder, vd)
		}
	})

	t.Run("stranger, row without pledges", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{noPledge: true})
		e.answer(u.id, http.StatusForbidden)
		if st := u.post(t, u.x, u.id); st != "success" {
			t.Errorf("status %q", st)
		}
		if u.inLibrary(t, u.x) || !u.inLibrary(t, u.o) || !u.has(t, "vault.resource") {
			t.Error("want only the stranger's library row gone")
		}
		if _, _, vd := e.calls(u.id); vd != 0 {
			t.Errorf("vault %d calls", vd)
		}
	})

	t.Run("batch", func(t *testing.T) {
		u1 := e.seed(t, next(), seedOpts{})
		u2 := e.seed(t, next(), seedOpts{owner: u1.o})
		e.answer(u1.id, ok, http.StatusNoContent)
		e.answer(u2.id, ok, http.StatusNoContent)
		if st := u1.post(t, u1.o, u1.id+",0123456789abcdef0123456789abcdef01234567,"+u2.id); st != "error" {
			t.Errorf("status %q, want the torrent's error", st)
		}
		for _, u := range []*upload{u1, u2} {
			if u.has(t, "torrent_resource") || u.has(t, "library") {
				t.Errorf("%s is left", u.id)
			}
		}
	})

	t.Run("upper case", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, ok, http.StatusNoContent)
		rest, _, _ := e.calls(u.id)
		if st := u.post(t, u.o, strings.ToUpper(u.id)); st != "error" {
			t.Errorf("status %q", st)
		}
		if after, seeder, _ := e.calls(u.id); after != rest || seeder != 0 {
			t.Errorf("rest-api %d, seeder %d calls", after-rest, seeder)
		}
		if !u.has(t, "torrent_resource") || !u.inLibrary(t, u.o) || !u.has(t, "vault.pledge") {
			t.Error("rows are touched")
		}
	})

	t.Run("retry after a THP failure", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, ok, http.StatusInternalServerError, http.StatusNoContent)
		if st := u.post(t, u.o, u.id); st != "error" {
			t.Errorf("status %q", st)
		}
		if u.has(t, "vault.pledge") || u.has(t, "vault.resource") || !u.has(t, "torrent_resource") {
			t.Error("first call: want the Vault rows gone and the resource kept")
		}
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("retry: status %q", st)
		}
		if r := u.reserve(t); u.has(t, "torrent_resource") || r.GoneAt == nil || r.WriteUntil != nil {
			t.Errorf("retry: resource left %v, reserve %+v", u.has(t, "torrent_resource"), r)
		}
	})

	t.Run("foreign 404", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, ok, http.StatusNotFound, http.StatusNoContent)
		if st := u.post(t, u.o, u.id); st != "error" {
			t.Errorf("status %q", st)
		}
		if _, seeder, _ := e.calls(u.id); seeder != 1 {
			t.Errorf("seeder %d calls", seeder)
		}
		if row, objects := u.pictures(t); !u.has(t, "torrent_resource") || !u.inLibrary(t, u.o) || !u.detected(t) || !row || objects != 2 {
			t.Error("first call: rows or pictures are gone with the files kept")
		}
		if r := u.reserve(t); r.GoneAt != nil {
			t.Errorf("first call: reserve %+v, want not gone", r)
		}
		if u.has(t, "vault.pledge") || u.has(t, "vault.resource") {
			t.Error("first call: the Vault rows are left")
		}
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("retry: status %q", st)
		}
		if row, objects := u.pictures(t); u.has(t, "torrent_resource") || row || objects != 0 {
			t.Error("retry: rows or pictures are left")
		}
		if r := u.reserve(t); r.GoneAt == nil || r.WriteUntil != nil {
			t.Errorf("retry: reserve %+v", r)
		}
	})

	t.Run("reserve mark fails", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, ok, http.StatusNoContent)
		t.Run("off", func(t *testing.T) {
			e.rename(t, "upload_reserve", "upload_reserve_off")
			if st := u.post(t, u.o, u.id); st != "success" {
				t.Errorf("status %q", st)
			}
			if u.has(t, "torrent_resource") || u.has(t, "library") || u.has(t, "vault.pledge") || u.has(t, "vault.resource") {
				t.Error("rows are left")
			}
			if _, seeder, _ := e.calls(u.id); seeder != 1 {
				t.Errorf("seeder %d calls", seeder)
			}
		})
		if r := u.reserve(t); r.GoneAt != nil {
			t.Errorf("reserve %+v, want no mark", r)
		}
	})

	t.Run("owner, pictures", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, ok, http.StatusNoContent)
		u.post(t, u.o, u.id)
		if row, objects := u.pictures(t); row || objects != 0 {
			t.Errorf("thumbnail row %v, %d objects; want none", row, objects)
		}
	})

	t.Run("pictures, s3 down", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, ok, http.StatusNoContent)
		u.store.Fail(http.MethodDelete)
		if st := u.post(t, u.o, u.id); st != "error" {
			t.Errorf("status %q", st)
		}
		if _, seeder, _ := e.calls(u.id); seeder != 1 {
			t.Errorf("seeder %d calls", seeder)
		}
		if row, _ := u.pictures(t); !u.has(t, "torrent_resource") || !row {
			t.Error("the resource or the thumbnail row is gone")
		}
		u.store.Fail("")
		e.answer(u.id, http.StatusNotFound)
		if st := u.post(t, u.o, u.id); st != "success" {
			t.Errorf("retry: status %q", st)
		}
		if row, objects := u.pictures(t); u.has(t, "torrent_resource") || row || objects != 0 {
			t.Error("retry: rows or pictures are left")
		}
	})

	t.Run("no api claims", func(t *testing.T) {
		u := e.seed(t, next(), seedOpts{})
		e.answer(u.id, http.StatusForbidden)
		rest, _, _ := e.calls(u.id)
		err := (&Handler{up: e.up, api: e.api, vault: e.v, pg: e.pgCS}).deleteUploads(context.Background(), &auth.User{ID: u.o}, nil, []string{u.id})
		if err == nil {
			t.Error("no error")
		}
		if after, _, _ := e.calls(u.id); after != rest || !u.inLibrary(t, u.o) || !u.has(t, "torrent_resource") {
			t.Error("rest-api asked or rows touched")
		}
	})
}
