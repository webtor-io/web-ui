package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// corsEngineFor builds the same CORS handler RegisterHandler installs, over
// the origin allowlist a given configured domain produces.
//
// The handler is reconstructed here rather than driven through
// RegisterHandler because that function also initialises SuperTokens, which
// needs a live core. The policy under test — which origins get credentials —
// is entirely the two lines below.
func corsEngineFor(domain string) http.Handler {
	a := &Auth{domain: domain}
	allowed := map[string]bool{}
	for _, h := range a.corsOrigins() {
		allowed["https://"+h] = true
		allowed["http://"+h] = true
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(cors.New(cors.Config{
		AllowOriginFunc:  func(origin string) bool { return allowed[origin] },
		AllowMethods:     []string{"GET", "POST", "DELETE", "PUT", "OPTIONS"},
		AllowHeaders:     []string{"content-type"},
		AllowCredentials: true,
	}))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "page") })
	return r
}

// TestCredentialedCORSIsNotGrantedToArbitraryOrigins is the negative control
// for the origin allowlist.
//
// gin-contrib/cors reflects the caller's Origin into
// Access-Control-Allow-Origin whenever an AllowOriginFunc is configured, and
// emits Access-Control-Allow-Credentials unconditionally. So an origin func
// that returns true for everything hands any website the ability to read
// authenticated page bodies — and the gin session cookie is SameSite=None for
// the embed flow, so it rides those requests.
//
// Negative control: make corsOrigins return every host, or restore
// `AllowOriginFunc: func(string) bool { return true }`, and the foreign-origin
// cases below must fail.
func TestCredentialedCORSIsNotGrantedToArbitraryOrigins(t *testing.T) {
	h := corsEngineFor("webtor.io")

	for _, tt := range []struct {
		name        string
		origin      string
		wantAllowed bool
	}{
		{name: "own origin over https", origin: "https://webtor.io", wantAllowed: true},
		{name: "own origin over http", origin: "http://webtor.io", wantAllowed: true},
		{name: "unrelated site", origin: "https://evil.example", wantAllowed: false},
		{name: "suffix lookalike", origin: "https://webtor.io.evil.example", wantAllowed: false},
		{name: "prefix lookalike", origin: "https://notwebtor.io", wantAllowed: false},
		{name: "subdomain not configured", origin: "https://evil.webtor.io", wantAllowed: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", tt.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			acao := w.Header().Get("Access-Control-Allow-Origin")
			acac := w.Header().Get("Access-Control-Allow-Credentials")

			if tt.wantAllowed {
				if acao != tt.origin {
					t.Errorf("origin %q: Access-Control-Allow-Origin = %q, want the origin back", tt.origin, acao)
				}
				return
			}
			if acao != "" {
				t.Errorf("origin %q was reflected into Access-Control-Allow-Origin as %q", tt.origin, acao)
			}
			if acac == "true" {
				t.Errorf("origin %q was granted credentials", tt.origin)
			}
		})
	}
}

// TestUnsetDomainGrantsNoCredentialedOrigin pins the fail-closed direction: a
// deployment that never configured its domain must deny cross-origin
// credentialed reads rather than allow them all.
func TestUnsetDomainGrantsNoCredentialedOrigin(t *testing.T) {
	h := corsEngineFor("")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if acao := w.Header().Get("Access-Control-Allow-Origin"); acao != "" {
		t.Errorf("with no domain configured, Access-Control-Allow-Origin = %q, want empty", acao)
	}
}

// TestCorsOriginsNormalisesAConfiguredScheme guards the shape of the
// configuration value: DOMAIN is a bare host in production, but a value
// carrying a scheme must not silently produce origins like
// "https://https://webtor.io", which would match nothing and disable the
// site's own credentialed requests.
func TestCorsOriginsNormalisesAConfiguredScheme(t *testing.T) {
	for _, in := range []string{"webtor.io", "https://webtor.io", "http://webtor.io", "https://webtor.io/"} {
		got := (&Auth{domain: in}).corsOrigins()
		if len(got) != 1 || got[0] != "webtor.io" {
			t.Errorf("corsOrigins(%q) = %v, want [webtor.io]", in, got)
		}
	}
}
