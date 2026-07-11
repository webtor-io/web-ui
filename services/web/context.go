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
	ginCtx       *gin.Context
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

