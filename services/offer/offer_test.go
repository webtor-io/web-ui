package offer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/webtor-io/web-ui/services/payments"
)

type fakeSource struct {
	c   *payments.Catalog
	err error
}

func (f *fakeSource) Catalog(context.Context) (*payments.Catalog, error) { return f.c, f.err }

func i64(v int64) *int64 { return &v }

// Mirrors production on 2026-09-22: Silver monthly is the promo plan with a
// 7-day trial; sparkling is unlimited and unpriced.
func prodCatalog() *payments.Catalog {
	return &payments.Catalog{
		Prices: []payments.Price{
			{TierID: 1, TierName: "bronze", PeriodDays: 365, AmountUSD: 18},
			{TierID: 2, TierName: "silver", PeriodDays: 30, AmountUSD: 5, TrialDays: 7, IsPromo: true},
			{TierID: 2, TierName: "silver", PeriodDays: 365, AmountUSD: 45},
			{TierID: 3, TierName: "gold", PeriodDays: 30, AmountUSD: 15},
		},
		Tiers: []payments.Tier{
			{TierID: 0, Name: "free", DownloadRate: i64(5), VaultPoints: i64(0)},
			{TierID: 1, Name: "bronze", DownloadRate: i64(20), VaultPoints: i64(50)},
			{TierID: 2, Name: "silver", DownloadRate: i64(50), VaultPoints: i64(250)},
			{TierID: 3, Name: "gold", DownloadRate: i64(100), VaultPoints: i64(1000)},
			{TierID: 6, Name: "sparkling"},
		},
	}
}

func checkoutAll(tier string, period int, trial bool) string {
	return fmt.Sprintf("co:%s:%d:%v", tier, period, trial)
}

func loaded(c *payments.Catalog, co Checkout) *Service {
	s := New(&fakeSource{c: c}, co)
	s.Refresh(context.Background())
	return s
}

