package onboarding

import (
	"testing"

	proto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/services/claims"
)

func claimsWithTier(id uint32) *claims.Data {
	return &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: id}}}
}

func TestPaidTierUsesClaimsTier(t *testing.T) {
	if PaidTier(claimsWithTier(0)) {
		t.Error("tier id 0 is the free tier")
	}
	if !PaidTier(claimsWithTier(2)) {
		t.Error("a non-zero tier id is paid")
	}
}

// A Patreon free trial grants a real tier, so trial users read as paid — which
// is what we want: they can use Vault and the Stremio backend.
func TestPaidTierCountsTrialTiers(t *testing.T) {
	for _, id := range []uint32{1, 2, 3} {
		if !PaidTier(claimsWithTier(id)) {
			t.Errorf("tier id %d must count as paid", id)
		}
	}
}

// Missing claims must read as free, matching handlers/vault.isFreeTier — if the
// two disagreed we would unlock the Vault step here and then show the free-tier
// upsell when the user follows it.
func TestPaidTierTreatsMissingClaimsAsFree(t *testing.T) {
	for name, cla := range map[string]*claims.Data{
		"nil claims":  nil,
		"nil context": {},
		"nil tier":    {Context: &proto.Context{}},
	} {
		if PaidTier(cla) {
			t.Errorf("%s must read as free", name)
		}
	}
}
