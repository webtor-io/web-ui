// Package upload serves the routes of an account's own uploads.
package upload

import (
	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/uploads"
)

type Handler struct {
	up  *uploads.Service
	api *api.Api
	pg  *cs.PG
}

// RegisterHandler mounts the upload routes. serve calls it only with uploads
// on, after the claims and api middleware: a token is cut from the viewer's
// claims.
func RegisterHandler(r *gin.Engine, up *uploads.Service, a *api.Api, pg *cs.PG) {
	h := &Handler{up: up, api: a, pg: pg}
	r.POST("/upload/token", auth.HasAuth, h.token)
}
