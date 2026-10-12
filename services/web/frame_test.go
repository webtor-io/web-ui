package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFrameAncestors(t *testing.T) {
	if FrameAncestors("") != nil {
		t.Fatal("empty sources must not install the middleware")
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.UseRawPath = true // as web.New sets it
	r.Use(FrameAncestors("'self'"))
	for _, p := range []string{"/", "/ext/magnet", "/embed", "/show", "/embed/example", "/:resource_id"} {
		r.GET(p, func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	}
	for _, c := range []struct {
		path string
		want string
	}{
		{"/", "frame-ancestors 'self'"},
		// What a site that lost /embed would frame instead.
		{"/ext/magnet?url=magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10", "frame-ancestors 'self'"},
		{"/0123456789abcdef0123456789abcdef01234567", "frame-ancestors 'self'"},
		{"/a/b/c", "frame-ancestors 'self'"}, // no route: still covered
		{"/embed/example", "frame-ancestors 'self'"},
		// Decodes to /embed, but the resource catch-all serves it.
		{"/%65mbed?id=x", "frame-ancestors 'self'"},
		{"/sh%6Fw?id=x", "frame-ancestors 'self'"},
		// Framed by design: the SDK's iframe and where it lands.
		{"/show?id=x", ""},
		{"/embed?id=x", ""},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, c.path, nil))
		if got := w.Header().Get("Content-Security-Policy"); got != c.want {
			t.Errorf("%s: Content-Security-Policy %q, want %q", c.path, got, c.want)
		}
	}
}
