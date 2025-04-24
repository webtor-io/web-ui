package library

import (
	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb  template.Builder[*web.Context]
	api *api.Api
	pg  *cs.PG
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], api *api.Api, pg *cs.PG) {
	h := &Handler{
		tb:  tm.MustRegisterViews("library/*").WithLayout("main"),
		api: api,
		pg:  pg,
	}
	r.GET("/lib", h.index)
	r.POST("/lib/add", h.add)
	r.POST("/lib/remove", h.remove)
}
