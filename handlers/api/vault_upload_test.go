package api

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/models/pgtest"
	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/libapi"
	"github.com/webtor-io/web-ui/services/uploads"
	"github.com/webtor-io/web-ui/services/vault"
)

const (
	pledgeU = "9c5db5752b7c0123456789abcdef0123456789ab" // an upload
	pledgeT = "08ada5a7a6183aae1e09d831df6748d566095a10" // a torrent
)

// pledgeEnv is the JSON API's Vault on a disposable Postgres, against fakes
// of rest-api, the seeder behind torrent-http-proxy and the Vault service.
// u is an account with unlimited points and a live write token on pledgeU.
type pledgeEnv struct {
	h  *Handler
	db *pg.DB
	u  *auth.User

	mu    sync.Mutex
	seals int // PUT /pledgeU/ the seeder got
}

// newPledgeEnv: rest-api answers pledgeU with upload, pledgeT as a public
// 1 GiB torrent.
func newPledgeEnv(t *testing.T, upload string) *pledgeEnv {
	t.Helper()
	e := &pledgeEnv{db: pgtest.Start(t)}
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch id := strings.TrimPrefix(r.URL.Path, "/resource/"); id {
		case pledgeU:
			_, _ = io.WriteString(w, upload)
		case pledgeT:
			_, _ = fmt.Fprintf(w, `{"id":%q,"name":"n","size":1073741824,"files_count":1,"private":false}`, id)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	thp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/"+pledgeU+"/" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		e.mu.Lock()
		e.seals++
		e.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	vaultAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{}`)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	for _, s := range []*httptest.Server{rest, thp, vaultAPI} {
		t.Cleanup(s.Close)
	}
	ru, _ := url.Parse(rest.URL)
	vu, _ := url.Parse(vaultAPI.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range vault.RegisterFlags(vault.RegisterApiFlags(uploads.RegisterFlags(restapi.RegisterFlags(nil)))) {
		f.Apply(fs)
	}
	// Every flag that decides where a request goes is given: they read the
	// environment on Apply.
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + ru.Hostname(), "--webtor-rest-api-port=" + ru.Port(),
		"--upload-url=" + thp.URL, "--use-internal-torrent-http-proxy=false",
		"--webtor-secret=test", "--webtor-key=k", "--rapidapi-host=", "--rapidapi-key=",
		"--vault-service-host=" + vu.Hostname(), "--vault-service-port=" + vu.Port()}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	a := restapi.New(c, http.DefaultClient)
	pgCS := pgtest.CS(t, e.db)
	up := uploads.New(c, pgCS, a)
	v := vault.New(c, vault.NewApi(c, http.DefaultClient), nil, http.DefaultClient, pgCS, a).WithUploads(up)
	if up == nil || v == nil {
		t.Fatal("uploads or Vault are off")
	}
	e.h = &Handler{api: a, vault: v}

	var id uuid.UUID
	if _, err := e.db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	e.u = &auth.User{ID: id}
	e.exec(t, `INSERT INTO vault.user_vp (user_id, total) VALUES (?, NULL)`, id)
	e.exec(t, `INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until) VALUES (?, ?, 1073741824, now() + interval '10 minutes')`, id, pledgeU)
	return e
}

func (e *pledgeEnv) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := e.db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func (e *pledgeEnv) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if _, err := e.db.QueryOne(pg.Scan(&n), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (e *pledgeEnv) sealed() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.seals
}

var pledgeClaims = &restapi.Claims{Uid: "76543210fedcba9876543210fedcba9876543210"}

const uploadReady = `{"id":"` + pledgeU + `","name":"n","size":1073741824,"files_count":1,"private":true,"reserved":1073741824}`

// The JSON API pledges an upload the way the site does: sealed first, the
// write token alive or not.
func TestPledgeUploadSealsAPI(t *testing.T) {
	e := newPledgeEnv(t, uploadReady)

	if p, err := e.h.createPledge(context.Background(), pledgeU, e.u, pledgeClaims); err != nil || p == nil {
		t.Fatalf("createPledge = %v, %v; want the pledge", p, err)
	}
	if n := e.sealed(); n != 1 {
		t.Errorf("the seeder got %d PUTs, want 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM vault.resource WHERE resource_id = ? AND required_vp = 1`, pledgeU); n != 1 {
		t.Errorf("%d vault.resource rows at 1 VP, want 1", n)
	}
}

// An upload's pledge is not removed through the API, its ID in any case:
// the upload is deleted instead. Both pledges are past their freeze, so only
// the guard can refuse. A torrent's goes as before.
func TestRemoveUploadPledgeAPI(t *testing.T) {
	e := newPledgeEnv(t, uploadReady)
	for _, id := range []string{pledgeU, pledgeT} {
		e.exec(t, `INSERT INTO vault.resource (resource_id, required_vp, funded_vp, funded, name) VALUES (?, 1, 1, true, 'n')`, id)
		e.exec(t, `INSERT INTO vault.pledge (resource_id, user_id, amount, funded, frozen_at) VALUES (?, ?, 1, true, now() - interval '2 days')`, id, e.u.ID)
	}
	pledges := func(id string) int {
		return e.count(t, `SELECT count(*) FROM vault.pledge WHERE resource_id = ? AND user_id = ?`, id, e.u.ID)
	}

	for _, id := range []string{pledgeU, strings.ToUpper(pledgeU)} {
		err := e.h.removePledge(context.Background(), id, e.u)
		if err == nil || err.Status != http.StatusConflict || err.Code != libapi.CodeConflict || !strings.Contains(err.Message, "delete the upload") {
			t.Errorf("%s: removePledge = %v, want 409 conflict saying to delete the upload", id, err)
		}
		if pledges(pledgeU) != 1 {
			t.Fatalf("%s: the upload's pledge is gone", id)
		}
	}

	if err := e.h.removePledge(context.Background(), pledgeT, e.u); err != nil {
		t.Errorf("a torrent: removePledge = %v", err)
	}
	if pledges(pledgeT) != 0 {
		t.Error("a torrent: the pledge is still there")
	}
}

// An upload with no finished file answers 409 through the API too, before
// anything is sealed.
func TestPledgeUploadEmptyAPI(t *testing.T) {
	e := newPledgeEnv(t, `{"id":"`+pledgeU+`","name":"n","size":0,"files_count":0,"private":true,"reserved":5368709120}`)

	_, err := e.h.createPledge(context.Background(), pledgeU, e.u, pledgeClaims)
	if err == nil || err.Status != http.StatusConflict || err.Code != libapi.CodeConflict || !strings.Contains(err.Message, "no finished files") {
		t.Fatalf("createPledge = %v, want 409 conflict: no finished files", err)
	}
	if n := e.sealed(); n != 0 {
		t.Errorf("the seeder got %d PUTs, want none", n)
	}
	if n := e.count(t, `SELECT count(*) FROM vault.resource WHERE resource_id = ?`, pledgeU) +
		e.count(t, `SELECT count(*) FROM vault.pledge WHERE resource_id = ?`, pledgeU); n != 0 {
		t.Errorf("%d vault.resource and pledge rows, want none", n)
	}
}
