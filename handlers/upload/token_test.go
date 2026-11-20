package upload

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/golang-jwt/jwt/v5"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	proto "github.com/webtor-io/claims-provider/proto"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/uploads"
)

const gib int64 = 1 << 30

// uid is the n-th upload ID: the upload prefix, then n in hex.
func uid(n int) string { return fmt.Sprintf("9c5db5752b7c0%027x", n) }

func tier(id uint32) *claims.Data {
	return &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: id}}}
}

// reply is what the fake rest-api answers for one ID: an upload resource
// with reserved, unless noReserved, on 200; body otherwise.
type reply struct {
	status     int
	body       string
	reserved   int64
	noReserved bool
}

func ok(n int64) reply { return reply{status: http.StatusOK, reserved: n} }

// fakeRest is rest-api's /resource/<id>, counting requests. An ID with no
// reply set answers 503.
type fakeRest struct {
	mu      sync.Mutex
	calls   int
	replies map[string]reply
}

func (f *fakeRest) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, _ := strings.CutPrefix(r.URL.Path, "/resource/")
	f.mu.Lock()
	f.calls++
	rp, set := f.replies[id]
	f.mu.Unlock()
	if !set {
		rp = reply{status: http.StatusServiceUnavailable, body: `{"error":"down"}`}
	}
	w.WriteHeader(rp.status)
	if rp.status != http.StatusOK {
		_, _ = io.WriteString(w, rp.body)
		return
	}
	res := map[string]any{"id": id, "name": "n", "multi_file": true, "size": 0, "files_count": 0, "private": true}
	if !rp.noReserved {
		res["reserved"] = rp.reserved
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (f *fakeRest) set(id string, r reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[id] = r
}

func (f *fakeRest) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// env is the token route behind a middleware that puts in the request what
// the test set: the user, the tier claims and the api claims.
type env struct {
	db   *pg.DB
	rest *fakeRest
	r    *gin.Engine
	user uuid.UUID
	cd   *claims.Data
	ac   *api.Claims
}

func newEnv(t *testing.T) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := &env{db: pgtest.Start(t), rest: &fakeRest{replies: map[string]reply{}}}
	srv := httptest.NewServer(e.rest)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, fl := range uploads.RegisterFlags(api.RegisterFlags(nil)) {
		fl.Apply(fs)
	}
	if err := fs.Parse([]string{"--webtor-rest-api-host=" + u.Hostname(), "--webtor-rest-api-port=" + u.Port(),
		"--upload-url=https://up.example.invalid", "--webtor-secret=test", "--webtor-key=k",
		"--rapidapi-host=", "--rapidapi-key="}); err != nil {
		t.Fatal(err)
	}
	c := cli.NewContext(cli.NewApp(), fs, nil)
	a := api.New(c, srv.Client())
	p := pgtest.CS(t, e.db)
	up := uploads.New(c, p, a)
	if up == nil {
		t.Fatal("uploads are off")
	}
	e.r = gin.New()
	e.r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if e.user != uuid.Nil {
			ctx = context.WithValue(ctx, auth.UserContext{}, &models.User{UserID: e.user})
		}
		if e.cd != nil {
			ctx = context.WithValue(ctx, claims.Context{}, e.cd)
		}
		if e.ac != nil {
			ctx = context.WithValue(ctx, api.ClaimsContext{}, e.ac)
		}
		c.Request = c.Request.WithContext(ctx)
	})
	RegisterHandler(e.r, up, a, nil, p, nil)
	return e
}

// signIn makes a new account and sends the following requests as it, with
// the tier cd.
func (e *env) signIn(t *testing.T, cd *claims.Data) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if _, err := e.db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, uuid.NewV4().String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	e.user, e.cd = id, cd
	e.ac = &api.Claims{Uid: api.GenerateSessionIDFromUser(&auth.User{ID: id})}
	return id
}

func (e *env) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/upload/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

type grant struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Token  string `json:"token"`
	APIKey string `json:"apiKey"`
	Cap    int64  `json:"cap"`
	Used   int64  `json:"used"`
	claims *api.Claims
}

// issued is the 200 answer to body, with its token's claims.
func (e *env) issued(t *testing.T, body string) *grant {
	t.Helper()
	w := e.post(t, body)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s, want 200", body, w.Code, w.Body)
	}
	g := &grant{claims: &api.Claims{}}
	if err := json.Unmarshal(w.Body.Bytes(), g); err != nil {
		t.Fatal(err)
	}
	if _, err := jwt.ParseWithClaims(g.Token, g.claims, func(*jwt.Token) (any, error) { return []byte("test"), nil }); err != nil {
		t.Fatalf("token of %s: %v", body, err)
	}
	return g
}