func TestPromo(t *testing.T) {
	cases := []struct {
		name string
		svc  *Service
		want *Offer
	}{
		{"no webhook configured", New(nil, checkoutAll), nil},
		{"catalog not loaded yet", New(&fakeSource{c: prodCatalog()}, checkoutAll), nil},
		{"promo with trial", loaded(prodCatalog(), checkoutAll),
			&Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, VaultPoints: 250, HasVault: true, TrialDays: 7, URL: "co:silver:30:true"}},
		// A trial the checkout cannot start is not offered; the plan still is.
		{"trial without a trial checkout", loaded(prodCatalog(), func(tier string, p int, trial bool) string {
			if trial {
				return ""
			}
			return checkoutAll(tier, p, trial)
		}), &Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, VaultPoints: 250, HasVault: true, URL: "co:silver:30:false"}},
		{"no direct checkout at all", loaded(prodCatalog(), nil),
			&Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, VaultPoints: 250, HasVault: true}},
		{"no promo plan", loaded(func() *payments.Catalog {
			c := prodCatalog()
			c.Prices[1].IsPromo = false
			return c
		}(), checkoutAll), nil},
		// A webhook that predates the tier catalog: the offer would have to
		// invent its numbers, so there is none.
		{"tier facts unknown", loaded(func() *payments.Catalog {
			c := prodCatalog()
			c.Tiers = nil
			return c
		}(), checkoutAll), nil},
	}
	for _, c := range cases {
		got := c.svc.Promo()
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestUnlimitedPromoTier(t *testing.T) {
	c := prodCatalog()
	c.Prices = append(c.Prices, payments.Price{TierID: 6, TierName: "sparkling", PeriodDays: 30, IsPromo: true})
	c.Prices[1].IsPromo = false
	o := loaded(c, nil).Promo()
	if o == nil || o.RateMbps != 0 || !o.HasVault {
		t.Fatalf("unlimited tier: got %+v, want rate 0 (unlimited) and Vault", o)
	}
}

// A failed refresh keeps the last catalog: offers must not blink out with
// the webhook.
func TestRefreshKeepsLastGoodCatalog(t *testing.T) {
	src := &fakeSource{c: prodCatalog()}
	s := New(src, checkoutAll)
	if !s.Refresh(context.Background()) {
		t.Fatal("first refresh failed")
	}
	src.c, src.err = nil, errors.New("webhook down")
	if s.Refresh(context.Background()) {
		t.Fatal("refresh against a failing source reported success")
	}
	if s.Promo() == nil || !s.HasPlans() {
		t.Fatal("catalog lost after a failed refresh")
	}
}

func TestTrialDays(t *testing.T) {
	s := loaded(prodCatalog(), checkoutAll)
	if got := s.TrialDays("silver"); got != 7 {
		t.Errorf("silver: got %d, want 7", got)
	}
	if got := s.TrialDays("gold"); got != 0 {
		t.Errorf("gold: got %d, want 0", got)
	}
	if got := New(nil, nil).TrialDays("silver"); got != 0 {
		t.Errorf("no catalog: got %d, want 0", got)
	}
}

func TestHasPlans(t *testing.T) {
	cases := []struct {
		name string
		svc  *Service
		want bool
	}{
		{"no webhook", New(nil, nil), false},
		{"catalog with plans", loaded(prodCatalog(), nil), true},
		// A storefront that lists tiers but sells nothing has nothing to
		// link an "upgrade" button to.
		{"catalog without plans", loaded(&payments.Catalog{Tiers: prodCatalog().Tiers}, nil), false},
	}
	for _, c := range cases {
		if got := c.svc.HasPlans(); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPitch(t *testing.T) {
	silver := &Offer{Tier: "silver", RateMbps: 50}
	format := func(sec float64) string { k, d := durationParts(sec); return k + " " + fmt.Sprint(d) }
	const gb = int64(1) << 30
	cases := []struct {
		name  string
		o     *Offer
		size  int64
		rate  int
		nilOK bool
		slow  string
		fast  string
	}{
		// 4.3 GiB at 5 Mbps ≈ 2 h 3 min; at 50 Mbps ≈ 12 min.
		{"typical movie", silver, 43 * gb / 10, 5, false, "offer.eta.hMin map[H:2 M:3]", "offer.eta.min map[M:12]"},
		{"small file is not worth it", silver, 50 << 20, 5, true, "", ""},
		{"size unknown", silver, 0, 5, true, "", ""},
		{"user rate unlimited", silver, 4 * gb, 0, true, "", ""},
		{"plan not faster", silver, 4 * gb, 50, true, "", ""},
		{"unlimited plan", &Offer{Tier: "sparkling"}, 4 * gb, 5, true, "", ""},
		{"multi-day", silver, 200 * gb, 5, false, "offer.eta.dH map[D:3 H:23]", "offer.eta.hMin map[H:9 M:33]"},
	}
	for _, c := range cases {
		p := pitch(c.o, c.size, c.rate, format)
		if c.nilOK {
			if p != nil {
				t.Errorf("%s: want no pitch, got %+v", c.name, p)
			}
			continue
		}
		if p == nil {
			t.Fatalf("%s: no pitch", c.name)
		}
		if p.Slow != c.slow || p.Fast != c.fast || p.FastRate != c.o.RateMbps {
			t.Errorf("%s: got slow=%q fast=%q rate=%d", c.name, p.Slow, p.Fast, p.FastRate)
		}
	}
}

func TestDurationParts(t *testing.T) {
	cases := []struct {
		sec  float64
		key  string
		data string
	}{
		{5, "offer.eta.min", "map[M:1]"},
		{89, "offer.eta.min", "map[M:1]"},
		{91, "offer.eta.min", "map[M:2]"},
		{3599, "offer.eta.h", "map[H:1]"},
		{3630, "offer.eta.hMin", "map[H:1 M:1]"},
		// 1 h 59 m 40 s rounds to the whole hour, not to "1 h 60 min".
		{7180, "offer.eta.h", "map[H:2]"},
		{3600 + 25*60, "offer.eta.hMin", "map[H:1 M:25]"},
		{2 * 86400, "offer.eta.d", "map[D:2]"},
		{2*86400 + 5*3600, "offer.eta.dH", "map[D:2 H:5]"},
	}
	for _, c := range cases {
		k, d := durationParts(c.sec)
		if k != c.key || fmt.Sprint(d) != c.data {
			t.Errorf("%v s: got %s %v, want %s %s", c.sec, k, d, c.key, c.data)
		}
	}
}
