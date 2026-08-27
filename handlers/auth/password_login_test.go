package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/adminauth"
	svcauth "github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/libapi"
)

type memRepo struct{ hash string }

func (m *memRepo) Get(_ context.Context) (string, error) { return m.hash, nil }
func (m *memRepo) Set(_ context.Context, h string) error { m.hash = h; return nil }

// alwaysActive stands in for auth.Auth.AdminPasswordActive in tests that
// only care about passwordLogin's own logic (verification, rate limiting,
// return-url handling) and not the SuperTokens gate — see
// TestPasswordFormGateIsRespected for the test that exercises the real gate.
func alwaysActive(*gin.Context) bool { return true }

func newLoginRouter(t *testing.T, store *adminauth.Store) *gin.Engine {
	t.Helper()
	return newLoginRouterWithGate(t, store, alwaysActive)
}

func newLoginRouterWithGate(t *testing.T, store *adminauth.Store, active func(*gin.Context) bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test secret"))))
	h := &Handler{adminStore: store, loginLimiter: libapi.NewRateLimiterWith(0.2, 5), passwordFormActive: active}
	r.GET("/login", h.login)
	r.POST("/login", h.passwordLogin)
	return r
}

func post(r *gin.Engine, password string) *httptest.ResponseRecorder {
	return postWithCookie(r, password, "")
}

// postWithCookie lets a test carry the session cookie a previous request
// received (via Set-Cookie) into a follow-up request, so it can prove
// something set by one request (return-url, the admin mark) is honoured or
// cleared by the next.
func postWithCookie(r *gin.Engine, password, cookieHeader string) *httptest.ResponseRecorder {
	form := url.Values{"password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func getLogin(r *gin.Engine, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/login?"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// sessionCookie pulls the "session=..." cookie out of a response's Set-Cookie
// headers, in the form a Cookie request header expects.
func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("no session cookie in response (Set-Cookie: %v)", w.Header().Values("Set-Cookie"))
	return ""
}

func TestPasswordLoginAcceptsCorrectPassword(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))

	w := post(r, "the right password")
	if w.Code != http.StatusFound {
		t.Fatalf("status: got %d, want 302 (body %q)", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.Join(w.Header().Values("Set-Cookie"), " "), "session=") {
		t.Error("no session cookie was set after a successful login")
	}
}

func TestPasswordLoginRejectsWrongPassword(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))

	w := post(r, "the wrong password")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if w.Header().Get("Location") != "" {
		t.Error("a failed login still issued a redirect")
	}
}

// Without a limit, a public instance is one script away from a password.
func TestPasswordLoginRateLimitsAttempts(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))

	var got429 bool
	for i := 0; i < 12; i++ {
		if post(r, "guess").Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("twelve wrong passwords in a row never hit the rate limit")
	}
}

// A visitor bounced off a protected page (Task 5's HasAuth) lands on
// /login?return-url=<path>, which the GET handler stashes in the session. A
// successful password login must send them back there, not just to "/".
func TestPasswordLoginRedirectsToStoredReturnURL(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))

	getW := getLogin(r, "return-url="+url.QueryEscape("/profile"))
	cookie := sessionCookie(t, getW)

	postW := postWithCookie(r, "the right password", cookie)
	if postW.Code != http.StatusFound {
		t.Fatalf("status: got %d, want 302 (body %q)", postW.Code, postW.Body.String())
	}
	if got := postW.Header().Get("Location"); got != "/profile" {
		t.Errorf("Location: got %q, want %q", got, "/profile")
	}

	// The stored return-url must be cleared once used — otherwise a second
	// login (e.g. after visiting /logout and signing back in) would keep
	// redirecting to a page the visitor never asked to return to this time.
	secondCookie := sessionCookie(t, postW)
	secondW := postWithCookie(r, "the right password", secondCookie)
	if got := secondW.Header().Get("Location"); got != "/" {
		t.Errorf("return-url was not cleared: second login redirected to %q, want %q", got, "/")
	}
}

// The common case — no bounce, someone just visits /login directly — must
// still land somewhere sane.
func TestPasswordLoginDefaultsToRootWithoutReturnURL(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))

	w := post(r, "the right password")
	if got := w.Header().Get("Location"); got != "/" {
		t.Errorf("Location: got %q, want %q", got, "/")
	}
}

