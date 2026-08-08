package libapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegisterCORSMiddleware opens the API surface to browser callers from any
// origin. Safe because API auth is a bearer header, never a cookie: there is
// no credentialed CORS here, so a wildcard exposes nothing a curl could not
// already read. Without it the Swagger "Try it out" cannot work at all as
// soon as a dedicated host is configured — the reference on the main domain
// then calls api.<domain> cross-origin.
//
// Preflights are answered in the middleware rather than via OPTIONS routes:
// gin would otherwise 404 them (no route), and registering OPTIONS on every
// endpoint is one more thing to forget.
func RegisterCORSMiddleware(r *gin.Engine, mountPath string) {
	mountPath = strings.TrimSuffix(mountPath, "/")
	r.Use(func(c *gin.Context) {
		p := c.Request.URL.Path
		if p != mountPath && !strings.HasPrefix(p, mountPath+"/") {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", "*")
		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Api-Key")
			c.Header("Access-Control-Max-Age", "86400")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
}

// AllowedKeyOrigins are the origins the session-authenticated key endpoint may
// answer to with credentials: exactly the dedicated API hosts, where the
// Swagger page runs on a sibling origin of the session cookie's host. This is
// deliberately NOT a wildcard — the response body is a secret and credentialed
// CORS with a reflected arbitrary origin would hand it to any website.
func AllowedKeyOrigins(hosts []string) map[string]bool {
	out := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		out["https://"+h] = true
	}
	return out
}
