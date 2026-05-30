package web

import (
	"github.com/gin-gonic/gin"
)

// NoindexDefault sets X-Robots-Tag: noindex, follow on every response.
// Routes listed in sitemap.xml opt in to indexing via IndexFollow,
// which overrides the header (last c.Header() call wins before flush).
// sitemap.xml and robots.txt are exempted — search engines consume them
// as directives, not as pages to index.
func NoindexDefault() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.URL.Path {
		case "/sitemap.xml", "/robots.txt":
		default:
			c.Header("X-Robots-Tag", "noindex, follow")
		}
		c.Next()
	}
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
