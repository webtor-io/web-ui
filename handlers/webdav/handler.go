package webdav

import (
	"context"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/common"
	j "github.com/webtor-io/web-ui/jobs"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	co "github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/web"
	webdav "github.com/webtor-io/web-ui/services/webdav"
)

type Handler struct {
	pg   *cs.PG
	at   *at.AccessToken
	sapi *api.Api
	wh   *webdav.Handler
}

func RegisterHandler(c *cli.Context, r *gin.Engine, pg *cs.PG, at *at.AccessToken, sapi *api.Api, jobs *j.Jobs) {
	if c.Bool(co.DisableWebDAVFlag) {
		return
	}
	fs := NewFileSystem(pg, sapi, jobs, "webdav")
	wh := &webdav.Handler{FileSystem: fs}
	h := &Handler{
		pg:   pg,
		at:   at,
		sapi: sapi,
		wh:   wh,
	}

	gr := r.Group("/webdav")
	gr.Use(auth.HasAuth)
	gr.Use(claims.IsPaid)
	gr.POST("/url/generate", h.generateUrl)

	// WebDAV protocol routes - these require token-based authentication
	grapi := gr.Group("")
	grapi.Use(at.HasScope("webdav:read", "webdav:write"))
	grapi.Match(common.AnyMethods, "/fs/*rest", h.handleWebDAV)
}

func (s *Handler) generateUrl(c *gin.Context) {
	_, err := s.at.Generate(c, "webdav", []string{"webdav:read", "webdav:write"})
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "WebDAV URL generated")
}

func (s *Handler) handleWebDAV(c *gin.Context) {
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), web.Context{}, web.NewContext(c)))
	u, err := url.Parse(c.Request.RequestURI)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	c.Request.URL.Path = u.Path
	s.wh.ServeHTTP(c.Writer, c.Request)
}
