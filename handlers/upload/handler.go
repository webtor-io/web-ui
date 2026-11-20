// Package upload serves the routes of an account's own uploads.
package upload

import (
	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/thumbnail"
	"github.com/webtor-io/web-ui/services/uploads"
	"github.com/webtor-io/web-ui/services/vault"
)

type Handler struct {
	up    *uploads.Service
	api   *api.Api
	vault *vault.Vault // nil: Vault is not configured
	pg    *cs.PG
	thumb *thumbnail.Service // nil: no thumbnail bucket
}

// RegisterHandler mounts the upload routes. serve calls it only with uploads
// on, after the claims and api middleware: a token is cut from the viewer's
// claims.
func RegisterHandler(r *gin.Engine, up *uploads.Service, a *api.Api, v *vault.Vault, pg *cs.PG, thumb *thumbnail.Service) {
	h := &Handler{up: up, api: a, vault: v, pg: pg, thumb: thumb}
	r.POST("/upload/token", auth.HasAuth, h.token)
	r.POST("/upload/delete", auth.HasAuth, h.delete)
}
