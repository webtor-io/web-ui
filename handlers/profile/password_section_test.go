package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/adminauth"
)

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
	gin.SetMode(gin.TestMode)
	repo := &memRepo{}
	store := adminauth.NewStore("", repo)
	r := gin.New()
	h := &Handler{adminStore: store, selfHosted: true}
	r.POST("/profile/password", h.setPassword)

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
	gin.SetMode(gin.TestMode)
	existing, err := adminauth.Hash("the old password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	repo := &memRepo{hash: existing}
	store := adminauth.NewStore("", repo)
	r := gin.New()
	h := &Handler{adminStore: store, selfHosted: true}
	r.POST("/profile/password", h.setPassword)

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
	gin.SetMode(gin.TestMode)
	store := adminauth.NewStore("env password", &memRepo{})
	r := gin.New()
	h := &Handler{adminStore: store, selfHosted: true}
	r.POST("/profile/password", h.setPassword)

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
	gin.SetMode(gin.TestMode)
	repo := &memRepo{}
	store := adminauth.NewStore("", repo)
	r := gin.New()
	h := &Handler{adminStore: store, selfHosted: false}
	r.POST("/profile/password", h.setPassword)

	w := postPassword(r, "", "a brand new password")
	if w.Code == http.StatusFound {
		t.Error("the password changed on a non-self-hosted deployment")
	}
	if store.Verify(context.Background(), "a brand new password") {
		t.Error("the new password took effect on a non-self-hosted deployment")
	}
}
