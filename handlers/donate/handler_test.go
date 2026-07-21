package donate

import (
	"testing"

	np "github.com/webtor-io/web-ui/services/nowpayments"
)

func price(tier int, name string, days int, amount float64) np.Price {
	return np.Price{TierID: tier, TierName: name, PeriodDays: days, AmountUSD: amount}
}

func TestBuildCards(t *testing.T) {
	d := buildCards([]np.Price{
		price(3, "gold", 30, 15),
		price(3, "gold", 365, 135),
		price(1, "bronze", 30, 2),
		price(1, "bronze", 365, 18),
		price(2, "silver", 30, 5),
		price(2, "silver", 365, 45),
	})
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
		benefits                        int
	}{
		{"bronze", "2", "1.50", "18", false, 4},
		{"silver", "5", "3.75", "45", true, 5},
		{"gold", "15", "11.25", "135", false, 5},
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
		if len(c.BenefitKeys) != e.benefits || c.TitleKey == "" || c.TaglineKey == "" {
			t.Errorf("%s: meta %+v", c.Name, c)
		}
	}
}

func TestBuildCards_UnknownTierAndMonthlyOnly(t *testing.T) {
	d := buildCards([]np.Price{
		price(7, "platinum", 30, 30),
	})
	if len(d.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(d.Cards))
	}
	c := d.Cards[0]
	if c.TitleKey != "" || c.TaglineKey != "" || len(c.BenefitKeys) != 0 {
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

func TestBuildCards_Empty(t *testing.T) {
	d := buildCards(nil)
	if len(d.Cards) != 0 || d.AnnualSavePct != 0 {
		t.Errorf("expected empty, got %+v", d)
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
