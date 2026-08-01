package s3

import (
	"context"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/common"
	j "github.com/webtor-io/web-ui/jobs"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	co "github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/libfs"
	s3 "github.com/webtor-io/web-ui/services/s3"
	"github.com/webtor-io/web-ui/services/web"
)

// CredentialsPath holds the profile-side buttons. It is deliberately not a
// child of s3.MountPath: everything under the endpoint is a bucket to an S3
// client, and gin cannot mix a catch-all with sibling routes anyway.
const CredentialsPath = "/s3-credentials"

var scopes = []string{s3.ScopeRead, s3.ScopeWrite}

type Handler struct {
	at *at.AccessToken
	sh *s3.Handler
}

func RegisterHandler(c *cli.Context, r *gin.Engine, pg *cs.PG, ats *at.AccessToken, sapi *api.Api, jobs *j.Jobs) {
	if c.Bool(co.DisableS3Flag) {
		return
	}
	// Same tree as WebDAV (services/libfs): the folders a user sees over S3 and
	// over WebDAV are the same objects, by construction.
	h := &Handler{
		at: ats,
		sh: s3.New(libfs.New(pg, sapi, jobs), s3.SigningSecret(c), s3.MountPath),
	}

	cr := r.Group(CredentialsPath)
	cr.Use(auth.HasAuth)
	cr.Use(claims.IsPaid)
	cr.POST("/generate", h.generateCredentials)
	cr.POST("/regenerate", h.regenerateCredentials)

	// The protocol itself authenticates with SigV4, so it gets its own
	// authorization middleware instead of at.HasScope/claims.IsPaid — clients
	// need an S3 error document, not a bare 401.
	gr := r.Group(s3.MountPath)
	gr.Use(h.authorize)
	gr.Match(common.AnyMethods, "", h.handle)
	gr.Match(common.AnyMethods, "/*rest", h.handle)
}

// authorize checks who is calling. The signature itself is verified later, in
// the protocol handler, which still has the untouched request URI.
func (s *Handler) authorize(c *gin.Context) {
	// Answer the "you are at the wrong door" case before anything about
	// credentials: a WebDAV client here has no access key to be wrong about.
	if err := s3.WrongProtocolError(c.Request.Method); err != nil {
		s.abort(c, err)
		return
	}
	key := s3.RequestAccessKeyID(c.Request)
	if key == "" {
		s.abort(c, s3.AnonymousError())
		return
	}
	// The middleware above derives the token from the signature and drops
	// anything the caller supplied, so these must agree. Re-checking here keeps
	// the invariant local to the place that acts on it: if they ever diverge,
	// the request would be authenticated as one account and signed by another.
	if key != c.Query(co.AccessTokenParamName) {
		s.abort(c, s3.NewError(http.StatusForbidden, s3.ErrCodeInvalidAccessKey,
			"The access key id you provided does not exist in our records", nil))
		return
	}
	u := auth.GetUserFromContext(c)
	if u == nil || !u.HasAuth() {
		s.abort(c, s3.NewError(http.StatusForbidden, s3.ErrCodeInvalidAccessKey,
			"The access key id you provided does not exist in our records", nil))
		return
	}
	scope, _ := c.Request.Context().Value(at.TokenScope{}).([]string)
	if !hasScope(scope, s3.ScopeRead) {
		s.abort(c, s3.NewError(http.StatusForbidden, s3.ErrCodeAccessDenied,
			"This access key is not allowed to use the S3 API", nil))
		return
	}
	// Tokens are issued with both scopes today, but the write path has to check
	// its own — otherwise a future read-only key would silently be able to
	// delete torrents.
	if isWrite(c.Request.Method) && !hasScope(scope, s3.ScopeWrite) {
		s.abort(c, s3.NewError(http.StatusForbidden, s3.ErrCodeAccessDenied,
			"This access key is read-only", nil))
		return
	}
	if cl := claims.GetFromContext(c); cl != nil && cl.Context.Tier.Id == 0 {
		s.abort(c, s3.NewError(http.StatusForbidden, s3.ErrCodeAccessDenied,
			"The S3 API is available on paid plans only", nil))
		return
	}
	c.Next()
}

func (s *Handler) handle(c *gin.Context) {
	uri, err := url.Parse(c.Request.RequestURI)
	if err != nil {
		s.abort(c, s3.NewError(http.StatusBadRequest, s3.ErrCodeInvalidRequest, "Malformed request URI", err))
		return
	}
	u := auth.GetUserFromContext(c)
	ctx := context.WithValue(c.Request.Context(), web.Context{}, web.NewContext(c))
	ctx = s3.WithUser(ctx, u.ID.String())
	c.Request = c.Request.WithContext(ctx)
	// Hand the protocol layer the URI as the client sent it: middleware in front
	// of us rewrote both the path and the query, and the signature covers both.
	c.Request.URL = uri
	s.sh.ServeHTTP(c.Writer, c.Request)
}

func (s *Handler) generateCredentials(c *gin.Context) {
	if _, err := s.at.Generate(c, s3.TokenName, scopes); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to generate s3 credentials"))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.s3CredentialsGenerated")
}

// regenerateCredentials rotates the access key. Destructive: every client
// configured with the old key stops authenticating at once, and because the
// secret is derived from the key, it changes too.
func (s *Handler) regenerateCredentials(c *gin.Context) {
	if _, err := s.at.Regenerate(c, s3.TokenName, scopes); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to regenerate s3 credentials"))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.s3CredentialsRegenerated")
}

func (s *Handler) abort(c *gin.Context, e *s3.Error) {
	s3.WriteError(c.Writer, c.Request, e)
	c.Abort()
}

func isWrite(method string) bool {
	switch method {
	case http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

func hasScope(scope []string, want string) bool {
	for _, s := range scope {
		if s == want {
			return true
		}
	}
	return false
}
