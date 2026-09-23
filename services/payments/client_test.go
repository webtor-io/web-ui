package payments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/webtor-io/lazymap"
)

func catalogFrom(t *testing.T, body string) *Catalog {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prices" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := &Client{
		url:          srv.URL,
		cl:           srv.Client(),
		catalogCache: lazymap.New[*Catalog](&lazymap.Config{Expire: time.Minute}),
	}
	cat, err := c.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// The webhook's GET /prices contract as web-ui reads it: a live code arrives
// with every attribute an offer quotes, and a webhook that predates discount
// codes reads as "no code" rather than failing the whole catalog.
func TestCatalogDecodesDiscounts(t *testing.T) {
	cat := catalogFrom(t, `{"prices":[],"tiers":[],"discounts":[{"code":"SAMPLE50","percent_off":50,"period_days":30,"expires_at":"2027-03-21T21:00:00Z"}]}`)
	if len(cat.Discounts) != 1 {
		t.Fatalf("want one discount, got %+v", cat.Discounts)
	}
	want := Discount{Code: "SAMPLE50", PercentOff: 50, PeriodDays: 30,
		ExpiresAt: time.Date(2027, 3, 21, 21, 0, 0, 0, time.UTC)}
	got := cat.Discounts[0]
	if got.Code != want.Code || got.PercentOff != want.PercentOff || got.PeriodDays != want.PeriodDays || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	old := catalogFrom(t, `{"prices":[],"tiers":[]}`)
	if len(old.Discounts) != 0 {
		t.Errorf("a webhook without discounts must yield none, got %+v", old.Discounts)
	}
}