func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if _, err := e.db.QueryOne(pg.Scan(&n), query, args...); err != nil {
		t.Fatal(err)
	}
	return n
}

// rows counts the rows a token request may write.
func (e *env) rows(t *testing.T) (resources, library, reserves int) {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM torrent_resource`), e.count(t, `SELECT count(*) FROM library`), e.count(t, `SELECT count(*) FROM upload_reserve`)
}

func (e *env) named(t *testing.T, name string) (resources, library int) {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM torrent_resource WHERE name = ?`, name), e.count(t, `SELECT count(*) FROM library WHERE name = ?`, name)
}

// reserve is the reserve row of the signed-in account for id; nil without one.
func (e *env) reserve(t *testing.T, id string) *models.UploadReserve {
	t.Helper()
	r := &models.UploadReserve{UserID: e.user, ResourceID: id}
	if err := e.db.Model(r).WherePK().Select(); err == pg.ErrNoRows {
		return nil
	} else if err != nil {
		t.Fatal(err)
	}
	return r
}

// row writes a reserve row of the signed-in account; writeUntil and gone are
// SQL expressions.
func (e *env) row(t *testing.T, id string, bytes int64, writeUntil, gone string) {
	t.Helper()
	if _, err := e.db.Exec(`INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until, gone_at) VALUES (?, ?, ?, `+writeUntil+`, `+gone+`)`,
		e.user, id, bytes); err != nil {
		t.Fatal(err)
	}
}

const live = "now() + interval '10 minutes'"

