package donate

import (
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/offer"
	np "github.com/webtor-io/web-ui/services/payments"
)

func price(tier int, name string, days int, amount float64) np.Price {
	return np.Price{TierID: tier, TierName: name, PeriodDays: days, AmountUSD: amount}
}

func i64(v int64) *int64 { return &v }

// catalog is production on 2026-09-22: Silver monthly is the promo plan with
// a 7-day trial; the tier facts are the tier table's.
func catalog(prices ...np.Price) *np.Catalog {
	return &np.Catalog{
		Prices: prices,
		Tiers: []np.Tier{
			{TierID: 0, Name: "free", DownloadRate: i64(5), VaultPoints: i64(0)},
			{TierID: 1, Name: "bronze", DownloadRate: i64(20), VaultPoints: i64(50)},
			{TierID: 2, Name: "silver", DownloadRate: i64(50), VaultPoints: i64(250)},
			{TierID: 3, Name: "gold", DownloadRate: i64(100), VaultPoints: i64(1000)},
		},
	}
}

func prodPrices() []np.Price {
	silver := price(2, "silver", 30, 5)
	silver.TrialDays, silver.IsPromo = 7, true
	return []np.Price{
		price(3, "gold", 30, 15),
		price(3, "gold", 365, 135),
		price(1, "bronze", 30, 2),
		price(1, "bronze", 365, 18),
		silver,
		price(2, "silver", 365, 45),
	}
}

func benefitKeys(bs []offer.Benefit) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Key
	}
	return out
}

func TestBuildCards(t *testing.T) {
	d := buildCards(catalog(prodPrices()...), true, true)
	if len(d.Cards) != 3 {
		t.Fatalf("expected 3 cards, got %d", len(d.Cards))
	}
	if d.AnnualSavePct != 25 {
		t.Errorf("expected save pct 25, got %d", d.AnnualSavePct)
	}
	expected := []struct {
		name                            string
		monthly, annualPerMonth, annual string
		recommended                     bool
		benefits                        []string
		trialDays                       int
	}{
		{"bronze", "2", "1.50", "18", false, []string{"donate.tier.vaultGB", "donate.tier.speed", "donate.tier.supporterBadge"}, 0},
		{"silver", "5", "3.75", "45", true, []string{"donate.tier.vaultGB", "donate.tier.speed", "donate.tier.prioritySupport", "donate.tier.supporterBadge"}, 7},
		{"gold", "15", "11.25", "135", false, []string{"donate.tier.vaultTB", "donate.tier.speed", "donate.tier.prioritySupport", "donate.tier.supporterBadge"}, 0},
	}
	for i, e := range expected {
		c := d.Cards[i]
		if c.Name != e.name || !c.HasMonthly || !c.HasAnnual {
			t.Errorf("card %d: unexpected %+v", i, c)
		}
		if c.MonthlyUSD != e.monthly || c.AnnualPerMonthUSD != e.annualPerMonth || c.AnnualTotalUSD != e.annual {
			t.Errorf("%s: prices %s/%s/%s, expected %s/%s/%s",
				c.Name, c.MonthlyUSD, c.AnnualPerMonthUSD, c.AnnualTotalUSD, e.monthly, e.annualPerMonth, e.annual)
		}
		if c.Recommended != e.recommended {
			t.Errorf("%s: recommended=%v, expected %v", c.Name, c.Recommended, e.recommended)
		}
		if got := benefitKeys(c.Benefits); strings.Join(got, ",") != strings.Join(e.benefits, ",") || c.TitleKey == "" || c.TaglineKey == "" {
			t.Errorf("%s: benefits %v, expected %v", c.Name, got, e.benefits)
		}
		if c.TrialDays != e.trialDays {
			t.Errorf("%s: trial days %d, expected %d", c.Name, c.TrialDays, e.trialDays)
		}
	}
	// Numbers on the lines are the tier's, not copy.
	silver := d.Cards[1].Benefits
	if silver[0].VP != 250 || silver[1].Rate != 50 {
		t.Errorf("silver facts: %+v", silver)
	}
	if gold := d.Cards[2].Benefits[0]; gold.VP != 1000 || gold.TB != 1 {
		t.Errorf("gold vault: %+v", gold)
	}
	// The trial plan's monthly Join is the trial checkout; the plaque and the
	// Patreon block advertise it.
	s := d.Cards[1]
	if !strings.HasSuffix(s.TrialURL, "rid=3972747&is_free_trial=true") || s.PatreonMonthURL != s.TrialURL {
		t.Errorf("silver trial links: trial=%q month=%q", s.TrialURL, s.PatreonMonthURL)
	}
	if !strings.HasSuffix(s.PatreonYearURL, "rid=3972747&cadence=12") {
		t.Errorf("silver year link: %q", s.PatreonYearURL)
	}
	if d.TrialDays != 7 || d.TrialTier != "Silver" {
		t.Errorf("patreon block trial: %d %q", d.TrialDays, d.TrialTier)
	}
	if strings.Contains(d.Cards[2].PatreonMonthURL, "is_free_trial") {
		t.Errorf("gold has no trial: %q", d.Cards[2].PatreonMonthURL)
	}
}

// Recommended follows the promo plan, not the card's position.
func TestBuildCards_RecommendedIsThePromoPlan(t *testing.T) {
	ps := prodPrices()
	ps[4].IsPromo = false // silver monthly
	ps[1].IsPromo = true  // gold annual
	d := buildCards(catalog(ps...), true, true)
	for _, c := range d.Cards {
		if c.Recommended != (c.Name == "gold") {
			t.Errorf("%s: recommended=%v", c.Name, c.Recommended)
		}
	}
}

