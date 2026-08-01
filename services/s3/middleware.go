package s3

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	co "github.com/webtor-io/web-ui/services/common"
)

const (
	// MountPath is the S3 endpoint users configure in their client. Path-style
	// addressing appends /<bucket>/<key> to it.
	MountPath = "/s3"

	// TokenName is the access_token row the credentials live in; Scope* are what
	// it is issued with.
	TokenName  = "s3"
	ScopeRead  = "s3:read"
	ScopeWrite = "s3:write"
)

// SigningSecret is the key every user's secret access key is derived from
// (DeriveSecretKey). It falls back to the session secret so a deployment that
// never sets it still gets stable, per-user secrets.
func SigningSecret(c *cli.Context) string {
	if s := c.String(co.S3SecretFlag); s != "" {
		return s
	}
	return c.String(co.SessionSecretFlag)
}

// RegisterAccessKeyMiddleware bridges SigV4 to the existing access-token chain
// by putting the access key id from the request signature into the token query
// param that services/access_token reads. Everything downstream —
// auth.UserContext, claims, web.Context — then works exactly as it does for
// WebDAV, with no S3-specific plumbing.
//
// It MUST be registered before services/access_token's own middleware: that one
// is global, so by the time a group middleware could run, the user would already
// have been resolved (or not) for this request.
//
// **The token param is always rewritten on this endpoint, never merged.** The
// signature is verified against the key in the signature itself, so if a caller
// could smuggle a different token in through the query string, they would be
// authenticated as its owner while signing as themselves — i.e. read any account
// whose access key id they know. Deleting the caller's value (even when the
// request carries no signature at all) is what makes the two impossible to
// separate. Covered by TestSuppliedTokenCannotOverrideSignature.
func RegisterAccessKeyMiddleware(r *gin.Engine, mountPath string) {
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
		// token as a server error, and a typo in rclone.conf must not read as
		// "webtor is broken".
		if key := RequestAccessKeyID(c.Request); key != "" {
			if _, err := uuid.FromString(key); err == nil {
				q.Set(co.AccessTokenParamName, key)
			}
		}
		c.Request.URL.RawQuery = q.Encode()
		c.Next()
	})
}

// Hosts returns the hostnames configured to serve S3 at their root.
func Hosts(c *cli.Context) []string {
	var out []string
	for _, h := range strings.Split(c.String(co.S3DomainFlag), ",") {
		if h = strings.ToLower(strings.TrimSpace(hostOnly(h))); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// PublicEndpoint is the endpoint users put in their client: the dedicated host
// when there is one, otherwise the main domain plus the mount path.
func PublicEndpoint(c *cli.Context) string {
	if hosts := Hosts(c); len(hosts) > 0 {
		return "https://" + hosts[0]
	}
	return strings.TrimSuffix(c.String(co.DomainFlag), "/") + MountPath
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

// RegisterHostMiddleware serves the S3 API at the root of its own hostnames, by
// rewriting their requests onto the mount path and re-dispatching.
//
// Two reasons this is done in the app rather than as an nginx rewrite:
//
//   - SigV4 covers the path the client sent. A proxy-side rewrite would hand us
//     /s3/bucket/key while the client signed /bucket/key, and every request
//     would fail. Rewriting here leaves r.RequestURI — which is what
//     verification reads — exactly as it arrived.
//   - It has to happen before the session middleware, whose CSRF exemption list
//     is keyed on the /s3 prefix.
//
// Registering it also requires exempting these hosts from the canonical-domain
// redirect, or every S3 request answers 302.
func RegisterHostMiddleware(r *gin.Engine, hosts []string, mountPath string) {
	if len(hosts) == 0 {
		return
	}
	mountPath = strings.TrimSuffix(mountPath, "/")
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
		if p == mountPath || strings.HasPrefix(p, mountPath+"/") {
			c.Next()
			return
		}
		p = mountPath + p
		c.Request.URL.Path = p
		c.Request.URL.RawPath = co.EscapePath(p)
		c.Abort()
		r.HandleContext(c)
	})
}

// RequestAccessKeyID returns the access key a request is signed with, reading
// the URI as the client sent it (the query is rewritten in flight, and a
// presigned request carries its credential there).
func RequestAccessKeyID(r *http.Request) string {
	u, err := url.Parse(r.RequestURI)
	if err != nil {
		return ""
	}
	return AccessKeyID(r, u.Query())
}
