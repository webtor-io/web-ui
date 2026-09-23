package event

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/payments"
)

// The welcome fires exactly once per account's life as a payer: on the step
// from free (or no tier yet) to paid. Everything else — paid↔paid moves, going
// free, staying put — is not a first welcome.
func TestWelcomeNeeded(t *testing.T) {
	cases := []struct {
		prev, next string
		want       bool
	}{
		{"free", "silver", true},
		{"", "bronze", true},
		{"free", "free", false},
		{"", "free", false},
		{"", "", false},
		{"silver", "gold", false},
		{"gold", "bronze", false},
		{"silver", "free", false},
		{"silver", "silver", false},
	}
	for _, c := range cases {
		if got := welcomeNeeded(c.prev, c.next); got != c.want {
			t.Errorf("welcomeNeeded(%q, %q) = %v, want %v", c.prev, c.next, got, c.want)
		}
	}
}

func TestParseChargeDate(t *testing.T) {
	if got := parseChargeDate("2026-09-09T00:00:00.000+00:00"); got == nil || !got.Equal(time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("patreon format: got %v", got)
	}
	if got := parseChargeDate(""); got != nil {
		t.Errorf("empty must be unknown, got %v", got)
	}
	if got := parseChargeDate("soon"); got != nil {
		t.Errorf("garbage must be unknown, got %v", got)
	}
}

