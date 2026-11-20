package vault

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
	"testing"

	"github.com/go-pg/pg/v10"
	"github.com/golang-jwt/jwt/v5"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/models/pgtest"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/uploads"
)

const (
	privateHash = "0123456789abcdef0123456789abcdef01234567"
	publicHash  = "fedcba9876543210fedcba9876543210fedcba98"
	uploadHash  = "9c5db5752b7c0123456789abcdef0123456789ab"
)

// vaultCall is one request the fake Vault got.
type vaultCall struct{ method, path, body string }

func (c vaultCall) String() string { return c.method + " " + c.path + " " + c.body }

// fakeVault is the Vault service: it knows a resource once it was PUT and
// answers /uids with uidsStatus.
type fakeVault struct {
	mu         sync.Mutex
	known      map[string]bool
	calls      []vaultCall
	uidsStatus int
}

func (f *fakeVault) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, vaultCall{r.Method, r.URL.Path, string(b)})
	id := strings.TrimPrefix(r.URL.Path, "/resource/")
	switch {
	case r.Method == http.MethodPut && strings.HasSuffix(id, "/uids"):
		w.WriteHeader(f.uidsStatus)
		if f.uidsStatus != http.StatusOK {
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		}
	case r.Method == http.MethodPut:
		f.known[id] = true
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"resource_id":%q}`, id)
	case r.Method == http.MethodGet && f.known[id]:
		_, _ = fmt.Fprintf(w, `{"resource_id":%q,"status":0}`, id)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeVault) failUIDs() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uidsStatus = http.StatusInternalServerError
}

// since is every call after the first n.
func (f *fakeVault) since(n int) []vaultCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]vaultCall(nil), f.calls[n:]...)
}

// with is the calls of method on path.
func (f *fakeVault) with(method, path string) []vaultCall {
	var out []vaultCall
	for _, c := range f.since(0) {
		if c.method == method && c.path == path {
			out = append(out, c)
		}
	}
	return out
}

type uidsEnv struct {
	s     *Vault
	db    *pg.DB
	vault *fakeVault

	mu         sync.Mutex
	rest       http.HandlerFunc // rest-api's answer; nil: restResource
	calls      []string         // "GET /resource" from rest-api and "PUT /<id>/" from the seeder, in order
	sealStatus int              // the seeder's answer to a service PUT it accepts
	seals      int              // PUTs the seeder got; never cleared, unlike calls
	tokens     []string         // the token of each PUT
}

// newUIDsEnv is a Vault on a disposable Postgres holding privateHash and
// publicHash, as rest-api calls them, both 1 VP, with uploads on: their
// service requests go to a fake of the seeder behind torrent-http-proxy.
func newUIDsEnv(t *testing.T) *uidsEnv {
	e := &uidsEnv{db: pgtest.Start(t), sealStatus: http.StatusNoContent}
	rest := httptest.NewServer(http.HandlerFunc(e.serveRest))
	thp := httptest.NewServer(http.HandlerFunc(e.serveSeeder))
	fv := &fakeVault{known: map[string]bool{}, uidsStatus: http.StatusOK}
	srv := httptest.NewServer(fv)
	for _, s := range []*httptest.Server{rest, thp, srv} {
		t.Cleanup(s.Close)
	}
	ru, _ := url.Parse(rest.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range uploads.RegisterFlags(api.RegisterFlags(nil)) {
		f.Apply(fs)
	}
	// Every flag that decides where a request goes is given: they read the
	// environment on Apply.
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + ru.Hostname(), "--webtor-rest-api-port=" + ru.Port(),
		"--webtor-secret=test", "--webtor-key=k", "--rapidapi-host=", "--rapidapi-key=",
		"--use-internal-torrent-http-proxy=false", "--upload-url=" + thp.URL}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	a := api.New(c, rest.Client())
	pgCS := pgtest.CS(t, e.db)
	e.s = (&Vault{api: a, vaultApi: &Api{url: srv.URL, cl: srv.Client()}, pg: pgCS, claims: claims.New(nil, nil, nil)}).
		WithUploads(uploads.New(c, pgCS, a))
	e.vault = fv
	for _, h := range []string{privateHash, publicHash} {
		if _, err := vaultModels.CreateResource(context.Background(), e.db, h, 1, "n"); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// serveRest is rest-api: rest, or restResource. Like rest-api, it takes the
// ID in the path in any case.
func (e *uidsEnv) serveRest(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.calls = append(e.calls, r.Method+" /resource")
	h := e.rest
	e.mu.Unlock()
	if h == nil {
		h = restResource
	}
	h(w, r)
}

// restResource answers 1 GiB: private for privateHash and an upload, which
// also has one finished file and 1 GiB reserved.
func restResource(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(strings.TrimPrefix(r.URL.Path, "/resource/"))
	up := ""
	if uploads.Owned(id) {
		up = `,"reserved":1073741824,"files_count":1`
	}
	_, _ = fmt.Fprintf(w, `{"id":%q,"name":"n","size":1073741824,"private":%v%s}`, id, id == privateHash || uploads.Owned(id), up)
}

// serveSeeder is torrent-http-proxy and the seeder behind it on a service
// PUT /<id>/: as the seeder, 403 to a token that is not web-ui's own service
// one before anything else, then sealStatus. Other requests get 405.
func (e *uidsEnv) serveSeeder(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, r.Method+" "+r.URL.Path)
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	e.seals++
	token := r.URL.Query().Get("token")
	e.tokens = append(e.tokens, token)
	c := &api.Claims{}
	if _, err := jwt.ParseWithClaims(token, c, func(*jwt.Token) (any, error) { return []byte("test"), nil }); err != nil ||
		!c.Internal || c.ExpiresAt == nil || c.Lim <= 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.WriteHeader(e.sealStatus)
}

func (e *uidsEnv) setRest(h http.HandlerFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rest = h
}

func (e *uidsEnv) setSealStatus(status int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sealStatus = status
}

// journal is what rest-api and the seeder got since the last clear.
func (e *uidsEnv) journal() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

func (e *uidsEnv) clearJournal() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = nil
}

// sealed is how many PUTs the seeder got, ever.
func (e *uidsEnv) sealed() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.seals
}

// user is a new account with unlimited points.
func (e *uidsEnv) user(t *testing.T) *auth.User {
	t.Helper()
	var id uuid.UUID
	if _, err := e.db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO vault.user_vp (user_id, total) VALUES (?, NULL)`, id); err != nil {
		t.Fatal(err)
	}
	return &auth.User{ID: id}
}

