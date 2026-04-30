package scripts

import (
	"math"
	"testing"

	claimsproto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/web"
)

func ctxWith(rate, tier string) *web.Context {
	c := &web.Context{}
	if rate != "" {
		c.ApiClaims = &api.Claims{Rate: rate}
	}
	if tier != "" {
		c.Claims = &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Name: tier}}}
	}
	return c
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestParseRateLimit(t *testing.T) {
	cases := map[string]int64{
		"":    0,
		"10M": 10_000_000,
		"1M":  1_000_000,
		"10":  0,
		"10K": 0,
		"xM":  0,
	}
	for in, want := range cases {
		if got := parseRateLimit(in); got != want {
			t.Errorf("parseRateLimit(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestIsRateLimited(t *testing.T) {
	// cap=10Mbit/s => 1_250_000 B/s
	cap := int64(10_000_000)
	if !isRateLimited(1_200_000, cap) {
		t.Error("1.2MB/s against 10Mbit/s cap should count as rate-limited (≥90%)")
	}
	if isRateLimited(1_000_000, cap) {
		t.Error("1.0MB/s (80% of cap) should not count as rate-limited")
	}
}

func TestBuildSlowDownloadData_RateLimited(t *testing.T) {
	c := ctxWith("10M", "free")
	// measured speed = 1.2 MB/s ≈ 9.6 Mbit/s (saturates 10Mbit cap)
	sdd := buildSlowDownloadData(c, 1_200_000, 8_000_000)
	if !sdd.IsRateLimited {
		t.Fatal("expected IsRateLimited=true")
	}
	if !almostEqual(sdd.RateLimitMbps, 10) {
		t.Errorf("RateLimitMbps = %v, want 10", sdd.RateLimitMbps)
	}
	if !almostEqual(sdd.MeasuredSpeedMbps, 9.6) {
		t.Errorf("MeasuredSpeedMbps = %v, want 9.6", sdd.MeasuredSpeedMbps)
	}
	if !almostEqual(sdd.RequiredSpeedMbps, 8) {
		t.Errorf("RequiredSpeedMbps = %v, want 8 (= bitrate)", sdd.RequiredSpeedMbps)
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free", sdd.TierName)
	}
}

func TestBuildSlowDownloadData_SlowNotCappedFreeFallback(t *testing.T) {
	c := &web.Context{}
	// No claims at all: slow peers, no cap. IsRateLimited must be false, tier defaults to "free".
	sdd := buildSlowDownloadData(c, 500_000, 8_000_000)
	if sdd.IsRateLimited {
		t.Error("no claims should not flag IsRateLimited")
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free fallback", sdd.TierName)
	}
	if sdd.RateLimitMbps != 0 {
		t.Errorf("RateLimitMbps = %v, want 0", sdd.RateLimitMbps)
	}
}

func TestBuildSlowDownloadData_CapPresentButNotSaturated(t *testing.T) {
	// Cap=20M, measured 500KB/s (4Mbit/s) — slow for other reasons, not rate limit.
	c := ctxWith("20M", "free")
	sdd := buildSlowDownloadData(c, 500_000, 8_000_000)
	if sdd.IsRateLimited {
		t.Error("measured speed far below cap should not be classified as rate-limited")
	}
}

func TestCheckCachedRateLimit_NoCap(t *testing.T) {
	c := &web.Context{}
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("no ApiClaims => not limited")
	}
	c = ctxWith("", "premium")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("empty Rate => not limited")
	}
}

func TestCheckCachedRateLimit_CapSufficient(t *testing.T) {
	// Cap=20M, bitrate=8M. Cap > bitrate => not limited.
	c := ctxWith("20M", "basic")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("cap above bitrate should not raise warning")
	}
}

func TestCheckCachedRateLimit_CapInsufficient(t *testing.T) {
	// Cap=5M, bitrate=8M. Cap < bitrate => warn.
	c := ctxWith("5M", "free")
	sdd, limited := checkCachedRateLimit(c, 8_000_000)
	if !limited {
		t.Fatal("cap below bitrate should raise warning")
	}
	if !sdd.IsRateLimited {
		t.Error("SlowDownloadData.IsRateLimited should be true on cached path")
	}
	if !almostEqual(sdd.RateLimitMbps, 5) {
		t.Errorf("RateLimitMbps = %v, want 5", sdd.RateLimitMbps)
	}
	// For cached path the "measured" speed equals the cap.
	if !almostEqual(sdd.MeasuredSpeedMbps, 5) {
		t.Errorf("MeasuredSpeedMbps = %v, want 5 (== cap)", sdd.MeasuredSpeedMbps)
	}
	if !almostEqual(sdd.RequiredSpeedMbps, 8) {
		t.Errorf("RequiredSpeedMbps = %v, want 8 (= bitrate)", sdd.RequiredSpeedMbps)
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free", sdd.TierName)
	}
}

func TestCheckCachedRateLimit_CapEqualsRequirement(t *testing.T) {
	// Boundary: cap exactly at bitrate — treat as sufficient.
	c := ctxWith("8M", "basic")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("cap == bitrate should not raise warning")
	}
}
