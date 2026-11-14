package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models/pgtest"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
)

const (
	privateHash = "0123456789abcdef0123456789abcdef01234567"
	publicHash  = "fedcba9876543210fedcba9876543210fedcba98"
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
}

// newUIDsEnv is a Vault on a disposable Postgres holding privateHash and
// publicHash, as rest-api calls them, both 1 VP.
func newUIDsEnv(t *testing.T) *uidsEnv {
	db := pgtest.Start(t)
	s := newTestVault(t, func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/resource/")
		_, _ = fmt.Fprintf(w, `{"id":%q,"name":"n","size":1073741824,"private":%v}`, id, id == privateHash)
	})
	fv := &fakeVault{known: map[string]bool{}, uidsStatus: http.StatusOK}
	srv := httptest.NewServer(fv)
	t.Cleanup(srv.Close)
	s.vaultApi = &Api{url: srv.URL, cl: srv.Client()}
	s.pg = pgtest.CS(t, db)
	for _, h := range []string{privateHash, publicHash} {
		if _, err := vaultModels.CreateResource(context.Background(), db, h, 1, "n"); err != nil {
			t.Fatal(err)
		}
	}
	return &uidsEnv{s: s, db: db, vault: fv}
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
