package embed

import (
	j "github.com/webtor-io/web-ui/handlers/job"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/template"
)

type Handler struct {
	tb   template.Builder[*web.Context]
	cl   *http.Client
	jobs *j.Handler
	ds   *embed.DomainSettings
	api  *api.Api
}

func RegisterHandler(cl *http.Client, r *gin.Engine, tm *template.Manager[*web.Context], jobs *j.Handler, ds *embed.DomainSettings, sapi *api.Api) {
	h := &Handler{
		tb:   tm.MustRegisterViews("embed/*"),
		jobs: jobs,
		ds:   ds,
		cl:   cl,
		api:  sapi,
	}
	r.GET("/embed", h.get)
	r.POST("/embed", h.post)
}
