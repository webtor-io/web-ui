package embed

import (
	"net/http"

	"github.com/urfave/cli"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/template"
)

type Handler struct {
	tb   template.Builder[*web.Context]
	cl   *http.Client
	jobs *j.Jobs
	ds   *embed.DomainSettings
	api  *api.Api
}

func RegisterHandler(c *cli.Context, cl *http.Client, r *gin.Engine, tm *template.Manager[*web.Context], jobs *j.Jobs, ds *embed.DomainSettings, sapi *api.Api) {
	if c.Bool(common.DisableEmbedFlag) {
		return
	}
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
