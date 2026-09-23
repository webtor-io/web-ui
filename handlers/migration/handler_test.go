package migration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

// /show is where extension builds up to 0.1.12 send a clicked magnet. The
// redirect must hand /ext/magnet the magnet itself as ?url=, not a second
// "url=" wrapped around it (the resource was named "url=magnet:?…" until
// 2026-09-23).
func TestShowMagnetRedirectCarriesTheMagnetUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterHandler(r)
	magnet := "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Sintel&tr=udp%3A%2F%2Ftracker.example%3A1337"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/show?"+url.Values{"magnet": {magnet}}.Encode(), nil))
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Path != "/ext/magnet" {
		t.Fatalf("redirect path = %q, want /ext/magnet", loc.Path)
	}
	if got := loc.Query().Get("url"); got != magnet {
		t.Errorf("url param = %q, want the magnet unchanged %q", got, magnet)
	}
}
