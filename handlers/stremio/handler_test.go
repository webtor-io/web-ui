package stremio

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestResolveRouteAcceptsHEAD guards the binge-watching fix: Stremio validates
// a stream's playback URL with a HEAD request before auto-playing the next
// episode (see docs/stremio.md). Gin does not auto-register HEAD for a GET
// route, so the route MUST be declared for both methods — otherwise the probe
// 404s and Stremio drops to the source-selection screen instead of binge-ing.
//
// The route registration here mirrors RegisterHandler. A bad JWT short-circuits
// the handler at the parse step (401) before LinkResolver is touched, so no DB
// or backend wiring is needed to prove the route is reachable under HEAD.
func TestResolveRouteAcceptsHEAD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{secret: "test-secret"}
	r := gin.New()
	r.Match([]string{http.MethodGet, http.MethodHead}, "/stremio/resolve/*data", h.resolve)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, "/stremio/resolve/not-a-jwt", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code == http.StatusNotFound {
			t.Fatalf("%s /stremio/resolve returned 404 — route not matched; the binge HEAD probe must reach the handler", method)
		}
		// Bad JWT must be rejected by the handler itself (not the router),
		// proving the request was actually dispatched to h.resolve.
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s /stremio/resolve with invalid JWT = %d, want %d", method, w.Code, http.StatusUnauthorized)
		}
	}
}
