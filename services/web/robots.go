package web

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// stagingCtxKey marks the request as handled by a staging deployment so
// that IndexFollow (which runs later, per-route) knows not to override
// the forced noindex.
const stagingCtxKey = "webStaging"

// NoindexDefault sets X-Robots-Tag: noindex, follow on every response.
// Routes listed in sitemap.xml opt in to indexing via IndexFollow,
// which overrides the header (last c.Header() call wins before flush).
// sitemap.xml and robots.txt are exempted — search engines consume them
// as directives, not as pages to index. Favicons, PWA manifest, and the
// og:image are also exempted: Google's favicon crawler honors
// X-Robots-Tag on image responses and will drop the site icon from
// search results otherwise.
//
// With staging=true every response gets noindex — no IndexFollow
// opt-ins, no asset exemptions — so a staging host can never enter the
// search index. Crawling must stay allowed (no robots.txt Disallow):
// a crawler that is blocked from fetching a page never sees the header.
func NoindexDefault(staging bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if staging {
			c.Set(stagingCtxKey, true)
			c.Header("X-Robots-Tag", "noindex")
			c.Next()
			return
		}
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
// On staging deployments the override is suppressed: everything stays
// noindex.
func IndexFollow() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool(stagingCtxKey) {
			c.Next()
			return
		}
		c.Header("X-Robots-Tag", "index, follow")
		c.Next()
	}
}
