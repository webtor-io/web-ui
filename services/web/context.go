package web

import (
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/handlers/geo"
	"github.com/webtor-io/web-ui/handlers/session"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/geoip"
	"github.com/webtor-io/web-ui/services/i18n"
)

type Context struct {
	Data        any
	CSRF        string
	SessionID   string
	Err         error
	User        *auth.User
	Claims      *claims.Data
	TierUpdated bool
	Geo         *geoip.Data
	ApiClaims   *api.Claims
	Lang        string
	ginCtx      *gin.Context
}

func (c *Context) WithData(obj any) *Context {
	nc := *c
	nc.Data = obj
	return &nc
}

func (c *Context) WithErr(err error) *Context {
	nc := *c
	nc.Err = err
	return &nc
}

func (s *Context) GetGinContext() *gin.Context {
	return s.ginCtx
}

// Path returns the current request path (after language prefix stripping).
func (c *Context) Path() string {
	return c.ginCtx.Request.URL.Path
}

func NewContext(c *gin.Context) *Context {
	user := auth.GetUserFromContext(c)
	cl := claims.GetFromContext(c)
	sess := session.GetFromContext(c)
	geoData := geo.GetFromContext(c)
	aCl := api.GetClaimsFromContext(c)
	tu := claims.GetTierUpdateFromContext(c)
	lang := i18n.GetLang(c)

	return &Context{
		CSRF:        sess.CSRF,
		User:        user,
		Claims:      cl,
		ApiClaims:   aCl,
		SessionID:   sess.ID,
		Geo:         geoData,
		TierUpdated: tu,
		Lang:        lang,
		ginCtx:      c,
	}
}
