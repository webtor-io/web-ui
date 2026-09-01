package onboarding

import (
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
// The dev-only `?onboarding=free|paid` override lives in the middleware
// (services/web.loadOnboarding → Service.Preview), not here: it has to render
// a fresh-account checklist past the age and progress gates, which a tier
// switch alone cannot do.
func PaidTier(cla *claims.Data) bool {
	if cla == nil || cla.Context == nil || cla.Context.Tier == nil {
		return false
	}
	return cla.Context.Tier.Id != 0
}