func (e *uidsEnv) pledge(t *testing.T, u *auth.User, hash string) *vaultModels.Pledge {
	t.Helper()
	r, err := vaultModels.GetResource(context.Background(), e.db, hash)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.s.CreatePledge(context.Background(), u, r)
	if err != nil {
		t.Fatalf("CreatePledge: %v", err)
	}
	return p
}

// defund runs recalculatePledgeFunding for u with total 0.5, below any of
// its 1 VP pledges, and syncs what it changed.
func (e *uidsEnv) defund(t *testing.T, u *auth.User) []string {
	t.Helper()
	total := 0.5
	var changed []string
	err := e.db.RunInTransaction(context.Background(), func(tx *pg.Tx) (err error) {
		changed, err = e.s.recalculatePledgeFunding(context.Background(), tx, u, &total)
		return
	})
	if err != nil {
		t.Fatal(err)
	}
	e.s.syncPrivateUIDs(context.Background(), changed...)
	return changed
}

// uidsBody is the body Vault gets for these users, in this order.
func uidsBody(users ...*auth.User) string {
	uids := []string{}
	for _, u := range users {
		uids = append(uids, api.GenerateSessionIDFromUser(u))
	}
	b, _ := json.Marshal(map[string][]string{"uids": uids})
	return string(b)
}

func wantCalls(t *testing.T, got []vaultCall, want ...vaultCall) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Vault got\n%v\nwant\n%v", got, want)
	}
}

// The queueing PUT of a private resource tells Vault its pledger from the
// first run; a public one keeps today's empty body.
func TestQueuePutCarriesUIDs(t *testing.T) {
	e := newUIDsEnv(t)
	a := e.user(t)
	e.pledge(t, a, privateHash)
	e.pledge(t, a, publicHash)

	wantCalls(t, e.vault.with(http.MethodPut, "/resource/"+privateHash), vaultCall{http.MethodPut, "/resource/" + privateHash, uidsBody(a)})
	wantCalls(t, e.vault.with(http.MethodPut, "/resource/"+publicHash), vaultCall{http.MethodPut, "/resource/" + publicHash, ""})
}

// A second pledger reaches Vault through /uids, both pledgers in pledge
// order; the resource is not queued again.
func TestSecondPledgeSendsOnlyUIDs(t *testing.T) {
	e := newUIDsEnv(t)
	a, b := e.user(t), e.user(t)
	e.pledge(t, a, privateHash)
	e.pledge(t, b, privateHash)

	calls := e.vault.since(0)
	wantCalls(t, calls[len(calls)-1:], vaultCall{http.MethodPut, "/resource/" + privateHash + "/uids", uidsBody(a, b)})
	if n := len(e.vault.with(http.MethodPut, "/resource/"+privateHash)); n != 1 {
		t.Errorf("PUT /resource/%s sent %d times, want once (A's)", privateHash, n)
	}
}

