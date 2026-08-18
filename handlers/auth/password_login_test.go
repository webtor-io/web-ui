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
	"github.com/webtor-io/web-ui/services/libapi"
)

type memRepo struct{ hash string }

func (m *memRepo) Get(_ context.Context) (string, error) { return m.hash, nil }
func (m *memRepo) Set(_ context.Context, h string) error { m.hash = h; return nil }

func newLoginRouter(t *testing.T, store *adminauth.Store) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test secret"))))
	h := &Handler{adminStore: store, loginLimiter: libapi.NewRateLimiterWith(0.2, 5)}
	r.POST("/login", h.passwordLogin)
	return r
}

func post(r *gin.Engine, password string) *httptest.ResponseRecorder {
	form := url.Values{"password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
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
