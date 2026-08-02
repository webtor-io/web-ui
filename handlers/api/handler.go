// Package api exposes resources, the user's library, Vault and profile over a
// documented JSON HTTP API for paid accounts.
//
// **/resource, /list and /export are pass-throughs to rest-api**, with its
// paths, parameters and response types kept verbatim (the handlers return
// rest-api's own structs). A client that already speaks rest-api speaks this,
// and the only thing that changes is the authentication and the account the
// claims are built from. Everything account-scoped — /library, /vault,
// /profile — lives only here, because rest-api knows nothing about accounts.
//
// The account-scoped halves call the same services and models the HTML handlers
// do, so behaviour cannot fork between the UI and the API.
//
// Swagger lives at /api/v1/docs/index.html; the annotations that produce it sit
// on the handlers in this package, and the generated spec is committed under
// docs/swagger.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/jobs"
	at "github.com/webtor-io/web-ui/services/access_token"
	restapi "github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	co "github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/libapi"
	usettings "github.com/webtor-io/web-ui/services/user_settings"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

// CredentialsPath holds the profile-side buttons. Deliberately not a child of
// libapi.MountPath: everything under the mount path is API surface that answers
// JSON to a token, and these two are form posts from a browser session.
const CredentialsPath = "/api-credentials"

var scopes = []string{libapi.ScopeRead, libapi.ScopeWrite}

type Handler struct {
	at           *at.AccessToken
	api          *restapi.Api
	pg           *cs.PG
	jobs         *j.Jobs
	vault        *vault.Vault
	userSettings *usettings.Service
}

func RegisterHandler(c *cli.Context, r *gin.Engine, pg *cs.PG, ats *at.AccessToken, sapi *restapi.Api, jobs *j.Jobs, v *vault.Vault, us *usettings.Service) {
	if c.Bool(co.DisableAPIFlag) {
		return
	}
	h := &Handler{
		at:           ats,
		api:          sapi,
		pg:           pg,
		jobs:         jobs,
		vault:        v,
		userSettings: us,
	}

	cr := r.Group(CredentialsPath)
	cr.Use(auth.HasAuth)
	cr.Use(claims.IsPaid)
	cr.POST("/generate", h.generateCredentials)
	cr.POST("/regenerate", h.regenerateCredentials)

	// Docs are public: the point of an API reference is that you can read it
	// before you have a key.
	registerDocs(r, libapi.MountPath, libapi.PublicEndpoint(c))

	gr := r.Group(libapi.MountPath)
	gr.Use(h.authorize)

	// rest-api's own shape, path for path. The trailing-slash variant is
	// registered because that is how rest-api documents the store endpoint, and
	// RedirectTrailingSlash is off globally — without it the documented URL
	// would 404.
	gr.POST("/resource", h.postResource)
	gr.POST("/resource/", h.postResource)
	gr.GET("/resource/:resource_id", h.getResource)
	gr.GET("/resource/:resource_id/list", h.listResource)
	gr.GET("/resource/:resource_id/export/:content_id", h.exportResource)

	gr.GET("/library", h.listLibrary)
	gr.POST("/library", h.addLibrary)
	gr.GET("/library/:resource_id", h.getLibraryItem)
	gr.PATCH("/library/:resource_id", h.renameLibrary)
	gr.DELETE("/library/:resource_id", h.deleteLibrary)

	gr.GET("/vault", h.getVault)
	gr.POST("/vault/pledges", h.postVaultPledge)
	gr.DELETE("/vault/pledges/:resource_id", h.deleteVaultPledge)

	gr.GET("/profile", h.getProfile)
	gr.PATCH("/profile", h.patchProfile)
}

// authorize answers who is calling, in JSON.
//
// It replaces at.HasScope + claims.IsPaid on this group for one reason: those
// answer with an empty body, and a client that gets a bare 403 from a JSON API
// has no way to tell "no key" from "wrong plan" — which are fixed differently.
func (s *Handler) authorize(c *gin.Context) {
	key := libapi.RequestAPIKey(c.Request)
	if key == "" {
		s.abort(c, libapi.NewError(http.StatusUnauthorized, libapi.CodeUnauthorized,
			"provide your API key as \"Authorization: Bearer <key>\" — issue one on your profile page", nil))
		return
	}
	// The middleware in front derives the token from the header and drops
	// anything the caller supplied, so these must agree. Re-checking here keeps
	// the invariant local to the place that acts on it: if they ever diverge,
	// the request would be authenticated as one account and keyed by another.
	// A malformed key never reaches the query param, so this also covers it.
	if key != c.Query(co.AccessTokenParamName) {
		s.abort(c, libapi.NewError(http.StatusUnauthorized, libapi.CodeUnauthorized, "invalid API key", nil))
		return
	}
	u := auth.GetUserFromContext(c)
	if u == nil || !u.HasAuth() {
		s.abort(c, libapi.NewError(http.StatusUnauthorized, libapi.CodeUnauthorized, "invalid API key", nil))
		return
	}
	scope, _ := c.Request.Context().Value(at.TokenScope{}).([]string)
	if !libapi.HasScope(scope, libapi.ScopeRead) {
		s.abort(c, libapi.NewError(http.StatusForbidden, libapi.CodeForbidden,
			"this key is not allowed to use the API", nil))
		return
	}
	// Keys are issued with both scopes today, but the write path has to check
	// its own — otherwise a future read-only key would silently be able to
	// delete torrents.
	if isWrite(c.Request.Method) && !libapi.HasScope(scope, libapi.ScopeWrite) {
		s.abort(c, libapi.NewError(http.StatusForbidden, libapi.CodeForbidden, "this key is read-only", nil))
		return
	}
	// Mirrors services/claims.IsPaid. A nil claims service means the deployment
	// runs without tiers (self-hosted), and then everyone is allowed.
	if cl := claims.GetFromContext(c); cl != nil && cl.Context.Tier.Id == 0 {
		s.abort(c, libapi.NewError(http.StatusPaymentRequired, libapi.CodePaymentRequired,
			"the API is available on paid plans only", nil))
		return
	}
	c.Next()
}

func (s *Handler) generateCredentials(c *gin.Context) {
	if _, err := s.at.Generate(c, libapi.TokenName, scopes); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to generate api key"))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.apiKeyGenerated")
}

// regenerateCredentials rotates the key. Destructive: every integration
// configured with the old key stops authenticating at once.
func (s *Handler) regenerateCredentials(c *gin.Context) {
	if _, err := s.at.Regenerate(c, libapi.TokenName, scopes); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to regenerate api key"))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.apiKeyRegenerated")
}

func (s *Handler) abort(c *gin.Context, e *libapi.Error) {
	libapi.WriteError(c, e)
}

func isWrite(method string) bool {
	switch method {
	case http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}
