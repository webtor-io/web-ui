package web

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// NoindexDefault sets X-Robots-Tag: noindex, follow on every response.
// Routes listed in sitemap.xml opt in to indexing via IndexFollow,
// which overrides the header (last c.Header() call wins before flush).
// sitemap.xml and robots.txt are exempted — search engines consume them
// as directives, not as pages to index. Favicons, PWA manifest, and the
// og:image are also exempted: Google's favicon crawler honors
// X-Robots-Tag on image responses and will drop the site icon from
// search results otherwise.
func NoindexDefault() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isIndexableAsset(c.Request.URL.Path) {
			c.Next()
			return
		}
		c.Header("X-Robots-Tag", "noindex, follow")
		c.Next()
	}
}

func isIndexableAsset(path string) bool {
	switch path {
	case "/sitemap.xml", "/robots.txt",
		"/favicon.ico", "/favicon.svg",
		"/manifest.webmanifest",
		"/webtor.jpg":
		return true
	}
	if strings.HasPrefix(path, "/favicon-") ||
		strings.HasPrefix(path, "/android-chrome-") ||
		strings.HasPrefix(path, "/apple-touch-icon") {
		return true
	}
	return false
}

// IndexFollow overrides the default noindex with index, follow. Apply
// only to routes that appear in sitemap.xml (homepage, tool pages,
// /about). Language-prefixed variants (/ru/, /es/, ...) are stripped
// to bare paths by the i18n HTTP middleware before Gin routing, so
// wrapping the canonical route covers all language variants.
func IndexFollow() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Robots-Tag", "index, follow")
		c.Next()
	}
}