// A correct password must not silently "succeed" (302) when the session
// itself can't actually be persisted — the visitor would be bounced right
// back to /login on the next request with no explanation. Trigger a genuine
// securecookie encode failure (its default MaxLength is 4096 bytes) rather
// than mocking Save.
//
// return-url can't be used to pad the session for this: passwordLogin reads
// and *deletes* it before calling Save (see the "clear it" comment there), so
// any padding stashed under that key is gone by the time Save runs and never
// gets a chance to push the encoded size over the limit. Instead this test
// registers its own /test/pad route that sets an unrelated session key
// passwordLogin never touches, primes the session just under 4096 bytes
// through it (verified empirically — see task-6-report.md for the
// calibration), and only then lets passwordLogin's own addition of
// AdminSessionKey push the total over the edge.
func TestPasswordLoginSessionSaveFailureReturns500(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))
	r.GET("/test/pad", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("pad", strings.Repeat("a", 2200))
		_ = session.Save()
	})

	padW := httptest.NewRecorder()
	r.ServeHTTP(padW, httptest.NewRequest(http.MethodGet, "/test/pad", nil))
	if !strings.Contains(strings.Join(padW.Header().Values("Set-Cookie"), " "), "session=") {
		t.Fatalf("setup failed: /test/pad did not persist a session (Set-Cookie: %v)", padW.Header().Values("Set-Cookie"))
	}
	cookie := sessionCookie(t, padW)

	postW := postWithCookie(r, "the right password", cookie)
	if postW.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500 (body %q)", postW.Code, postW.Body.String())
	}
	if postW.Header().Get("Location") != "" {
		t.Error("a save failure still issued a redirect")
	}
}

// The whole password branch must be inert wherever SuperTokens is
// configured — that is production. This drives the gate through the real
// auth.Auth.AdminPasswordActive (via NewForAdminPasswordTest), not a stub,
// so a regression in that method itself — not just in whether the handler
// remembers to call it — turns this test red too.
func TestPasswordFormGateIsRespected(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	store := adminauth.NewStore("", &memRepo{hash: h})

	// A password IS configured (store above). That's the point: SuperTokens
	// must win over a perfectly valid password, otherwise dropping the check
	// would go unnoticed.
	supertokensAuth := svcauth.NewForAdminPasswordTest(true, store)
	r := newLoginRouterWithGate(t, store, supertokensAuth.AdminPasswordActive)

	// identityManagedExternally mirrors NewForAdminPasswordTest(true, ...)
	// above: this is the SuperTokens-configured world, and loginView reads
	// the field rather than re-deriving it.
	handler := &Handler{adminStore: store, passwordFormActive: supertokensAuth.AdminPasswordActive, identityManagedExternally: true}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/login", nil)
	if view, _ := handler.loginView(c); view != "auth/login" {
		t.Errorf("loginView: got %q, want %q — SuperTokens should win over a configured password", view, "auth/login")
	}

	w := post(r, "the right password")
	if w.Code != http.StatusNotFound {
		t.Errorf("POST /login with SuperTokens configured: got %d, want 404 (correct password must not authenticate)", w.Code)
	}
	if strings.Contains(strings.Join(w.Header().Values("Set-Cookie"), " "), "session=") {
		t.Error("POST /login with SuperTokens configured set a session cookie — it must not authenticate")
	}
}

// TestLoginViewOffersNothingWhenNothingCanAuthenticate covers the third
// state, which used to fall through to the SuperTokens view: no local
// password and no external provider. That view loads the SuperTokens client,
// which then talks to an endpoint no such instance has -- so an open
// instance answered /login with a form that could not work and a failed
// fetch to whatever DOMAIN names. An empty view name is loginView's way of
// saying "send them home"; the handler redirects on it.
func TestLoginViewOffersNothingWhenNothingCanAuthenticate(t *testing.T) {
	// No password in the store, and no SuperTokens: an open instance.
	store := adminauth.NewStore("", &memRepo{})
	openAuth := svcauth.NewForAdminPasswordTest(false, store)

	handler := &Handler{adminStore: store, passwordFormActive: openAuth.AdminPasswordActive}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/login", nil)

	view, _ := handler.loginView(c)
	if view != "" {
		t.Errorf("loginView: got %q, want \"\" -- with neither a password nor a provider there is nothing to sign in to, and %q ships the SuperTokens client to a browser with no server for it", view, view)
	}
}
