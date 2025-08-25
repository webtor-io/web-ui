package webdav

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/handlers/job"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/web"
	webdav "github.com/webtor-io/web-ui/services/webdav"
)

type Handler struct {
	pg   *cs.PG
	at   *at.AccessToken
	sapi *api.Api
	wh   *webdav.Handler
}

func RegisterHandler(r *gin.Engine, pg *cs.PG, at *at.AccessToken, sapi *api.Api, jobs *job.Handler) {
	fs := NewFileSystem(pg, sapi, jobs)
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
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) handleWebDAV(c *gin.Context) {
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), web.Context{}, web.NewContext(c)))
	c.Request.URL.Path = strings.TrimPrefix(c.Param("rest"), "/webdav")
	s.wh.ServeHTTP(c.Writer, c.Request)
}
