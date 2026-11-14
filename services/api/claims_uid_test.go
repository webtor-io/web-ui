package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
)

const testUserID = "01234567-89ab-cdef-0123-456789abcdef"

// uidEngine puts a signed-in user into the context the way
// services/access_token does for an API key or a WebDAV/S3/Stremio token,
// then runs the api middleware. "/embed" swaps the claims for the embedding
// domain's as handlers/embed does.
func uidEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-User") != "" {
			u := &models.User{UserID: uuid.FromStringOrNil(testUserID)}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContext{}, u))
		}
	})
	s := &Api{domain: "webtor.example", expire: 1}
	s.RegisterHandler(r)
	r.GET("/", func(c *gin.Context) {
		c.Header("X-Uid", GetClaimsFromContext(c).Uid)
	})
	r.GET("/embed", func(c *gin.Context) {
		c, err := s.SetClaims(c, "embed.example", nil, "sid")
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Header("X-Uid", GetClaimsFromContext(c).Uid)
	})
	return r
}

func TestRegisterHandler_UidOnlyForTheSignedIn(t *testing.T) {
	r := uidEngine()
	for _, tc := range []struct {
		name     string
		signedIn bool
		path     string
		want     string
	}{
		{`signed in, "/"`, true, "/", sha1hex(testUserID)},
		{`anonymous, "/"`, false, "/", ""},
		// The embed never gets uid, even for a webtor.io user: its claims
		// may be the embedding domain owner's.
		{`signed in, "/embed"`, true, "/embed", ""},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.signedIn {
			req.Header.Set("X-Test-User", "1")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.name, w.Code)
		}
		if got := w.Header().Get("X-Uid"); got != tc.want {
			t.Errorf("%s: uid = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func parseTestToken(t *testing.T, a *Api, tok string) jwt.MapClaims {
	t.Helper()
	p, err := jwt.Parse(tok, func(*jwt.Token) (interface{}, error) { return []byte(a.secret), nil })
	if err != nil || !p.Valid {
		t.Fatalf("token did not validate: %v", err)
	}
	return p.Claims.(jwt.MapClaims)
}

func TestInternalClaims(t *testing.T) {
	a := newTestApi(t)
	tok, err := a.SignClaims(InternalClaims())
	if err != nil {
		t.Fatal(err)
	}
	m := parseTestToken(t, a, tok)
	if m["internal"] != true {
		t.Errorf("internal = %v, want true", m["internal"])
	}
	if _, ok := m["uid"]; ok {
		t.Errorf("internal token carries uid: %+v", m)
	}
	// Not forever: an hour.
	exp, err := m.GetExpirationTime()
	if err != nil || exp == nil {
		t.Fatalf("no exp: %v", err)
	}
	if d := time.Until(exp.Time); d < time.Hour-5*time.Second || d > time.Hour+5*time.Second {
		t.Errorf("exp in %v, want ~1h", d)
	}
	// An anonymous token is what it was: neither claim.
	anon, err := a.SignClaims(&Claims{SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	am := parseTestToken(t, a, anon)
	for _, k := range []string{"uid", "internal"} {
		if _, ok := am[k]; ok {
			t.Errorf("anonymous token carries %q: %+v", k, am)
		}
	}
}
