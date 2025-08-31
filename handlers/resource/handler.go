package resource

import (
	"strings"

	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/handlers/job"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	api  *api.Api
	jobs *j.Handler
	tb   template.Builder[*web.Context]
	pg   *cs.PG
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], api *api.Api, jobs *j.Handler, pg *cs.PG) {
	helper := NewHelper()
	h := &Handler{
		api:  api,
		jobs: jobs,
		tb:   tm.MustRegisterViews("resource/*").WithHelper(helper).WithLayout("main"),
		pg:   pg,
	}
	r.POST("/", h.post)
	r.GET("/:resource_id", func(c *gin.Context) {
		if strings.HasPrefix(c.Param("resource_id"), "magnet") {
			h.post(c)
			return
		}
		h.get(c)
	})
}
