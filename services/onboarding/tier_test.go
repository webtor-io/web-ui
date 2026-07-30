package onboarding

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	proto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/services/claims"
)

func claimsWithTier(id uint32) *claims.Data {
	return &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: id}}}
}

func TestIsPaidUsesClaimsTier(t *testing.T) {
	if isPaid(claimsWithTier(0)) {
		t.Error("tier id 0 is the free tier")
	}
	if !isPaid(claimsWithTier(2)) {
		t.Error("a non-zero tier id is paid")
	}
}

// A Patreon free trial grants a real tier, so trial users read as paid — which
// is what we want: they can use Vault and the Stremio backend.
func TestIsPaidCountsTrialTiers(t *testing.T) {
	for _, id := range []uint32{1, 2, 3} {
		if !isPaid(claimsWithTier(id)) {
			t.Errorf("tier id %d must count as paid", id)
		}
	}
}

// Missing claims must read as free, matching handlers/vault.isFreeTier — if the
// two disagreed we would unlock the Vault step here and then show the free-tier
// upsell when the user follows it.
func TestIsPaidTreatsMissingClaimsAsFree(t *testing.T) {
	if isPaid(nil) {
		t.Error("nil claims must read as free, like handlers/vault does")
	}
}

// Partially-populated claims must not panic and must fall through to the
// column rather than silently treating everyone as paid.
func TestIsPaidHandlesIncompleteClaims(t *testing.T) {
	if isPaid(&claims.Data{}) {
		t.Error("claims without a context must read as free")
	}
	if isPaid(&claims.Data{Context: &proto.Context{}}) {
		t.Error("claims without a tier must read as free")
	}
}

// The dev-only tier override must be inert in production: shipping a query
// param that changes paid rendering on the live site would be a hole.
func TestPaidTierOverrideIsInertInRelease(t *testing.T) {
	prev := gin.Mode()
	defer gin.SetMode(prev)

	gin.SetMode(gin.ReleaseMode)
	for _, q := range []string{"free", "paid", ""} {
		c := ginContextWithQuery("onboarding=" + q)
		if got := PaidTier(c, paidClaims); !got {
			t.Errorf("release mode, onboarding=%q: paid user must stay paid", q)
		}
		if got := PaidTier(c, freeClaims); got {
			t.Errorf("release mode, onboarding=%q: free user must stay free", q)
		}
	}
}

func TestPaidTierOverrideAppliesOutsideRelease(t *testing.T) {
	prev := gin.Mode()
	defer gin.SetMode(prev)

	gin.SetMode(gin.DebugMode)
	if PaidTier(ginContextWithQuery("onboarding=free"), paidClaims) {
		t.Error("onboarding=free must render a paying user as free")
	}
	if !PaidTier(ginContextWithQuery("onboarding=paid"), freeClaims) {
		t.Error("onboarding=paid must render a free user as paid")
	}
	// Anything else leaves the real tier alone.
	for _, q := range []string{"", "onboarding=", "onboarding=bogus", "other=free"} {
		if !PaidTier(ginContextWithQuery(q), paidClaims) {
			t.Errorf("query %q must not downgrade a paying user", q)
		}
		if PaidTier(ginContextWithQuery(q), freeClaims) {
			t.Errorf("query %q must not upgrade a free user", q)
		}
	}
}

var (
	paidClaims = claimsWithTier(2)
	freeClaims = claimsWithTier(0)
)

func ginContextWithQuery(rawQuery string) *gin.Context {
	c := &gin.Context{}
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	return c
}
