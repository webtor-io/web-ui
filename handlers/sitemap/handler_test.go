package sitemap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// The sitemap lists pages meant to rank. The /trial short link (a redirect
// to a checkout) and the Stremio paywall clips under /pub/stremio are not
// such pages; this pins that nobody adds them.
func TestSitemapLeavesOutTheTrialLinkAndThePaywallClips(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{baseURL: "https://webtor.io"}
	r := gin.New()
	r.GET("/sitemap.xml", h.sitemap)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, p := range []string{"/trial", "/pub/", ".mp4"} {
		if strings.Contains(body, p) {
			t.Errorf("sitemap lists %q", p)
		}
	}
	if !strings.Contains(body, "<loc>https://webtor.io/about</loc>") {
		t.Error("sitemap did not render its pages — the check above proves nothing")
	}
}
