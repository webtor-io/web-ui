package speedtest

import (
	"testing"

	np "github.com/webtor-io/web-ui/services/payments"
)

func i64(v int64) *int64 { return &v }

// The plan list is the storefront's, not a copy of it: free is in (it is what
// a free viewer is being compared against), a tier nobody can buy is out, and
// without a catalog there is no list at all rather than a stale one.
func TestCatalogPlans(t *testing.T) {
	cat := &np.Catalog{
		Prices: []np.Price{
			{TierID: 2, TierName: "silver", PeriodDays: 30},
			{TierID: 1, TierName: "bronze", PeriodDays: 365},
		},
		Tiers: []np.Tier{
			{TierID: 2, Name: "silver", DownloadRate: i64(50)},
			{TierID: 0, Name: "free", DownloadRate: i64(5)},
			{TierID: 1, Name: "bronze", DownloadRate: i64(20)},
			// Sold to nobody: not a plan to compare against.
			{TierID: 3, Name: "gold", DownloadRate: i64(100)},
			// Broken reference data must not take the page down.
			{TierID: 9, Name: ""},
		},
	}
	got := catalogPlans(cat)
	want := []Plan{
		{Name: "Free", Speed: 5, Label: "5 Mbps"},
		{Name: "Bronze", Speed: 20, Label: "20 Mbps"},
		{Name: "Silver", Speed: 50, Label: "50 Mbps"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("plan %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if p := catalogPlans(nil); p != nil {
		t.Errorf("no catalog must mean no plan list, got %+v", p)
	}
}

// An unlimited tier has no number to sort by; it belongs at the end of the
// ladder, not at its start.
func TestCatalogPlansUnlimitedGoesLast(t *testing.T) {
	cat := &np.Catalog{
		Prices: []np.Price{{TierID: 6, TierName: "sparkling", PeriodDays: 30}, {TierID: 1, TierName: "bronze", PeriodDays: 30}},
		Tiers: []np.Tier{
			{TierID: 6, Name: "sparkling"},
			{TierID: 0, Name: "free", DownloadRate: i64(5)},
			{TierID: 1, Name: "bronze", DownloadRate: i64(20)},
		},
	}
	got := catalogPlans(cat)
	if len(got) != 3 || got[2].Name != "Sparkling" || got[2].Speed != 0 || got[2].Label != "∞" {
		t.Errorf("got %+v", got)
	}
}
