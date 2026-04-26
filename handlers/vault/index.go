package vault

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

// index handles HTTP request for displaying the My Vault dashboard (Level 1: HTTP interaction).
// Anonymous visitors are redirected to /login with from=vault — the login page
// renders a contextual info card (intro + 4 features from vault.landing.* keys)
// driven by the from param so the click never dead-ends. Stats and pledges come
// back from the same vault.GetUserStats call so the underlying
// GetUserPledgesWithResources query and per-pledge IsPledgeFrozen checks run only
// once per request.
func (h *Handler) index(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	if !user.HasAuth() {
		lang := i18n.GetLang(c)
		v := url.Values{
			"from":       []string{"vault"},
			"return-url": []string{i18n.LangPath(lang, "/vault")},
		}
		c.Redirect(http.StatusFound, i18n.LangPath(lang, "/login")+"?"+v.Encode())
		return
	}

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

		// Mirror the LoadingCount predicate in services/vault.GetUserStats so the per-row
		// live-progress UI and the dashboard "Loading" stat agree on what's loading.
		showProgress := e.Pledge.Resource != nil &&
			e.Pledge.Resource.Funded &&
			!e.Pledge.Resource.Vaulted &&
			!e.Pledge.Resource.Expired

		display = append(display, PledgeDisplay{
			PledgeID:     e.Pledge.PledgeID.String(),
			ResourceID:   e.Pledge.ResourceID,
			Resource:     e.Pledge.Resource,
			Amount:       e.Pledge.Amount,
			IsFrozen:     e.IsFrozen,
			Funded:       e.Pledge.Funded,
			CreatedAt:    e.Pledge.CreatedAt.Format("2006-01-02 15:04:05"),
			ExpiresIn:    expiresIn,
			ShowProgress: showProgress,
		})
	}
	return display
}
