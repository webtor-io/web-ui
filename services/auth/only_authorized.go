package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// OnlyAuthorized requires an authenticated user for every route registered
// after it. It exists for the self-hosted deployment: there, a resource page
// is reachable by anyone who knows the hash, so an instance with an
// administrator password would still hand out its content to strangers.
// webtor.io leaves the flag off and is unaffected.
//
// Two properties are load-bearing.
//
// First, gin applies Use() only to routes registered afterwards. Static
// assets and the login form are registered before this middleware goes in
// (see serve.go), which is what keeps the login page reachable and styled
// for a visitor who is not signed in yet. Move this call earlier and the
// login form loses its stylesheet along with its ability to log anyone in.
//
// Second, the exempt prefixes are for surfaces that authenticate by their
// own mechanics — the JSON API's key, the Stremio addon's token, the S3
// signature. They are named explicitly rather than inferred, so adding a new
// page route cannot accidentally opt out of the gate: anything unlisted is
// closed.
func OnlyAuthorized(exempt ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if isExempt(c.Request.URL.Path, exempt) {
			c.Next()
			return
		}
		HasAuth(c)
	}
}

// isExempt reports whether path falls under one of the prefixes. Matching is
// on a path boundary: "/s3" covers "/s3" and "/s3/bucket" but not
// "/s3cret-page", so a route cannot escape the gate by sharing a few leading
// characters with an exempt surface.
func isExempt(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if path == p || strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}
