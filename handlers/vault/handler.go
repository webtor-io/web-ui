package vault

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
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
	PledgeID     string
	ResourceID   string
	Resource     *vaultModels.Resource
	Amount       float64
	IsFrozen     bool
	Funded       bool
	CreatedAt    string
	ExpiresIn    time.Duration // time until resource is removed (for unfunded pledges)
	ShowProgress bool          // funded but not yet vaulted — wire up live progress SSE
}

type PledgeListData struct {
	Pledges               []PledgeDisplay
	Stats                 *vault.UserStats
	FreezePeriod          time.Duration
	ExpirePeriod          time.Duration
	TransferTimeoutPeriod time.Duration
	IsFree                bool
}

func RegisterHandler(r *gin.Engine, v *vault.Vault, tm *template.Manager[*web.Context], pg *cs.PG) {
	h := &Handler{
		vault: v,
		pg:    pg,
		tb: tm.MustRegisterViews("vault/*").
			WithLayout("main"),
	}
	gr := r.Group("/vault")
	// GET /vault redirects guests to /login?from=vault inside the handler so the
	// click never dead-ends on a 401. Mutating routes stay gated by middleware.
	gr.GET("", h.index)
	gr.POST("/add", auth.HasAuth, h.addPledge)
	gr.POST("/remove", auth.HasAuth, h.removePledge)

	// Backwards-compat redirect: old /vault/pledge URL was renamed to /vault.
	// 302 (not 301) so the redirect can be removed later without poisoning
	// browser caches — the URL was new and never had time to spread.
	// The target keeps the language prefix the user came in with.
	r.GET("/vault/pledge", func(c *gin.Context) {
		target := "/vault"
		if lang := i18n.GetLang(c); lang != "" && lang != i18n.DefaultLang {
			target = "/" + lang + "/vault"
		}
		c.Redirect(http.StatusFound, target)
	})
}