// A webhook that predates offer terms and the tier catalog: the page keeps
// its shape (middle card recommended) and simply has no trial and no
// speed/Vault lines to quote.
func TestBuildCards_OlderWebhook(t *testing.T) {
	d := buildCards(&np.Catalog{Prices: []np.Price{
		price(1, "bronze", 30, 2), price(2, "silver", 30, 5), price(3, "gold", 30, 15),
	}}, true, true)
	if !d.Cards[1].Recommended || d.Cards[0].Recommended || d.Cards[2].Recommended {
		t.Errorf("fallback recommended must be the middle card: %+v", d.Cards)
	}
	if d.TrialDays != 0 || d.Cards[1].TrialDays != 0 {
		t.Errorf("no trial without offer terms: %+v", d)
	}
	if got := benefitKeys(d.Cards[1].Benefits); strings.Join(got, ",") != "donate.tier.prioritySupport,donate.tier.supporterBadge" {
		t.Errorf("without tier facts only the static lines remain, got %v", got)
	}
}

// A trial Patreon cannot start is not advertised.
func TestBuildCards_TrialNeedsPatreon(t *testing.T) {
	d := buildCards(catalog(prodPrices()...), false, true)
	if d.TrialDays != 0 || d.Cards[1].TrialDays != 0 || d.Cards[1].PatreonMonthURL != "" {
		t.Errorf("patreon off: %+v", d.Cards[1])
	}
}

func TestBuildCards_UnknownTierAndMonthlyOnly(t *testing.T) {
	d := buildCards(catalog(price(7, "platinum", 30, 30)), true, true)
	if len(d.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(d.Cards))
	}
	c := d.Cards[0]
	if c.TitleKey != "" || c.TaglineKey != "" || len(c.Benefits) != 0 {
		t.Errorf("unknown tier must have no meta keys: %+v", c)
	}
	if !c.HasMonthly || c.HasAnnual {
		t.Errorf("expected monthly-only card: %+v", c)
	}
	if d.AnnualSavePct != 0 {
		t.Errorf("expected no save pct, got %d", d.AnnualSavePct)
	}
	if !c.Recommended {
		t.Errorf("single card should be recommended: %+v", c)
	}
}

func TestBuildCards_CryptoDisabled(t *testing.T) {
	unavailable := false
	p := price(1, "bronze", 30, 2)
	p.Available = &unavailable
	d := buildCards(catalog(p, price(1, "bronze", 365, 18)), true, false)
	if d.CryptoEnabled {
		t.Error("expected CryptoEnabled=false")
	}
	if d.HasUnavailable {
		t.Error("unavailable-plans footnote must be off without the crypto links it explains")
	}
	if len(d.Cards) != 1 || !d.Cards[0].HasAnnual {
		t.Errorf("cards must stay without crypto: %+v", d.Cards)
	}
	if d.Cards[0].PatreonMonthURL == "" {
		t.Errorf("patreon links must stay without crypto: %+v", d.Cards[0])
	}
}

func TestBuildCards_Empty(t *testing.T) {
	d := buildCards(nil, true, true)
	if len(d.Cards) != 0 || d.AnnualSavePct != 0 {
		t.Errorf("expected empty, got %+v", d)
	}
}

func TestTierBenefits_Unlimited(t *testing.T) {
	got := TierBenefits("gold", &np.Tier{Name: "gold"})
	if k := benefitKeys(got); strings.Join(k[:2], ",") != "donate.tier.vaultUnlimited,donate.tier.speedUnlimited" {
		t.Errorf("unlimited tier: %v", k)
	}
	if got := TierBenefits("bronze", &np.Tier{Name: "bronze", DownloadRate: i64(20), VaultPoints: i64(0)}); benefitKeys(got)[0] != "donate.tier.speed" {
		t.Errorf("a tier without Vault must not promise it: %v", benefitKeys(got))
	}
}

// Vault Points are storage: whole thousands read as TB, the rest as GB, and
// the unit itself lives in the locale key.
func TestVaultBenefit(t *testing.T) {
	cases := []struct {
		vp   int64
		key  string
		tb   int64
	}{
		{50, "donate.tier.vaultGB", 0},
		{999, "donate.tier.vaultGB", 0},
		{1000, "donate.tier.vaultTB", 1},
		{1500, "donate.tier.vaultGB", 0},
		{2000, "donate.tier.vaultTB", 2},
	}
	for _, c := range cases {
		got := vaultBenefit(c.vp)
		if got.Key != c.key || got.VP != c.vp || got.TB != c.tb {
			t.Errorf("%d VP: got %+v, want %s TB=%d", c.vp, got, c.key, c.tb)
		}
	}
}

func TestCheckout(t *testing.T) {
	cases := []struct {
		tier   string
		period int
		trial  bool
		want   string
	}{
		{"silver", 30, true, "https://www.patreon.com/checkout/pavel_tatarskiy?rid=3972747&is_free_trial=true"},
		{"silver", 365, false, "https://www.patreon.com/checkout/pavel_tatarskiy?rid=3972747&cadence=12"},
		{"gold", 30, false, "https://www.patreon.com/checkout/pavel_tatarskiy?rid=3981014"},
		{"platinum", 30, false, ""},
	}
	for _, c := range cases {
		if got := patreonCheckout(c.tier, c.period, c.trial); got != c.want {
			t.Errorf("%s/%d/%v: got %q, want %q", c.tier, c.period, c.trial, got, c.want)
		}
	}
}

func TestFmtUSD(t *testing.T) {
	for _, tc := range []struct {
		in       float64
		expected string
	}{
		{2, "2"},
		{1.5, "1.50"},
		{11.25, "11.25"},
		{135, "135"},
	} {
		if got := fmtUSD(tc.in); got != tc.expected {
			t.Errorf("fmtUSD(%v): expected %s, got %s", tc.in, tc.expected, got)
		}
	}
}
