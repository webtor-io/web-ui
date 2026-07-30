package onboarding

import (
	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/claims"
)

// PaidTier reports whether the user is on any paid tier, Patreon free trials
// included — a trial grants a real tier, so tier id 0 is the only free state.
//
// Deliberately the exact inverse of handlers/vault.isFreeTier, including the
// treatment of missing claims as free. The Vault step links to /vault, and if
// the two disagreed we would unlock the step here and then show the user the
// free-tier upsell on arrival. (In practice claims are always present: the
// claims middleware aborts the request on provider failure, so nothing
// downstream runs without them.)
//
// The dev-only `?onboarding=free|paid` override rides along so the locked PRO
// rows can be reviewed without a second account. It is gated on gin.Mode()
// exactly like the streaming-error debug modals in handlers/action: under
// GIN_MODE=release the query param is inert. It only changes how the checklist
// draws — nothing downstream reads it, so no paid feature becomes reachable.
func PaidTier(c *gin.Context, cla *claims.Data) bool {
	paid := isPaid(cla)
	if gin.Mode() == gin.ReleaseMode {
		return paid
	}
	switch c.Query("onboarding") {
	case "free":
		return false
	case "paid":
		return true
	}
	return paid
}

func isPaid(cla *claims.Data) bool {
	if cla == nil || cla.Context == nil || cla.Context.Tier == nil {
		return false
	}
	return cla.Context.Tier.Id != 0
}
