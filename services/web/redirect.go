package web

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RedirectNonCanonical 302-redirects any request whose Host header does
// not match the canonical domain to the same URI on redirectDomain.
// Used on staging: the deployment answers on its own hostname only,
// while the legacy public hostname (webtor.cc) still resolves to it and
// must bounce visitors to production. Implemented here rather than as
// an ingress-nginx redirect annotation because the controller's
// annotation validation rejects $request_uri, and a variable-free
// redirect would drop the path.
//
// 302, not 301: browsers cache 301 per-URL indefinitely, which would
// keep bouncing clients even after the hostnames are reshuffled.
// Returns nil when redirectDomain is empty (production).
func RedirectNonCanonical(domain string, redirectDomain string) gin.HandlerFunc {
	if redirectDomain == "" {
		return nil
	}
	target := strings.TrimSuffix(redirectDomain, "/")
	canonical := hostOnly(domain)
	return func(c *gin.Context) {
		if strings.EqualFold(hostOnly(c.Request.Host), canonical) {
			c.Next()
			return
		}
		c.Redirect(http.StatusFound, target+c.Request.RequestURI)
		c.Abort()
	}
}

// hostOnly extracts a bare lowercase hostname from a URL or Host
// header value: scheme, path, and port are stripped.
func hostOnly(s string) string {
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.ToLower(s)
}
