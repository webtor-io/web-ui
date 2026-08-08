package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func docsEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerDocs(r, "/api/v1", "http://localhost:8080/api/v1")
	return r
}

func getDocs(r *gin.Engine, path string, requestURI string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	req.RequestURI = requestURI
	r.ServeHTTP(w, req)
	return w
}

// The Swagger UI page is only as alive as its assets: the HTML always renders,
// so a broken bundle shows up as a blank page, not an error.
func TestDocsServesUIAssets(t *testing.T) {
	r := docsEngine()
	for _, p := range []string{
		"/api/v1/docs/index.html",
		"/api/v1/docs/swagger-ui-bundle.js",
		"/api/v1/docs/swagger-ui-standalone-preset.js",
		"/api/v1/docs/swagger-ui.css",
	} {
		if w := getDocs(r, p, p); w.Code != 200 || w.Body.Len() == 0 {
			t.Errorf("%s: got %d (%d bytes), want 200 with content", p, w.Code, w.Body.Len())
		}
	}
}

// A middleware that rewrites URL.Path but not RequestURI (the i18n lang prefix,
// the api-host rewrite) must not break asset serving: ginSwagger derives the
// webdav strip-prefix from the FIRST matched request's RequestURI and freezes
// it on a package-global, so one mismatched request used to 404 every asset
// until process restart.
func TestDocsSurvivesRewrittenFirstRequest(t *testing.T) {
	r := docsEngine()

	// First request arrives as /ru/... with the prefix already stripped from
	// URL.Path, exactly as the i18n middleware leaves it.
	if w := getDocs(r, "/api/v1/docs/index.html", "/ru/api/v1/docs/index.html"); w.Code != 200 {
		t.Fatalf("rewritten index.html: got %d, want 200", w.Code)
	}

	p := "/api/v1/docs/swagger-ui-bundle.js"
	if w := getDocs(r, p, p); w.Code != 200 || w.Body.Len() == 0 {
		t.Errorf("%s after rewritten first request: got %d (%d bytes), want 200 with content", p, w.Code, w.Body.Len())
	}
}

// The key prefill rides on the initializer and must stay off everything else —
// on index.html it would run twice, on the bundle it would corrupt a file
// served with the webdav path.
func TestDocsPrefillInjectedIntoInitializerOnly(t *testing.T) {
	r := docsEngine()

	p := "/api/v1/docs/swagger-initializer.js"
	w := getDocs(r, p, p)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "preauthorizeApiKey") {
		t.Errorf("initializer: got %d, prefill present=%v, want 200 with prefill",
			w.Code, strings.Contains(w.Body.String(), "preauthorizeApiKey"))
	}
	if !strings.Contains(w.Body.String(), CredentialsPath+"/key") {
		t.Errorf("prefill does not fetch %s/key", CredentialsPath)
	}

	p = "/api/v1/docs/index.html"
	if w := getDocs(r, p, p); strings.Contains(w.Body.String(), "preauthorizeApiKey") {
		t.Errorf("prefill leaked into index.html")
	}
}

// The key endpoint must never answer without a session: its body is a secret,
// and the in-handler auth check (not just the middleware) is what this pins.
func TestCredentialsKeyRejectsAnonymous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handler{}
	r.GET(CredentialsPath+"/key", h.getCredentialsKey)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", CredentialsPath+"/key", nil)
	r.ServeHTTP(w, req)
	if w.Code != 401 || w.Body.Len() != 0 {
		t.Errorf("anonymous key request: got %d (%d bytes), want 401 with empty body", w.Code, w.Body.Len())
	}
}
