package s3

import (
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
