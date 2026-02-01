package instructions

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb    template.Builder[*web.Context]
	vault *vault.Vault
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], v *vault.Vault) {
	h := &Handler{
		tb:    tm.MustRegisterViews("instructions/**/*").WithLayout("main"),
		vault: v,
	}

	r.GET("/instructions/*template", h.get)
}

type VaultData struct {
	FreezePeriod          time.Duration
	ExpirePeriod          time.Duration
	TransferTimeoutPeriod time.Duration
}

type Data struct {
	Vault *VaultData
}

func (s *Handler) get(c *gin.Context) {
	data := &Data{}
	if s.vault != nil {
		data.Vault = &VaultData{
			FreezePeriod:          s.vault.GetFreezePeriod(),
			ExpirePeriod:          s.vault.GetExpirePeriod(),
			TransferTimeoutPeriod: s.vault.GetTransferTimeoutPeriod(),
		}
	}
	s.tb.Build("instructions"+c.Param("template")).HTML(http.StatusOK, web.NewContext(c).WithData(data))
}
