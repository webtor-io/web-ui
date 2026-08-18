package auth

import (
	"context"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/supertokens/supertokens-golang/recipe/session"
	"github.com/supertokens/supertokens-golang/supertokens"
	"github.com/urfave/cli"

	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/adminauth"
)

// supertokensTestInitOnce brings up the minimal SuperTokens global state
// GetAllCORSHeaders (called from RegisterHandler's SuperTokens branch)
// needs to not panic. supertokens.Init is idempotent process-wide — a
// second call is a no-op — so one Once shared by every test in this file is
// enough; nothing here ever talks to a real SuperTokens core, Init does no
// network I/O of its own.
var supertokensTestInitOnce sync.Once

func ensureSupertokensInitializedForTest(t *testing.T) {
	t.Helper()
	supertokensTestInitOnce.Do(func() {
		apiBasePath := "/auth"
		websiteBasePath := "/auth"
		err := supertokens.Init(supertokens.TypeInput{
			Supertokens: &supertokens.ConnectionInfo{
				ConnectionURI: "http://localhost:9999",
			},
			AppInfo: supertokens.AppInfo{
				AppName:         "webtor-test",
				APIDomain:       "http://localhost",
				WebsiteDomain:   "http://localhost",
				APIBasePath:     &apiBasePath,
				WebsiteBasePath: &websiteBasePath,
			},
			RecipeList: []supertokens.Recipe{
				session.Init(nil),
			},
		})
		if err != nil {
			t.Fatalf("supertokens.Init: %v", err)
		}
	})
}

// noPasswordRepo is a HashRepo stub that reports no stored hash, so
// adminauth.NewStore("", noPasswordRepo{}) is unconfigured.
type noPasswordRepo struct{}

func (noPasswordRepo) Get(ctx context.Context) (string, error)    { return "", nil }
func (noPasswordRepo) Set(ctx context.Context, hash string) error { return nil }

// withSession wires the same session middleware the app uses, so the tests
// exercise the real cookie plumbing rather than a stub.
func withSession(r *gin.Engine) {
	store := cookie.NewStore([]byte("test secret"))
	r.Use(sessions.Sessions("session", store))
}

// The whole feature must be inert wherever SuperTokens is configured — that is
// production. A regression here would put a login form in front of every
// webtor.io user.
func TestAdminPasswordInertWithSupertokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// A password IS configured here. That is the point: the SuperTokens check
	// must win over a perfectly valid password, otherwise dropping the check
	// would go unnoticed.
	a := &Auth{
		hasSupetokens: true,
		adminStore:    adminauth.NewStore("some password", nil),
	}

	if a.adminPasswordActive(nil) {
		t.Error("the admin-password branch activated while SuperTokens is configured")
	}
}

// TestSupertokensBranchNotBypassed pins the check that actually protects
// production: `if !s.hasSupetokens` at the top of RegisterHandler.
// adminPasswordActive (pinned above) has no production caller — it exists
// for handlers added in a later task. The thing that decides which branch a
// live request takes is this one, and nothing exercised it before this test:
// weakening it to always take the self-hosted branch left the rest of the
// package green.
//
// It does not send a request through the SuperTokens branch — that needs a
// real supertokens.Init(), out of scope here. Instead it counts the
// middlewares RegisterHandler actually wires onto the engine: the
// SuperTokens branch always adds exactly three (CORS, the SuperTokens
// wrapper, verifySession) and falls through; the self-hosted branch adds
// exactly one and returns immediately. If the gate collapses the two
// branches into one, that count changes.
func TestSupertokensBranchNotBypassed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ensureSupertokensInitializedForTest(t)
	r := gin.New()

	a := &Auth{
		hasSupetokens: true,
		pg:            &cs.PG{},
		adminStore:    adminauth.NewStore("some password", nil),
	}

	before := len(r.Handlers)
	a.RegisterHandler(r)
	got := len(r.Handlers) - before

	const wantSupertokensMiddlewareCount = 3
	if got != wantSupertokensMiddlewareCount {
		t.Errorf("RegisterHandler wired %d middleware(s) while hasSupetokens=true, want %d — the self-hosted branch ran instead of the SuperTokens one", got, wantSupertokensMiddlewareCount)
	}
}

// fakePGListener stands in for Postgres so registerAdminUser's DB call is
// observable without a real database. registerAdminUser takes a concrete
// *pg.DB (models.GetOrCreateUser has no interface seam), so proving it ran
// means proving it attempted a query. go-pg dials lazily on first query;
// closing the accepted connection immediately turns that first query into a
// fast, deterministic failure instead of a hang or a multi-second retry
// loop (retries are disabled by the caller via postgres-max-retries=0).
// What is observed is not a successful admin login — it is that the
// session mark made registerAdminUser attempt the query it always attempts
// when called.
func fakePGListener(t *testing.T) (pgHandle *cs.PG, connected <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	ch := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			select {
			case ch <- struct{}{}:
			default:
			}
			_ = conn.Close()
		}
	}()

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, fl := range cs.RegisterPGFlags(nil) {
		fl.Apply(fs)
	}
	if err := fs.Parse([]string{
		"-postgres-host", host,
		"-postgres-port", port,
		"-postgres-max-retries", "0",
	}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	return cs.NewPG(cli.NewContext(nil, fs, nil)), ch
}

// TestAdminSessionMarkActivatesAdmin pins the "password configured and the
// session carries the login mark" branch:
// `if adminSessionActive(c) { s.registerAdminUser(c) }`. Deleting that call
// leaves a user who supplied the correct password stuck anonymous forever —
// an endless bounce back to the login form. See fakePGListener for why this
// checks "was the DB queried" rather than "did the login succeed".
func TestAdminSessionMarkActivatesAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pgHandle, connected := fakePGListener(t)

	r := gin.New()
	withSession(r)
	// Stand in for a prior successful password login (Task 5's concern):
	// mark the session directly.
	r.Use(func(c *gin.Context) {
		sessions.Default(c).Set(AdminSessionKey, true)
		c.Next()
	})

	a := &Auth{
		hasSupetokens: false,
		pg:            pgHandle,
		adminStore:    adminauth.NewStore("some password", nil),
	}
	a.RegisterHandler(r)
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Error("registerAdminUser never queried the database — the admin-authenticated session mark did not activate the admin branch")
	}
}

func TestOpenInstanceWhenNoPasswordConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	withSession(r)

	// Exercise the real !hasSupetokens branch of RegisterHandler so the
	// IsOpenInstanceContext value actually gets set — reading IsOpenInstance
	// without ever running that middleware would trivially stay false and
	// prove nothing.
	a := &Auth{
		hasSupetokens: false,
		pg:            &cs.PG{},
		adminStore:    adminauth.NewStore("", noPasswordRepo{}),
	}
	a.RegisterHandler(r)

	var open, admin bool
	r.Use(func(c *gin.Context) {
		open = IsOpenInstance(c)
		admin = IsAdmin(c)
	})
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if !open {
		t.Error("IsOpenInstance is false while no password is configured")
	}
	_ = admin
}
