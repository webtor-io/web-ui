package scripts

import (
	"context"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/payments"
)

type catalogSource struct{ c *payments.Catalog }

func (s catalogSource) Catalog(context.Context) (*payments.Catalog, error) { return s.c, nil }

func i64p(v int64) *int64 { return &v }

// offerFuncs wires the real offer helper over a catalog (nil = no storefront)
// with the real i18n bundle, the way serve.go does.
func offerFuncs(t *testing.T, c *payments.Catalog, trialCheckout bool) template.FuncMap {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	t.Cleanup(func() { locales.Close() })
	svc := i18n.New(locales.FS())
	h := i18n.NewHelper(svc)
	var src offer.Source
	if c != nil {
		src = catalogSource{c}
	}
	checkout := func(tier string, period int, trial bool) string {
		if trial && !trialCheckout {
			return ""
		}
		if trial {
			return "https://checkout.example/" + tier + "?trial"
		}
		return "https://checkout.example/" + tier
	}
	offers := offer.New(src, checkout)
	offers.Refresh(context.Background())
	oh := offer.NewHelper(offers, func(lang, key string, data map[string]any) string {
		return i18n.TranslateWithLocalizerData(svc.Localizer(lang), key, data)
	})
	return template.FuncMap{
		"t":             h.T,
		"tp":            h.Tp,
		"tn":            h.Tn,
		"langPath":      func(lang, p string) string { return p },
		"promoOffer":    oh.PromoOffer,
		"hasPlans":      oh.HasPlans,
		"downloadPitch": oh.DownloadPitch,
	}
}

func prodCatalog() *payments.Catalog {
	return &payments.Catalog{
		Prices: []payments.Price{
			{TierID: 1, TierName: "bronze", PeriodDays: 365, AmountUSD: 18},
			{TierID: 2, TierName: "silver", PeriodDays: 30, AmountUSD: 5, TrialDays: 7, IsPromo: true},
			{TierID: 3, TierName: "gold", PeriodDays: 30, AmountUSD: 15},
		},
		Tiers: []payments.Tier{
			{TierID: 0, Name: "free", DownloadRate: i64p(5), VaultPoints: i64p(0)},
			{TierID: 1, Name: "bronze", DownloadRate: i64p(20), VaultPoints: i64p(50)},
			{TierID: 2, Name: "silver", DownloadRate: i64p(50), VaultPoints: i64p(250)},
			{TierID: 3, Name: "gold", DownloadRate: i64p(100), VaultPoints: i64p(1000)},
		},
	}
}

func assertRender(t *testing.T, name, out string, want, banned []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("%s: missing %q:\n%s", name, w, out)
		}
	}
	for _, b := range banned {
		if strings.Contains(out, b) {
			t.Errorf("%s: must not contain %q:\n%s", name, b, out)
		}
	}
}

