package libapi

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	co "github.com/webtor-io/web-ui/services/common"
)

// AuthorizationHeader / APIKeyHeader are the two ways a key may arrive. Bearer
// is the one documented in Swagger; X-Api-Key exists because it survives
// environments that strip Authorization (some corporate proxies do).
const (
	AuthorizationHeader = "Authorization"
	APIKeyHeader        = "X-Api-Key"
)

// RegisterAPIKeyMiddleware bridges the API key to the existing access-token
// chain by putting it into the token query param that services/access_token
// reads. Everything downstream — auth.UserContext, claims, web.Context — then
// works exactly as it does for WebDAV and S3, with no API-specific plumbing.
//
// It MUST be registered before services/access_token's own middleware: that one
// is global, so by the time a group middleware could run, the user would already
// have been resolved (or not) for this request.
//
// **The token param is always rewritten on this endpoint, never merged.** A
// caller who could smuggle `?token=<someone else's key>` past a request carrying
// its own key would be authenticated as that account — so the caller-supplied
// value is deleted first, unconditionally, even when no key is present at all.
// Covered by TestSuppliedTokenCannotOverrideHeader.
func RegisterAPIKeyMiddleware(r *gin.Engine, mountPath string) {
	mountPath = strings.TrimSuffix(mountPath, "/")
	r.Use(func(c *gin.Context) {
		p := c.Request.URL.Path
		if p != mountPath && !strings.HasPrefix(p, mountPath+"/") {
			c.Next()
			return
		}
		q := c.Request.URL.Query()
		q.Del(co.AccessTokenParamName)
		// Only feed the chain something it can parse: it treats a malformed
		// token as a server error, and a typo in a curl command must not read
		// as "webtor is broken".
		if key := RequestAPIKey(c.Request); key != "" {
			if _, err := uuid.FromString(key); err == nil {
				q.Set(co.AccessTokenParamName, key)
			}
		}
		c.Request.URL.RawQuery = q.Encode()
		c.Next()
	})
}

// RequestAPIKey returns the key a request carries, or "" when it carries none.
func RequestAPIKey(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(AuthorizationHeader)); v != "" {
		// Scheme is case-insensitive per RFC 7235; clients get it wrong.
		if len(v) >= 7 && strings.EqualFold(v[:7], "bearer ") {
			return strings.TrimSpace(v[7:])
		}
		return ""
	}
	return strings.TrimSpace(r.Header.Get(APIKeyHeader))
}

// Hosts returns the hostnames configured to serve the API at their root.
func Hosts(c *cli.Context) []string {
	var out []string
	for _, h := range strings.Split(c.String(co.APIDomainFlag), ",") {
		if h = strings.ToLower(strings.TrimSpace(hostOnly(h))); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// hostOnly reduces a URL or Host header value to a bare hostname: scheme, path
// and port stripped. The port matters — a Host header carries one whenever the
// service is not on 443, and matching would silently fail without this.
func hostOnly(s string) string {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return s
}

// RegisterHostMiddleware serves the API at the root of its own hostnames
// (api.webtor.io) by rewriting their requests onto the mount path and
// re-dispatching.
//
// **The version stays in the URL**: api.webtor.io/v1/library maps onto
// /api/v1/library, not api.webtor.io/library. Dropping it would leave the
// dedicated host with no way to address a future /api/v2, and clients pinned to
// "the host" rather than to a version is exactly the breakage the version
// prefix exists to prevent.
//
// Doing it here rather than as an nginx rewrite keeps one moving part: the
// session middleware's CSRF exemption and the access-key middleware are both
// keyed on the /api prefix this produces, so a proxy-side rewrite would have to
// be kept in sync with two things it cannot see. Registering it also requires
// exempting these hosts from the canonical-domain redirect, or every API
// request answers 302.
func RegisterHostMiddleware(r *gin.Engine, hosts []string, prefix string) {
	if len(hosts) == 0 {
		return
	}
	prefix = strings.TrimSuffix(prefix, "/")
	set := map[string]bool{}
	for _, h := range hosts {
		set[strings.ToLower(h)] = true
	}
	r.Use(func(c *gin.Context) {
		if !set[strings.ToLower(hostOnly(c.Request.Host))] {
			c.Next()
			return
		}
		p := c.Request.URL.Path
		// Already rewritten: this is the re-dispatched pass, and rewriting
		// again would loop.
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			c.Next()
			return
		}
		p = prefix + p
		c.Request.URL.Path = p
		c.Request.URL.RawPath = co.EscapePath(p)
		c.Abort()
		r.HandleContext(c)
	})
}

// HasScope reports whether a token's scope list grants want.
func HasScope(scope []string, want string) bool {
	for _, s := range scope {
		if s == want {
			return true
		}
	}
	return false
}
