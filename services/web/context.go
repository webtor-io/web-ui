package web

import (
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/handlers/geo"
	"github.com/webtor-io/web-ui/handlers/session"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/geoip"
	"github.com/webtor-io/web-ui/services/i18n"
)

// userSettingsContextKey is the gin-context key the user-settings
// middleware (services/user_settings/middleware.go) writes into. We
// keep the constant here (the consumer side) so the middleware can
// stay decoupled from web.Context's internal layout.
const userSettingsContextKey = "web.user_settings"

const vaultEnabledContextKey = "web.vault_enabled"

// SetVaultEnabled records whether the vault service is configured. The navbar
// link and the /vault routes must agree: the routes are registered only when
// the vault API client exists, so an ungated link is a 404.
func SetVaultEnabled(c *gin.Context, enabled bool) {
	c.Set(vaultEnabledContextKey, enabled)
}

// vaultEnabled defaults to false when nothing set it, so a caller that forgets
// hides the link rather than rendering a broken one.
func vaultEnabled(c *gin.Context) bool {
	v, ok := c.Get(vaultEnabledContextKey)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// The onboarding checklist travels on Context because the navbar counter needs
// it on every page, and the navbar renders from Context rather than from any
// handler's data struct.
//
// It is resolved lazily: the middleware only stores a closure, and the query
// runs the first time a template actually asks. The middleware is global, so
// without that every poster image, Stremio addon poll and WebDAV request from
// a signed-in user would pay for a checklist nobody is rendering.
const (
	onboardingResolverKey = "web.onboarding_resolver"
	onboardingContextKey  = "web.onboarding"
)

// OnboardingResolver produces the checklist for the current request.
type OnboardingResolver func() *models.OnboardingChecklist

type Context struct {
	Data         any
	CSRF         string
	SessionID    string
	ErrKey       string
	User         *auth.User
	Claims       *claims.Data
	TierUpdated  bool
	Geo          *geoip.Data
	ApiClaims    *api.Claims
	UserSettings *models.UserSettings
	Lang         string
	Path         string
	// OpenInstance is true on a self-hosted instance running without an
	// administrator password: anyone who can reach the port is admin. The
	// banner rendered from this is the only thing standing between the
	// instance and a stranger, so it is deliberately hard to ignore.
	OpenInstance bool
	// Vault reports whether the vault service is configured.
	Vault  bool
	ginCtx *gin.Context
}

func (c *Context) WithData(obj any) *Context {
	nc := *c
	nc.Data = obj
	return &nc
}

// WithErrKey sets an i18n error key directly (e.g. from query params).
func (c *Context) WithErrKey(key string) *Context {
	nc := *c
	nc.ErrKey = key
	return &nc
}

// WithErr classifies the error into an i18n key and logs the original.
func (c *Context) WithErr(err error) *Context {
	nc := *c
	nc.ErrKey = ClassifyError(err)
	return &nc
}

func (s *Context) GetGinContext() *gin.Context {
	return s.ginCtx
}

func NewContext(c *gin.Context) *Context {
	user := auth.GetUserFromContext(c)
	cl := claims.GetFromContext(c)
	sess := session.GetFromContext(c)
	if sess == nil {
		// Defensive: the session middleware normally populates this for
		// every request, but the centralized error handler can render a
		// Context for a request that failed before it ran. Fall back to an
		// empty session (anonymous, no CSRF) instead of panicking.
		sess = &session.Session{}
	}
	geoData := geo.GetFromContext(c)
	aCl := api.GetClaimsFromContext(c)
	tu := claims.GetTierUpdateFromContext(c)
	lang := i18n.GetLang(c)
	path := c.Request.URL.Path
	us, _ := c.Get(userSettingsContextKey)
	settings, _ := us.(*models.UserSettings)

	return &Context{
		CSRF:         sess.CSRF,
		User:         user,
		Claims:       cl,
		ApiClaims:    aCl,
		SessionID:    sess.ID,
		Geo:          geoData,
		TierUpdated:  tu,
		UserSettings: settings,
		Lang:         lang,
		Path:         path,
		OpenInstance: auth.IsOpenInstance(c),
		Vault:        vaultEnabled(c),
		ginCtx:       c,
	}
}

// SetUserSettings stashes the loaded UserSettings into the gin
// context. Called by the user-settings middleware right after the
// auth middleware runs so any downstream handler that wraps the
// request in a web.Context gets the value populated. Exposed (rather
// than direct c.Set) so the context-key constant stays internal.
func SetUserSettings(c *gin.Context, us *models.UserSettings) {
	c.Set(userSettingsContextKey, us)
}

// SetOnboardingResolver registers how to build the checklist for this request.
// Written by the onboarding middleware; nothing is computed until a template
// reads Context.Onboarding.
func SetOnboardingResolver(c *gin.Context, r OnboardingResolver) {
	c.Set(onboardingResolverKey, r)
}

// Onboarding returns the activation checklist, or nil when there is nothing to
// show. It is a method rather than a field so the work is deferred to the
// templates that actually use it — Go templates call it exactly like a field,
// so `{{ if .Onboarding }}` reads the same either way.
//
// The result is memoised on the gin context, not on Context itself: handlers
// build several Context values per request (WithData, WithErrKey), and the home
// page renders both the navbar counter and the card from them.
func (c *Context) Onboarding() *models.OnboardingChecklist {
	if c.ginCtx == nil {
		return nil
	}
	// Memo first: once resolved for this request, the answer stands.
	if v, ok := c.ginCtx.Get(onboardingContextKey); ok {
		cl, _ := v.(*models.OnboardingChecklist)
		return cl
	}
	v, ok := c.ginCtx.Get(onboardingResolverKey)
	if !ok {
		return nil
	}
	resolve, ok := v.(OnboardingResolver)
	if !ok {
		return nil
	}
	cl := resolve()
	c.ginCtx.Set(onboardingContextKey, cl)
	return cl
}