// Only the END of a membership that never paid earns the letter — the
// provider's codes are for people without a paid membership — and only the
// two ends we can tell apart: a cancelled trial running out, and a declined
// card the provider gave up on.
func TestWinbackReason(t *testing.T) {
	zero, paid := 0, 500
	yes, no := true, false
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	on := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		m    userUpdatedMsg
		from time.Time
		want notification.WinBackReason
	}{
		{"trial cancelled, reason on", userUpdatedMsg{Event: "members:delete", PatronStatus: "active_patron", IsFreeTrial: &yes, LifetimeSupportCents: &zero}, on, notification.WinBackTrialEnded},
		{"trial cancelled, reason off", userUpdatedMsg{Event: "members:delete", IsFreeTrial: &yes, LifetimeSupportCents: &zero}, time.Time{}, 0},
		{"trial cancelled, before the start", userUpdatedMsg{Event: "members:delete", IsFreeTrial: &yes, LifetimeSupportCents: &zero}, now.Add(time.Hour), 0},
		{"member deleted after paying", userUpdatedMsg{Event: "members:delete", IsFreeTrial: &no, LifetimeSupportCents: &paid}, on, 0},
		{"provider gave up on a declined card", userUpdatedMsg{Event: "members:update", PatronStatus: "former_patron", LastChargeStatus: "Declined", LifetimeSupportCents: &zero}, time.Time{}, notification.WinBackPaymentFailed},
		{"still retrying the card", userUpdatedMsg{Event: "members:update", PatronStatus: "declined_patron", LastChargeStatus: "Declined", LifetimeSupportCents: &zero}, on, 0},
		{"former after a fraud flag", userUpdatedMsg{PatronStatus: "former_patron", LastChargeStatus: "Fraud", LifetimeSupportCents: &zero}, on, 0},
		{"former with no charge status", userUpdatedMsg{PatronStatus: "former_patron", LifetimeSupportCents: &zero}, on, 0},
		{"former who paid once", userUpdatedMsg{PatronStatus: "former_patron", LastChargeStatus: "Declined", LifetimeSupportCents: &paid}, on, 0},
		{"lifetime unknown", userUpdatedMsg{PatronStatus: "former_patron", LastChargeStatus: "Declined"}, on, 0},
	}
	for _, c := range cases {
		if got := winbackReason(c.m, now, c.from); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The wire format the webhook publishes (webhook services/user_updated.go):
// a known 0 of lifetime support must arrive as a known 0.
func TestUserUpdatedMsgDecodesChargeFacts(t *testing.T) {
	var m userUpdatedMsg
	raw := `{"email":"u@example.com","source":"patreon","event":"members:update","patron_status":"former_patron","last_charge_status":"Declined","lifetime_support_cents":0}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if got := winbackReason(m, time.Now(), time.Time{}); got != notification.WinBackPaymentFailed {
		t.Errorf("decoded message: reason %v, %+v", got, m)
	}
}

type fakeCatalog struct{ c *payments.Catalog }

func (f fakeCatalog) Catalog(context.Context) (*payments.Catalog, error) { return f.c, nil }

func loadedOffers(discounts ...payments.Discount) *offer.Service {
	i64 := func(v int64) *int64 { return &v }
	s := offer.New(fakeCatalog{&payments.Catalog{
		Prices: []payments.Price{
			{TierID: 2, TierName: "silver", PeriodDays: 30, AmountUSD: 5, TrialDays: 7, IsPromo: true},
			{TierID: 3, TierName: "gold", PeriodDays: 30, AmountUSD: 15},
		},
		Tiers: []payments.Tier{
			{TierID: 0, Name: "free", DownloadRate: i64(5)},
			{TierID: 2, Name: "silver", DownloadRate: i64(50)},
			{TierID: 3, Name: "gold", DownloadRate: i64(100)},
		},
		Discounts: discounts,
	}}, func(tier string, days int, trial bool) string { return fmt.Sprintf("co:%s:%d:%v", tier, days, trial) })
	s.Refresh(context.Background())
	return s
}

// The letter's facts come from the catalog: no live code, no letter; the cap
// is the free tier's; the button is the promo plan's full-price checkout (no
// trial — the reader cannot start another) whatever tier the reader had; the
// tier is named only for a cancelled trial.
func TestWinbackLetter(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	code := payments.Discount{Code: "SAMPLE50", PercentOff: 50, PeriodDays: 30, ExpiresAt: now.AddDate(0, 6, 0)}

	if w := winbackLetter(loadedOffers(), "silver", notification.WinBackTrialEnded, now); w != nil {
		t.Errorf("no live code must mean no letter, got %+v", w)
	}

	w := winbackLetter(loadedOffers(code), "gold", notification.WinBackTrialEnded, now)
	if w == nil {
		t.Fatal("want a letter")
	}
	if w.Tier != "gold" || w.CapMbps != 5 || w.CheckoutURL != "co:silver:30:false" || w.Discount.Code != "SAMPLE50" {
		t.Errorf("trial ended: %+v", w)
	}

	w = winbackLetter(loadedOffers(code), "free", notification.WinBackPaymentFailed, now)
	if w == nil || w.Tier != "" || w.Reason != notification.WinBackPaymentFailed || w.CheckoutURL != "co:silver:30:false" {
		t.Errorf("payment failed: %+v", w)
	}
	w = winbackLetter(loadedOffers(code), "silver", notification.WinBackPaymentFailed, now)
	if w == nil || w.Tier != "" {
		t.Errorf("a declined card names no plan even if the account still looks paid: %+v", w)
	}
}

// Off by default: no control group, and no cancelled-trial letters until a
// start is configured; an unparseable start keeps them off.
func TestWinbackFlagDefaults(t *testing.T) {
	app := cli.NewApp()
	app.Flags = RegisterFlags(nil)
	ran := false
	app.Action = func(c *cli.Context) error {
		ran = true
		if got := c.Int(winbackHoldoutFlag); got != 0 {
			t.Errorf("holdout default = %d, want 0", got)
		}
		if got := parseWinbackFrom(c.String(winbackTrialEndedFromFlag)); !got.IsZero() {
			t.Errorf("trial-ended start default = %v, want off", got)
		}
		return nil
	}
	if err := app.Run([]string{"web-ui"}); err != nil || !ran {
		t.Fatalf("run: %v", err)
	}
	if got := parseWinbackFrom("2026-10-08T00:00:00Z"); !got.Equal(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("parse: %v", got)
	}
	if got := parseWinbackFrom("08.10.2026"); !got.IsZero() {
		t.Errorf("garbage must keep the reason off, got %v", got)
	}
}

// The control group must be recomputable in SQL when the effect is measured:
// these buckets were computed by Postgres with the expression in the doc
// comment of winbackHeldOut.
func TestWinbackHeldOutMatchesSQL(t *testing.T) {
	buckets := map[string]int{
		"00000000-0000-0000-0000-000000000000": 40,
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8": 41,
		"f47ac10b-58cc-4372-a567-0e02b2c3d479": 67,
		"123e4567-e89b-12d3-a456-426614174000": 45,
	}
	for id, b := range buckets {
		u := uuid.FromStringOrNil(id)
		if !winbackHeldOut(u, b+1) || winbackHeldOut(u, b) {
			t.Errorf("%s: bucket must be %d", id, b)
		}
		if winbackHeldOut(u, 0) || !winbackHeldOut(u, 100) {
			t.Errorf("%s: 0%% holds out nobody, 100%% everybody", id)
		}
	}
	held := 0
	for i := 0; i < 10000; i++ {
		if winbackHeldOut(uuid.NewV5(uuid.NamespaceOID, fmt.Sprint(i)), 50) {
			held++
		}
	}
	if held < 4700 || held > 5300 {
		t.Errorf("a 50%% holdout kept out %d of 10000", held)
	}
}
