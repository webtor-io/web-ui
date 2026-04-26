package vault

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

// index handles HTTP request for displaying the My Vault dashboard (Level 1: HTTP interaction).
// Stats and pledges come back from the same vault.GetUserStats call so the
// underlying GetUserPledgesWithResources query and per-pledge IsPledgeFrozen
// checks run only once per request.
func (h *Handler) index(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	stats, enriched, err := h.vault.GetUserStats(c.Request.Context(), user)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	data := &PledgeListData{
		Pledges:               buildPledgeDisplay(enriched, h.vault.GetExpirePeriod()),
		Stats:                 stats,
		FreezePeriod:          h.vault.GetFreezePeriod(),
		ExpirePeriod:          h.vault.GetExpirePeriod(),
		TransferTimeoutPeriod: h.vault.GetTransferTimeoutPeriod(),
	}

	h.tb.Build("vault/index").HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

// buildPledgeDisplay converts enriched pledges into display rows for the table
// (Level 2: presentation logic).
func buildPledgeDisplay(enriched []vault.EnrichedPledge, expirePeriod time.Duration) []PledgeDisplay {
	display := make([]PledgeDisplay, 0, len(enriched))
	for _, e := range enriched {
		var expiresIn time.Duration
		if !e.Pledge.Funded && e.Pledge.Resource != nil && e.Pledge.Resource.ExpiredAt != nil {
			remaining := time.Until(e.Pledge.Resource.ExpiredAt.Add(expirePeriod))
			if remaining > 0 {
				expiresIn = remaining
			}
		}

		display = append(display, PledgeDisplay{
			PledgeID:   e.Pledge.PledgeID.String(),
			ResourceID: e.Pledge.ResourceID,
			Resource:   e.Pledge.Resource,
			Amount:     e.Pledge.Amount,
			IsFrozen:   e.IsFrozen,
			Funded:     e.Pledge.Funded,
			CreatedAt:  e.Pledge.CreatedAt.Format("2006-01-02 15:04:05"),
			ExpiresIn:  expiresIn,
		})
	}
	return display
}
