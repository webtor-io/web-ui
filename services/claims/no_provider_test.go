package claims

import "testing"

// A self-hosted instance runs without a claims provider, so the client is nil.
// The ordinary request path never notices: the visitor is an auto-admin and
// gets synthetic claims before the client is ever touched. But embed's domain
// lookup asks for a specific domain owner's claims directly, with no such
// shortcut — and that call used to dereference the nil client and panic, which
// meant registering a domain for embedding took the request down.
func TestClaimsWithoutProviderYieldSyntheticData(t *testing.T) {
	s := New(nil, nil, nil)

	d, err := s.Get(&Request{Email: "owner@example.com"})
	if err != nil {
		t.Fatalf("Get with no provider configured: %v", err)
	}
	if d == nil || d.Context == nil || d.Context.Tier == nil {
		t.Fatal("no provider configured: got empty claims, want synthetic ones")
	}
	// Tier 0 is what claims.IsPaid answers 402 to, so a synthetic tier has to
	// be a real one or every paid-gated route closes on a self-hosted box.
	if d.Context.Tier.Id == 0 {
		t.Error("synthetic claims carry tier 0, which IsPaid rejects with 402")
	}
	if d.Claims == nil || d.Claims.Embed == nil || !d.Claims.Embed.NoAds {
		t.Error("synthetic claims should carry no-ads: a self-hosted instance has no ad inventory")
	}
}
