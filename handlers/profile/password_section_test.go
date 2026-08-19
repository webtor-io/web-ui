package profile

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
)

// newPasswordRouter wires the same session middleware production does
// (services/session/handler.go's RegisterHandler) so setPassword's
// sessions.Default(c) call has something to work with — a bare gin.New()
// engine has no session store registered and panics the moment the handler
// touches it.
func newPasswordRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test secret"))))
	r.POST("/profile/password", h.setPassword)
	return r
}

type memRepo struct{ hash string }

func (m *memRepo) Get(_ context.Context) (string, error) { return m.hash, nil }
func (m *memRepo) Set(_ context.Context, h string) error { m.hash = h; return nil }

func postPassword(r *gin.Engine, current, next string) *httptest.ResponseRecorder {
	form := url.Values{"current": {current}, "new": {next}}
	req := httptest.NewRequest(http.MethodPost, "/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSetPasswordOnAnOpenInstance(t *testing.T) {
	repo := &memRepo{}
	store := adminauth.NewStore("", repo)
	h := &Handler{adminStore: store, selfHosted: true}
	r := newPasswordRouter(h)

	if w := postPassword(r, "", "a brand new password"); w.Code != http.StatusFound {
		t.Fatalf("status: got %d, want 302 (body %q)", w.Code, w.Body.String())
	}
	if !store.Verify(context.Background(), "a brand new password") {
		t.Error("the password was not stored")
	}
}

// Changing an existing password without proving you know it turns a stolen
// session into a permanent takeover.
func TestChangePasswordRequiresTheCurrentOne(t *testing.T) {
	existing, err := adminauth.Hash("the old password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	repo := &memRepo{hash: existing}
	store := adminauth.NewStore("", repo)
	h := &Handler{adminStore: store, selfHosted: true}
	r := newPasswordRouter(h)

	// A wrong current password still redirects (the profile POST handlers all
	// bounce back to GET /profile), so 302 alone can't distinguish "refused"
	// from "changed" here — both success and refusal use it. What does
	// distinguish them is the query-string error key the handler attaches on
	// refusal (see setPassword's "?err=auth.password.wrongCurrent"), and,
	// decisively, whether the password store actually changed.
	if w := postPassword(r, "not the old one", "a brand new password"); !strings.Contains(w.Header().Get("Location"), "err=") {
		t.Errorf("wrong current password was not reported as a failure; redirected to %q", w.Header().Get("Location"))
	}
	if store.Verify(context.Background(), "a brand new password") {
		t.Error("the new password took effect despite a wrong current password")
	}
	if w := postPassword(r, "the old password", "a brand new password"); w.Code != http.StatusFound || strings.Contains(w.Header().Get("Location"), "err=") {
		t.Errorf("a correct current password was rejected: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestPasswordChangeRefusedWhenEnvManaged(t *testing.T) {
	store := adminauth.NewStore("env password", &memRepo{})
	h := &Handler{adminStore: store, selfHosted: true}
	r := newPasswordRouter(h)

	w := postPassword(r, "env password", "a brand new password")
	if w.Code == http.StatusFound {
		t.Error("the profile changed a password that is managed by ADMIN_PASSWORD")
	}
}

// This is the route's other guard, independent of the current-password
// check: even a well-formed request must not be able to touch the admin
// password on a SuperTokens (production) deployment. adminauth's Postgres
// repo also fails closed here on its own (it only ever touches a row that
// the self-hosted auto-admin flow creates), but that is a second layer, not
// a substitute for this one.
func TestPasswordChangeRefusedWhenNotSelfHosted(t *testing.T) {
	repo := &memRepo{}
	store := adminauth.NewStore("", repo)
	h := &Handler{adminStore: store, selfHosted: false}
	r := newPasswordRouter(h)

	w := postPassword(r, "", "a brand new password")
	if w.Code == http.StatusFound {
		t.Error("the password changed on a non-self-hosted deployment")
	}
	if store.Verify(context.Background(), "a brand new password") {
		t.Error("the new password took effect on a non-self-hosted deployment")
	}
}

// sessionCookieFrom pulls the "session=..." cookie out of a response's
// Set-Cookie headers, in the form a Cookie request header expects. Mirrors
// handlers/auth's sessionCookie helper (unexported there, so duplicated
// rather than shared across packages).
func sessionCookieFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("no session cookie in response (Set-Cookie: %v)", w.Header().Values("Set-Cookie"))
	return ""
}

// Setting the first password flips adminauth.Store.IsConfigured() to true
// starting with the very next request — including the one this same visitor
// is about to make. Without also marking this session admin-authenticated,
// that next request (e.g. the redirect target, /profile) finds a configured
// store and an unmarked session and treats the person who JUST set the
// password as a stranger, bouncing them to /login. Verified via a second,
// independent request replaying the Set-Cookie the first response issued —
// not by inspecting the handler's internals — so this test fails the same
// way a real browser would notice the bug.
func TestSettingTheFirstPasswordKeepsTheAdministratorSignedIn(t *testing.T) {
	repo := &memRepo{}
	store := adminauth.NewStore("", repo)
	h := &Handler{adminStore: store, selfHosted: true}
	r := newPasswordRouter(h)
	r.GET("/test/admin-marked", func(c *gin.Context) {
		marked, _ := sessions.Default(c).Get(svcauth.AdminSessionKey).(bool)
		if marked {
			c.String(http.StatusOK, "marked")
			return
		}
		c.String(http.StatusOK, "not-marked")
	})

	w := postPassword(r, "", "a brand new password")
	if w.Code != http.StatusFound {
		t.Fatalf("status: got %d, want 302 (body %q)", w.Code, w.Body.String())
	}
	cookie := sessionCookieFrom(t, w)

	req := httptest.NewRequest(http.MethodGet, "/test/admin-marked", nil)
	req.Header.Set("Cookie", cookie)
	checkW := httptest.NewRecorder()
	r.ServeHTTP(checkW, req)
	if checkW.Body.String() != "marked" {
		t.Errorf("session was not marked admin-authenticated after setting the first password (got %q)", checkW.Body.String())
	}
}