// The download nudge sells the promo plan to a free viewer in time, not in
// adjectives, and links straight to its trial.
func TestDownloadNudgeRenders(t *testing.T) {
	type ctx struct {
		Lang string
		Data *FileDownload
	}
	const movie = int64(43) << 30 / 10 // 4.3 GiB
	cases := []struct {
		name          string
		cat           *payments.Catalog
		trialCheckout bool
		data          FileDownload
		want, banned  []string
	}{
		{"free, movie", prodCatalog(), true, FileDownload{URL: "u", TierName: "free", RateMbps: 5, SizeBytes: movie},
			[]string{"Downloading at 5\u00a0Mbps", "4.3\u00a0GB takes about 2\u00a0h 3\u00a0min", "about 12\u00a0min at 50\u00a0Mbps",
				"https://checkout.example/silver?trial", "Try free for 7 days", `data-umami-event="donate-download"`,
				`data-umami-event-target="trial"`, "donate-download-shown", "eta: 1"},
			[]string{"ads", "action.", "offer."}},
		// Too small for the wait to matter: the plan's speed, no clock.
		{"free, small file", prodCatalog(), true, FileDownload{URL: "u", TierName: "free", RateMbps: 5, SizeBytes: 20 << 20},
			[]string{"Up to 50\u00a0Mbps with a subscription", "Try free for 7 days", "eta: 0"},
			[]string{"takes about"}},
		// Patreon cannot start the trial: the plan's own checkout, its speed on the button.
		{"free, no trial checkout", prodCatalog(), false, FileDownload{URL: "u", TierName: "free", RateMbps: 5, SizeBytes: movie},
			[]string{"https://checkout.example/silver", "Get 50\u00a0Mbps", `data-umami-event-target="checkout"`},
			[]string{"Try free", "?trial"}},
		{"paying viewer", prodCatalog(), true, FileDownload{URL: "u", TierName: "silver", RateMbps: 50, SizeBytes: movie},
			nil, []string{"donate-download", "Downloading at"}},
		{"no storefront", nil, true, FileDownload{URL: "u", TierName: "free", RateMbps: 5, SizeBytes: movie},
			nil, []string{"donate-download", "Downloading at"}},
	}
	for _, c := range cases {
		tpl, err := template.New("download_file.html").Funcs(offerFuncs(t, c.cat, c.trialCheckout)).
			ParseFiles("../../templates/views/action/download_file.html")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		d := c.data
		assertRender(t, c.name, renderModal(t, tpl, "en", &ctx{Lang: "en", Data: &d}), c.want, c.banned)
		for _, lang := range i18n.SupportedLangs {
			o := renderModal(t, tpl, lang, &ctx{Lang: lang, Data: &d})
			if strings.Contains(o, "action.download.") || strings.Contains(o, "offer.") {
				t.Errorf("%s lang=%s: unresolved key:\n%s", c.name, lang, o)
			}
		}
	}
}

// Under a plan cap: a free viewer is sold the promo plan's trial, a paying
// one is sent to compare plans, and nothing is sold without a storefront.
func TestSlowDownloadUpsellRenders(t *testing.T) {
	type ctx struct {
		Lang string
		Data *SlowDownloadData
	}
	base := SlowDownloadData{MeasuredSpeedMbps: 5, RequiredSpeedMbps: 9, IsRateLimited: true, RateLimitMbps: 5}
	cases := []struct {
		name         string
		cat          *payments.Catalog
		tier         string
		limited      bool
		want, banned []string
	}{
		{"free capped", prodCatalog(), "free", true,
			[]string{"https://checkout.example/silver?trial", "Try free for 7 days", `data-umami-event="donate-slow-download"`, `data-umami-event-target="trial"`},
			[]string{"Upgrade plan"}},
		{"paid capped", prodCatalog(), "bronze", true,
			[]string{"Upgrade plan", `href="/donate"`}, []string{"Try free", "checkout.example"}},
		{"not a cap", prodCatalog(), "free", false,
			nil, []string{"donate-slow-download"}},
		{"no storefront", nil, "free", true,
			nil, []string{"donate-slow-download"}},
		{"no storefront, paid", nil, "bronze", true,
			nil, []string{"donate-slow-download"}},
	}
	for _, c := range cases {
		tpl, err := template.New("slow_download.html").Funcs(offerFuncs(t, c.cat, true)).
			ParseFiles("../../templates/views/action/errors/slow_download.html", "../../templates/partials/icons.html")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		d := base
		d.TierName, d.IsRateLimited = c.tier, c.limited
		assertRender(t, c.name, renderModal(t, tpl, "en", &ctx{Lang: "en", Data: &d}), c.want, c.banned)
		for _, lang := range i18n.SupportedLangs {
			if o := renderModal(t, tpl, lang, &ctx{Lang: lang, Data: &d}); strings.Contains(o, "offer.") {
				t.Errorf("%s lang=%s: unresolved key", c.name, lang)
			}
		}
	}
}