// A removed pledge leaves the list, and nothing is queued.
func TestRemovePledgeSendsOnlyUIDs(t *testing.T) {
	e := newUIDsEnv(t)
	a, b := e.user(t), e.user(t)
	e.pledge(t, a, privateHash)
	pb := e.pledge(t, b, privateHash)
	n := len(e.vault.since(0))

	if err := e.s.RemovePledge(context.Background(), pb); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, e.vault.since(n), vaultCall{http.MethodPut, "/resource/" + privateHash + "/uids", uidsBody(a)})
}

// A defunded pledger is no pledger: the list is emptied, nothing is queued.
func TestDefundSendsOnlyUIDs(t *testing.T) {
	e := newUIDsEnv(t)
	a := e.user(t)
	e.pledge(t, a, privateHash)
	n := len(e.vault.since(0))

	if changed := e.defund(t, a); fmt.Sprint(changed) != fmt.Sprint([]string{privateHash}) {
		t.Errorf("recalculatePledgeFunding changed %v, want [%s]", changed, privateHash)
	}
	wantCalls(t, e.vault.since(n), vaultCall{http.MethodPut, "/resource/" + privateHash + "/uids", uidsBody()})
}

// A public resource never gets /uids, whatever happens to its pledges.
func TestPublicSendsNoUIDs(t *testing.T) {
	e := newUIDsEnv(t)
	a, b := e.user(t), e.user(t)
	e.pledge(t, a, publicHash)
	pb := e.pledge(t, b, publicHash)
	if err := e.s.RemovePledge(context.Background(), pb); err != nil {
		t.Fatal(err)
	}
	if changed := e.defund(t, a); len(changed) != 1 {
		t.Errorf("recalculatePledgeFunding changed %v, want [%s]", changed, publicHash)
	}
	if err := e.s.ForgetUser(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range e.vault.since(0) {
		if strings.HasSuffix(c.path, "/uids") {
			t.Errorf("public resource got %v", c)
		}
	}
}

// /uids goes after the commit: Vault failing it costs the pledge nothing.
func TestUIDsFailureKeepsPledge(t *testing.T) {
	e := newUIDsEnv(t)
	e.vault.failUIDs()
	a := e.user(t)
	r, err := vaultModels.GetResource(context.Background(), e.db, privateHash)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.s.CreatePledge(context.Background(), a, r); err != nil {
		t.Fatalf("CreatePledge: %v, want no error", err)
	}
	if p, err := vaultModels.GetUserResourcePledge(context.Background(), e.db, a.ID, privateHash); err != nil || p == nil {
		t.Errorf("pledge row %v, %v; want it kept", p, err)
	}
	if len(e.vault.with(http.MethodPut, "/resource/"+privateHash+"/uids")) == 0 {
		t.Error("no /uids sent: the failure was never exercised")
	}
}

// A deleted account leaves the pledger list of every private resource it
// pledged to, and a Vault failure is the caller's error.
func TestForgetUserExcludesUser(t *testing.T) {
	e := newUIDsEnv(t)
	a, b := e.user(t), e.user(t)
	e.pledge(t, a, privateHash)
	e.pledge(t, b, privateHash)
	n := len(e.vault.since(0))

	if err := e.s.ForgetUser(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, e.vault.since(n), vaultCall{http.MethodPut, "/resource/" + privateHash + "/uids", uidsBody(b)})

	e.vault.failUIDs()
	if err := e.s.ForgetUser(context.Background(), a.ID); err == nil {
		t.Error("ForgetUser with Vault failing: no error")
	}
}

// An upload's pledger stays on its list unfunded: Vault keeps the upload's
// only copy for the reaper's grace, and only its owner may read it then.
// A private torrent's unfunded pledger leaves the list, as before.
func TestUploadPledgersIncludeUnfunded(t *testing.T) {
	e := newUIDsEnv(t)
	if _, err := vaultModels.CreateResource(context.Background(), e.db, uploadHash, 1, "n"); err != nil {
		t.Fatal(err)
	}
	a := e.user(t)
	e.pledge(t, a, uploadHash)
	e.pledge(t, a, privateHash)
	e.defund(t, a)

	for hash, want := range map[string]string{uploadHash: uidsBody(a), privateHash: uidsBody()} {
		calls := e.vault.with(http.MethodPut, "/resource/"+hash+"/uids")
		if len(calls) == 0 {
			t.Errorf("%s: no /uids sent", hash)
			continue
		}
		if got := calls[len(calls)-1].body; got != want {
			t.Errorf("%s: last /uids %s, want %s", hash, got, want)
		}
	}
}
