package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	proto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/libapi"
)

const testKey = "99999999-8888-7777-6666-555555555555"

// newAuthTestServer wires the real key middleware and the real authorize
// middleware, and fakes only what the access-token middleware would have put
// into the request context for a valid key.
func newAuthTestServer(t *testing.T, scope []string, tierID uint32, known bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	libapi.RegisterAPIKeyMiddleware(r, libapi.MountPath)
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if known {
			ctx = context.WithValue(ctx, auth.UserContext{}, &models.User{UserID: uuid.NewV4(), Email: "u@example.com"})
			ctx = context.WithValue(ctx, at.TokenScope{}, scope)
		}
		ctx = context.WithValue(ctx, claims.Context{}, &claims.Data{
			Context: &proto.Context{Tier: &proto.Tier{Id: tierID, Name: "test"}},
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	h := &Handler{}
	gr := r.Group(libapi.MountPath)
	gr.Use(h.authorize)
	gr.GET("/library", func(c *gin.Context) { c.Status(http.StatusOK) })
	gr.POST("/library", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func do(r *gin.Engine, method string, path string, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var res libapi.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("body is not an error document: %q", w.Body.String())
	}
	return res.Error.Code
}

func TestAuthorizeAcceptsAValidKey(t *testing.T) {
	r := newAuthTestServer(t, []string{libapi.ScopeRead, libapi.ScopeWrite}, 1, true)
	if w := do(r, http.MethodGet, libapi.MountPath+"/library", testKey); w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

// A client that forgot its key needs to be told that, not handed an empty 403 —
// "no key" and "wrong plan" are fixed differently.
func TestAuthorizeRejectsAnonymous(t *testing.T) {
	r := newAuthTestServer(t, nil, 1, false)
	w := do(r, http.MethodGet, libapi.MountPath+"/library", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if got := errCode(t, w); got != libapi.CodeUnauthorized {
		t.Errorf("code = %q, want %q", got, libapi.CodeUnauthorized)
	}
}

// A key that the access-token middleware did not resolve to a user is an
// unknown key, not a server error.
func TestAuthorizeRejectsUnknownKey(t *testing.T) {
	r := newAuthTestServer(t, nil, 1, false)
	w := do(r, http.MethodGet, libapi.MountPath+"/library", testKey)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (%s)", w.Code, w.Body.String())
	}
}

func TestAuthorizeRequiresAPaidPlan(t *testing.T) {
	r := newAuthTestServer(t, []string{libapi.ScopeRead, libapi.ScopeWrite}, 0, true)
	w := do(r, http.MethodGet, libapi.MountPath+"/library", testKey)
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402 (%s)", w.Code, w.Body.String())
	}
	if got := errCode(t, w); got != libapi.CodePaymentRequired {
		t.Errorf("code = %q, want %q", got, libapi.CodePaymentRequired)
	}
}

// Keys are issued with both scopes today, so this guards the future read-only
// key rather than a case that exists — which is the point: without it, adding
// one would silently grant deletes.
func TestAuthorizeRejectsWriteWithoutScope(t *testing.T) {
	r := newAuthTestServer(t, []string{libapi.ScopeRead}, 1, true)
	if w := do(r, http.MethodGet, libapi.MountPath+"/library", testKey); w.Code != http.StatusOK {
		t.Errorf("read with api:read = %d, want 200", w.Code)
	}
	w := do(r, http.MethodPost, libapi.MountPath+"/library", testKey)
	if w.Code != http.StatusForbidden {
		t.Fatalf("write with api:read = %d, want 403", w.Code)
	}
	if got := errCode(t, w); got != libapi.CodeForbidden {
		t.Errorf("code = %q, want %q", got, libapi.CodeForbidden)
	}
}

// A key with no API scope at all (e.g. the WebDAV or Stremio token, which are
// rows of the same shape) must not open this door.
func TestAuthorizeRejectsForeignScope(t *testing.T) {
	r := newAuthTestServer(t, []string{"webdav:read", "webdav:write"}, 1, true)
	if w := do(r, http.MethodGet, libapi.MountPath+"/library", testKey); w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// The docs are wired to a named swag instance (see docsInstanceName). A
// mismatch between the name the spec registers under and the one the UI asks
// for produces an empty reference at runtime and nothing at build time — this
// is the check that would have caught it.
func TestDocsServeTheSpec(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerDocs(r, libapi.MountPath, "https://api.example.com/v1", "/api-credentials/key")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, libapi.MountPath+"/swagger.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("spec status = %d, want 200", w.Code)
	}
	var spec struct {
		Host     string                    `json:"host"`
		BasePath string                    `json:"basePath"`
		Paths    map[string]map[string]any `json:"paths"`
		Defs     map[string]map[string]any `json:"definitions"`
		Security map[string]map[string]any `json:"securityDefinitions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("spec is not JSON: %v", err)
	}
	// The endpoint the deployment actually serves, not whatever was baked in.
	if spec.Host != "api.example.com" || spec.BasePath != "/v1" {
		t.Errorf("spec points at %q%q, want api.example.com/v1", spec.Host, spec.BasePath)
	}
	for _, p := range []string{"/resource/{resource_id}", "/resource/{resource_id}/list", "/resource/{resource_id}/export/{content_id}", "/library", "/vault", "/profile"} {
		if _, ok := spec.Paths[p]; !ok {
			t.Errorf("spec is missing %s", p)
		}
	}
	if _, ok := spec.Security["BearerAuth"]; !ok {
		t.Error("spec does not document the bearer scheme")
	}

	// The UI itself: a 200 with the swagger-ui shell, not a 404 from gin.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, libapi.MountPath+"/docs/index.html", nil))
	if w.Code != http.StatusOK {
		t.Errorf("docs status = %d, want 200", w.Code)
	}
}

// Past the burst the answer must be a 429 error document with Retry-After —
// and scoped to the offending key: one abusive integration must not starve
// the rest.
func TestAuthorizeRateLimitsPerKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	libapi.RegisterAPIKeyMiddleware(r, libapi.MountPath)
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, auth.UserContext{}, &models.User{UserID: uuid.NewV4(), Email: "u@example.com"})
		ctx = context.WithValue(ctx, at.TokenScope{}, []string{libapi.ScopeRead})
		ctx = context.WithValue(ctx, claims.Context{}, &claims.Data{
			Context: &proto.Context{Tier: &proto.Tier{Id: 1, Name: "test"}},
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	h := &Handler{limiter: libapi.NewRateLimiterWith(1, 2)}
	gr := r.Group(libapi.MountPath)
	gr.Use(h.authorize)
	gr.GET("/library", func(c *gin.Context) { c.Status(http.StatusOK) })

	for i := 0; i < 2; i++ {
		if w := do(r, http.MethodGet, libapi.MountPath+"/library", testKey); w.Code != http.StatusOK {
			t.Fatalf("request %d within burst: status = %d (%s)", i+1, w.Code, w.Body.String())
		}
	}
	w := do(r, http.MethodGet, libapi.MountPath+"/library", testKey)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (%s)", w.Code, w.Body.String())
	}
	if code := errCode(t, w); code != libapi.CodeRateLimited {
		t.Errorf("code = %q, want %q", code, libapi.CodeRateLimited)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header is missing")
	}

	otherKey := "11111111-2222-3333-4444-555555555555"
	if w := do(r, http.MethodGet, libapi.MountPath+"/library", otherKey); w.Code != http.StatusOK {
		t.Errorf("another key hit the spent bucket: status = %d (%s)", w.Code, w.Body.String())
	}
}
