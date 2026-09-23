package offer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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

func TestFreeCapMbps(t *testing.T) {
	uncapped := prodCatalog()
	uncapped.Tiers[0].DownloadRate = nil
	noFree := prodCatalog()
	noFree.Tiers = noFree.Tiers[1:]
	var nilSvc *Service
	cases := []struct {
		name string
		svc  *Service
		want int64
	}{
		{"production catalog", loaded(prodCatalog(), checkoutAll), 5},
		{"no webhook configured", New(nil, checkoutAll), 0},
		{"catalog not loaded yet", New(&fakeSource{c: prodCatalog()}, checkoutAll), 0},
		{"free tier without a cap", loaded(uncapped, checkoutAll), 0},
		{"no free tier in the catalog", loaded(noFree, checkoutAll), 0},
		{"no service", nilSvc, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.svc.FreeCapMbps(); got != tc.want {
				t.Errorf("FreeCapMbps() = %d, want %d", got, tc.want)
			}
		})
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
		// Every file shows the difference, a small one too — in seconds, not
		// rounded up to a minute.
		{"small file", silver, 123 << 20, 5, false, "offer.eta.min map[M:3]", "offer.eta.sec map[S:20]"},
		{"tiny file", silver, 5 << 20, 5, false, "offer.eta.sec map[S:10]", "offer.eta.sec map[S:5]"},
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
		// Under a minute: seconds in fives, never "1 min" — rounding 20 s up
		// made a 10× plan read as 3× next to a 3-minute wait.
		{1, "offer.eta.sec", "map[S:5]"},
		{21, "offer.eta.sec", "map[S:20]"},
		{57, "offer.eta.sec", "map[S:55]"},
		{58, "offer.eta.min", "map[M:1]"},
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

// The CTA quotes how many times faster the plan is — a number from the
// catalog, rounded down, and only when it is worth saying.
func TestSpeedUp(t *testing.T) {
	silver := &Offer{Tier: "silver", RateMbps: 50}
	cases := []struct {
		name string
		o    *Offer
		rate int
		want int
	}{
		{"free to silver", silver, 5, 10},
		{"bronze to silver: 2.5× rounds down", silver, 20, 2},
		{"under 2× is not a pitch", silver, 30, 0},
		{"same speed", silver, 50, 0},
		{"viewer rate unknown", silver, 0, 0},
		{"unlimited plan has no ratio", &Offer{Tier: "sparkling"}, 5, 0},
		{"no offer", nil, 5, 0},
	}
	for _, c := range cases {
		if got := speedUp(c.o, c.rate); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// A code is handed out only while it will still be honoured DiscountLead
// later — a letter read a couple of days after it was sent must not lead to a
// dead code — and among the live ones the code with the most time left wins.
func TestDiscount(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	code := func(c string, pct int, periodDays int, left time.Duration) payments.Discount {
		return payments.Discount{Code: c, PercentOff: pct, PeriodDays: periodDays, ExpiresAt: now.Add(left)}
	}
	withDiscounts := func(ds ...payments.Discount) *Service {
		c := prodCatalog()
		c.Discounts = ds
		return loaded(c, checkoutAll)
	}
	cases := []struct {
		name string
		svc  *Service
		want string
	}{
		{"no webhook configured", New(nil, checkoutAll), ""},
		{"webhook predates discounts", loaded(prodCatalog(), checkoutAll), ""},
		{"live code", withDiscounts(code("SAMPLE50", 50, 30, 180*24*time.Hour)), "SAMPLE50"},
		{"expired", withDiscounts(code("old", 50, 30, -time.Hour)), ""},
		{"inside the lead", withDiscounts(code("soon", 50, 30, DiscountLead-time.Minute)), ""},
		{"exactly at the lead", withDiscounts(code("edge", 50, 30, DiscountLead)), "edge"},
		{"most time left wins", withDiscounts(code("short", 50, 30, 10*24*time.Hour), code("long", 30, 365, 90*24*time.Hour)), "long"},
		{"unknown plan length skipped", withDiscounts(code("week", 50, 7, 90*24*time.Hour)), ""},
		{"no code skipped", withDiscounts(payments.Discount{PercentOff: 50, PeriodDays: 30, ExpiresAt: now.Add(90 * 24 * time.Hour)}), ""},
	}
	for _, c := range cases {
		got := c.svc.Discount(now)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: want none, got %q", c.name, got.Code)
		case c.want != "" && (got == nil || got.Code != c.want):
			t.Errorf("%s: want %q, got %+v", c.name, c.want, got)
		}
	}
}

// The code is typed in at the provider's checkout of the plan it discounts:
// monthly for a first-month code, annual for a first-year one, never the trial
// variant — a former trialist cannot start another trial.
func TestDiscountCheckout(t *testing.T) {
	month := &payments.Discount{Code: "SAMPLE50", PercentOff: 50, PeriodDays: 30}
	year := &payments.Discount{Code: "SAMPLE30", PercentOff: 30, PeriodDays: 365}
	svc := loaded(prodCatalog(), checkoutAll)
	if got := svc.DiscountCheckout("silver", month); got != "co:silver:30:false" {
		t.Errorf("month: %q", got)
	}
	if got := svc.DiscountCheckout("gold", year); got != "co:gold:365:false" {
		t.Errorf("year: %q", got)
	}
	if got := svc.DiscountCheckout("", month); got != "" {
		t.Errorf("no tier: %q", got)
	}
	if got := loaded(prodCatalog(), nil).DiscountCheckout("silver", month); got != "" {
		t.Errorf("no provider: %q", got)
	}
}

func TestFreeRateMbps(t *testing.T) {
	cases := []struct {
		name string
		svc  *Service
		want int
	}{
		{"no webhook configured", New(nil, checkoutAll), 0},
		{"catalog not loaded yet", New(&fakeSource{c: prodCatalog()}, checkoutAll), 0},
		{"production catalog", loaded(prodCatalog(), checkoutAll), 5},
		// A nil rate is unlimited, not zero: there is no cap to quote.
		{"free tier without a cap", loaded(func() *payments.Catalog {
			c := prodCatalog()
			c.Tiers[0].DownloadRate = nil
			return c
		}(), checkoutAll), 0},
		{"no free tier listed", loaded(func() *payments.Catalog {
			c := prodCatalog()
			c.Tiers = c.Tiers[1:]
			return c
		}(), checkoutAll), 0},
		{"webhook predates the tier catalog", loaded(func() *payments.Catalog {
			c := prodCatalog()
			c.Tiers = nil
			return c
		}(), checkoutAll), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.svc.FreeRateMbps(); got != tc.want {
				t.Errorf("FreeRateMbps() = %d, want %d", got, tc.want)
			}
		})
	}
}
