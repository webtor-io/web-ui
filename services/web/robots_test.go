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
	r.Use(NoindexDefault(false))
	r.GET("/profile", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/sitemap.xml", func(c *gin.Context) { c.String(http.StatusOK, "<xml/>") })
	r.GET("/robots.txt", func(c *gin.Context) { c.String(http.StatusOK, "User-agent: *") })
	r.GET("/favicon.ico", func(c *gin.Context) { c.String(http.StatusOK, "ico") })
	r.GET("/favicon.svg", func(c *gin.Context) { c.String(http.StatusOK, "svg") })
	r.GET("/favicon-32x32.png", func(c *gin.Context) { c.String(http.StatusOK, "png") })
	r.GET("/android-chrome-192x192.png", func(c *gin.Context) { c.String(http.StatusOK, "png") })
	r.GET("/apple-touch-icon.png", func(c *gin.Context) { c.String(http.StatusOK, "png") })
	r.GET("/manifest.webmanifest", func(c *gin.Context) { c.String(http.StatusOK, "{}") })
	r.GET("/webtor.jpg", func(c *gin.Context) { c.String(http.StatusOK, "jpg") })

	cases := []struct {
		path string
		want string
	}{
		{"/profile", "noindex, follow"},
		{"/sitemap.xml", ""},
		{"/robots.txt", ""},
		{"/favicon.ico", ""},
		{"/favicon.svg", ""},
		{"/favicon-32x32.png", ""},
		{"/android-chrome-192x192.png", ""},
		{"/apple-touch-icon.png", ""},
		{"/manifest.webmanifest", ""},
		{"/webtor.jpg", ""},
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
	r.Use(NoindexDefault(false))
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

func TestStagingForcesNoindexEverywhere(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NoindexDefault(true))
	indexable := r.Group("", IndexFollow())
	indexable.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "home") })
	r.GET("/profile", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/favicon.ico", func(c *gin.Context) { c.String(http.StatusOK, "ico") })

	for _, path := range []string{"/", "/profile", "/favicon.ico"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		got := w.Header().Get("X-Robots-Tag")
		if got != "noindex" {
			t.Errorf("path %q: X-Robots-Tag = %q, want %q", path, got, "noindex")
		}
	}
}
