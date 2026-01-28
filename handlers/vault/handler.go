package vault

import (
	"time"

	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	vault *vault.Vault
	pg    *cs.PG
	tb    *template.BuilderWithLayout[*web.Context]
}

type PledgeDisplay struct {
	PledgeID   string
	ResourceID string
	Resource   *vaultModels.Resource
	Amount     float64
	IsFrozen   bool
	Funded     bool
	CreatedAt  string
}

type PledgeListData struct {
	Pledges               []PledgeDisplay
	FreezePeriod          time.Duration
	ExpirePeriod          time.Duration
	TransferTimeoutPeriod time.Duration
}

func RegisterHandler(r *gin.Engine, v *vault.Vault, tm *template.Manager[*web.Context], pg *cs.PG) {
	h := &Handler{
		vault: v,
		pg:    pg,
		tb: tm.MustRegisterViews("vault/pledge/*").
			WithLayout("main"),
	}
	gr := r.Group("/vault/pledge")
	gr.Use(auth.HasAuth)
	gr.GET("", h.index)
	gr.POST("/add", h.addPledge)
	gr.POST("/remove", h.removePledge)
}
