package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNoindexDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NoindexDefault())
	r.GET("/profile", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/sitemap.xml", func(c *gin.Context) { c.String(http.StatusOK, "<xml/>") })
	r.GET("/robots.txt", func(c *gin.Context) { c.String(http.StatusOK, "User-agent: *") })

	cases := []struct {
		path string
		want string
	}{
		{"/profile", "noindex, follow"},
		{"/sitemap.xml", ""},
		{"/robots.txt", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		got := w.Header().Get("X-Robots-Tag")
		if got != tc.want {
			t.Errorf("path %q: X-Robots-Tag = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestIndexFollowOverridesDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NoindexDefault())
	indexable := r.Group("", IndexFollow())
	indexable.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "home") })
	indexable.GET("/torrent-player", func(c *gin.Context) { c.String(http.StatusOK, "tool") })

	for _, path := range []string{"/", "/torrent-player"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		got := w.Header().Get("X-Robots-Tag")
		if got != "index, follow" {
			t.Errorf("path %q: X-Robots-Tag = %q, want %q", path, got, "index, follow")
		}
	}
}