func TestTokenAnonymous(t *testing.T) {
	e := newEnv(t)
	if w := e.post(t, `{"name":"Name"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("%d, want 401", w.Code)
	}
	if res, lib, rsv := e.rows(t); res+lib+rsv != 0 {
		t.Errorf("rows: resource %d, library %d, reserve %d", res, lib, rsv)
	}
}

func TestTokenLimitByTier(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		name string
		tier uint32
		cap  int64
	}{
		{"free", 0, uploads.FreeCap},
		{"paid", 2, uploads.PaidCap},
		{"a synthetic paid tier, no payment provider", 1, uploads.PaidCap},
	} {
		t.Run(c.name, func(t *testing.T) {
			e.signIn(t, tier(c.tier))
			g := e.issued(t, `{"name":"Name","want":53687091200}`)
			if g.Cap != c.cap || g.claims.Lim != c.cap {
				t.Errorf("cap %d, lim %d; want %d", g.Cap, g.claims.Lim, c.cap)
			}
			if r := e.reserve(t, g.ID); r == nil || r.ReservedBytes != c.cap {
				t.Errorf("reserve %+v, want %d", r, c.cap)
			}
		})
	}
}

func TestTokenWant(t *testing.T) {
	e := newEnv(t)
	e.signIn(t, tier(0))
	g := e.issued(t, `{"name":"Name","want":3221225472}`)
	x := g.ID
	if g.claims.Lim != 3*gib {
		t.Errorf("lim %d, want %d", g.claims.Lim, 3*gib)
	}
	if r := e.reserve(t, x); r == nil || r.ReservedBytes != 3*gib {
		t.Fatalf("reserve %+v, want %d", r, 3*gib)
	}

	t.Run("a renewal, with want 0 or without want, keeps the live reserve", func(t *testing.T) {
		e.rest.set(x, ok(0))
		for _, body := range []string{`{"id":"` + x + `","want":0}`, `{"id":"` + x + `"}`} {
			g := e.issued(t, body)
			if g.claims.Up != "add" || g.claims.Lim != 3*gib || g.Used != 0 {
				t.Errorf("%s: up %q, lim %d, used %d; want add, %d, 0", body, g.claims.Up, g.claims.Lim, g.Used, 3*gib)
			}
			if r := e.reserve(t, x); r.ReservedBytes != 3*gib {
				t.Errorf("%s: reserve %d, want %d", body, r.ReservedBytes, 3*gib)
			}
		}
	})

	t.Run("a new upload without want", func(t *testing.T) {
		before := e.rest.count()
		_, _, reserves := e.rows(t)
		for _, body := range []string{`{"name":"a","want":0}`, `{"name":"a"}`} {
			if w := e.post(t, body); w.Code != http.StatusBadRequest {
				t.Errorf("%s: %d, want 400", body, w.Code)
			}
		}
		if res, lib := e.named(t, "a"); res+lib != 0 {
			t.Errorf("rows named a: resource %d, library %d", res, lib)
		}
		if _, _, n := e.rows(t); n != reserves {
			t.Errorf("reserve rows %d, want %d", n, reserves)
		}
		if n := e.rest.count() - before; n != 0 {
			t.Errorf("rest-api asked %d times", n)
		}
	})

	t.Run("not the form", func(t *testing.T) {
		res0, lib0, rsv0 := e.rows(t)
		for _, body := range []string{
			`{"name":"a","want":-1}`,
			`{"id":"` + x + `","want":-1}`,
			`{"name":"a","id":"` + x + `","want":1}`,
			`{}`,
			`not json`,
		} {
			if w := e.post(t, body); w.Code != http.StatusBadRequest {
				t.Errorf("%s: %d, want 400", body, w.Code)
			}
		}
		if res, lib, rsv := e.rows(t); res != res0 || lib != lib0 || rsv != rsv0 {
			t.Errorf("rows %d %d %d, want %d %d %d", res, lib, rsv, res0, lib0, rsv0)
		}
	})

	// The seeder holds nothing and the reserve is over: a renewal has
	// nothing to renew.
	t.Run("a renewal of an empty upload whose reserve is over", func(t *testing.T) {
		e.signIn(t, tier(0))
		y := uid(0xd)
		e.rest.set(y, ok(0))
		w := e.post(t, `{"id":"`+y+`","want":0}`)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		want := map[string]any{"code": "quota", "used": float64(0), "cap": float64(uploads.FreeCap)}
		if w.Code != http.StatusRequestEntityTooLarge || !reflect.DeepEqual(body, want) {
			t.Errorf("%d %s, want 413 %v", w.Code, w.Body, want)
		}
		if r := e.reserve(t, y); r != nil {
			t.Errorf("reserve %+v, want none", r)
		}
		if g := e.issued(t, `{"id":"`+y+`","want":1}`); g.claims.Lim != 1 {
			t.Errorf("lim %d, want 1", g.claims.Lim)
		}
	})
}

func TestTokenQuotaFull(t *testing.T) {
	e := newEnv(t)
	e.signIn(t, tier(0))
	e.row(t, uid(1), 10*gib, live, "NULL")
	w := e.post(t, `{"name":"Name","want":1}`)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("%d %s, want 413", w.Code, w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"code": "quota", "used": float64(10 * gib), "cap": float64(10 * gib)}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body %v, want %v", body, want)
	}
	if res, lib := e.named(t, "Name"); res+lib != 0 {
		t.Errorf("rows named Name: resource %d, library %d", res, lib)
	}
	if n := e.count(t, `SELECT count(*) FROM upload_reserve`); n != 1 || e.reserve(t, uid(1)) == nil {
		t.Errorf("%d reserve rows, want only the other upload's", n)
	}
}

// A paid account went free while its upload holds 4 GiB: renewing keeps
// what the seeder holds, though the other uploads fill the free cap.
func TestTokenDowngrade(t *testing.T) {
	e := newEnv(t)
	e.signIn(t, tier(0))
	e.row(t, uid(1), 10*gib, live, "NULL")
	x := uid(2)
	e.rest.set(x, ok(4*gib))
	if g := e.issued(t, `{"id":"`+x+`","want":1073741824}`); g.claims.Lim != 4*gib {
		t.Errorf("lim %d, want %d", g.claims.Lim, 4*gib)
	}
}

// A signed-in request without what the claims and api middleware put in.
func TestTokenNilClaims(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		name  string
		unset func()
	}{
		{"no tier claims", func() { e.cd = nil }},
		{"no api claims", func() { e.ac = nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e.signIn(t, tier(0))
			c.unset()
			w := e.post(t, `{"name":"Name","want":1}`)
			if w.Code != http.StatusInternalServerError {
				t.Errorf("%d, want 500", w.Code)
			}
			if strings.Contains(w.Body.String(), "token") {
				t.Errorf("body %s", w.Body)
			}
			if res, lib, rsv := e.rows(t); res+lib+rsv != 0 {
				t.Errorf("rows: resource %d, library %d, reserve %d", res, lib, rsv)
			}
		})
	}
}

func TestTokenClaims(t *testing.T) {
	e := newEnv(t)
	u := e.signIn(t, tier(0))
	// The viewer's claims carry what a write token must not: another
	// hash, rules, the internal mark and their own expiry.
	e.ac = &api.Claims{
		Uid:              api.GenerateSessionIDFromUser(&auth.User{ID: u}),
		SessionID:        "session",
		Hash:             "0123456789abcdef0123456789abcdef01234567",
		Rules:            []api.Rule{{Kind: "grace"}},
		Internal:         true,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)), Subject: "s"},
	}
	before := time.Now()
	g := e.issued(t, `{"name":"Name","want":10737418240}`)
	after := time.Now()
	c := g.claims
	if c.Hash != g.ID || !uploads.Owned(g.ID) || c.Up != "new" || c.Lim != uploads.FreeCap {
		t.Errorf("hash %q, id %q, up %q, lim %d", c.Hash, g.ID, c.Up, c.Lim)
	}
	if c.Uid != api.GenerateSessionIDFromUser(&auth.User{ID: u}) || c.SessionID != "session" {
		t.Errorf("uid %q, session %q: not the viewer's", c.Uid, c.SessionID)
	}
	if c.Internal || c.Rules != nil || c.Subject != "" {
		t.Errorf("internal %v, rules %v, subject %q", c.Internal, c.Rules, c.Subject)
	}
	if exp := c.ExpiresAt.Time; !exp.After(before.Add(14*time.Minute)) || exp.After(after.Add(15*time.Minute)) {
		t.Errorf("expires %v, want 15 minutes from %v", exp, before)
	}
	if g.URL != "https://up.example.invalid/"+g.ID || g.APIKey != "k" || g.Used != 0 || g.Cap != uploads.FreeCap {
		t.Errorf("url %q, apiKey %q, used %d, cap %d", g.URL, g.APIKey, g.Used, g.Cap)
	}

	e.rest.set(g.ID, ok(0))
	if r := e.issued(t, `{"id":"`+g.ID+`"}`); r.claims.Up != "add" || r.claims.Hash != g.ID {
		t.Errorf("renewal: up %q, hash %q", r.claims.Up, r.claims.Hash)
	}
}

func TestTokenForID(t *testing.T) {
	e := newEnv(t)
	for n, c := range []struct {
		name   string
		id     string // empty: X
		rest   reply  // X's answer; none for another ID
		frozen bool
		status int
		body   string
		calls  int // -1: not checked
	}{
		{"kind 1", "9c5db5752b7c1123456789abcdef0123456789ab", reply{}, false, http.StatusNotFound, "", 0},
		{"upper case", "9C5DB5752B7C0123456789ABCDEF0123456789AB", reply{}, false, http.StatusNotFound, "", 0},
		{"rest-api refuses", "", reply{status: http.StatusForbidden, body: `{"error":"x","code":"private_torrent","reason":"no_record"}`}, false, http.StatusNotFound, "", -1},
		{"rest-api does not know it", "", reply{status: http.StatusNotFound}, false, http.StatusGone, "", -1},
		{"rest-api fails", "", reply{status: http.StatusServiceUnavailable, body: `{"error":"down"}`}, false, http.StatusServiceUnavailable, "", -1},
		{"rest-api answers without reserved", "", reply{status: http.StatusOK, noReserved: true}, false, http.StatusServiceUnavailable, "", -1},
		{"saved to Vault", "", ok(0), true, http.StatusConflict, `{"code":"frozen"}`, -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			e.signIn(t, tier(0))
			x := uid(0x100 + n)
			e.row(t, x, gib, "now() - interval '1 hour'", "now()")
			before := e.reserve(t, x)
			id := c.id
			if id == "" {
				id = x
			}
			if c.rest.status != 0 {
				e.rest.set(x, c.rest)
			}
			if c.frozen {
				if _, err := e.db.Exec(`INSERT INTO vault.resource (resource_id, required_vp, funded_vp, name) VALUES (?, 1, 1, 'n')`, x); err != nil {
					t.Fatal(err)
				}
			}
			calls := e.rest.count()
			w := e.post(t, `{"id":"`+id+`","want":1}`)
			if w.Code != c.status || (c.body != "" && w.Body.String() != c.body) {
				t.Errorf("%d %s, want %d %s", w.Code, w.Body, c.status, c.body)
			}
			if c.calls >= 0 && e.rest.count()-calls != c.calls {
				t.Errorf("rest-api asked %d times, want %d", e.rest.count()-calls, c.calls)
			}
			if after := e.reserve(t, x); !reflect.DeepEqual(after, before) {
				t.Errorf("reserve %+v, was %+v", after, before)
			}
		})
	}
	t.Run("a token", func(t *testing.T) {
		e.signIn(t, tier(0))
		x := uid(0x200)
		e.row(t, x, gib, "now() - interval '1 hour'", "now()")
		e.rest.set(x, ok(0))
		e.issued(t, `{"id":"`+x+`","want":1}`)
		if r := e.reserve(t, x); r.GoneAt != nil {
			t.Errorf("gone at %v, want NULL", r.GoneAt)
		}
	})
}
