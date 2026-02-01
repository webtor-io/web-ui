package resource

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	api            *api.Api
	jobs           *j.Jobs
	tb             template.Builder[*web.Context]
	pg             *cs.PG
	vault          *vault.Vault
	useDirectLinks bool
}

func RegisterHandler(c *cli.Context, r *gin.Engine, tm *template.Manager[*web.Context], api *api.Api, jobs *j.Jobs, pg *cs.PG, v *vault.Vault) {
	helper := NewHelper()
	h := &Handler{
		api:            api,
		jobs:           jobs,
		tb:             tm.MustRegisterViews("resource/*").WithHelper(helper).WithLayout("main"),
		pg:             pg,
		vault:          v,
		useDirectLinks: c.BoolT(common.UseDirectLinks),
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
