package embed

import (
	"net/http"

	"github.com/urfave/cli"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/template"
)

type Handler struct {
	tb      template.Builder[*web.Context]
	cl      *http.Client
	jobs    *j.Jobs
	ds      *embed.DomainSettings
	api     *api.Api
	enabled bool
}

// RegisterHandler mounts /embed even when the instance does not serve embeds:
// iframes already placed on other sites keep loading it, and they must get an
// explanation. Without the route GET /embed fell through to the resource
// catch-all and drew this site's 404 page inside the other site's frame.
func RegisterHandler(c *cli.Context, cl *http.Client, r *gin.Engine, tm *template.Manager[*web.Context], jobs *j.Jobs, ds *embed.DomainSettings, sapi *api.Api) {
	h := &Handler{
		tb:      tm.MustRegisterViews("embed/*"),
		jobs:    jobs,
		ds:      ds,
		cl:      cl,
		api:     sapi,
		enabled: embed.Enabled(c),
	}
	r.GET("/embed", h.get)
	r.POST("/embed", h.post)
}
