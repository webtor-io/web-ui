package donate

import (
	"bytes"
	"context"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/offer/offertest"
	np "github.com/webtor-io/web-ui/services/payments"
)

type catalogSource struct{ c *np.Catalog }

func (s catalogSource) Catalog(context.Context) (*np.Catalog, error) { return s.c, nil }

// renderDonate renders donate/index.html the way the page does: the cards
// from buildCards over cat, the promo offer from the offers service over
// offerCat (the same catalog in production, read through its own cache).
func renderDonate(t *testing.T, lang string, cat, offerCat *np.Catalog, patreonOn bool) string {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locales.Close() })
	h := i18n.NewHelper(i18n.New(locales.FS()))
	var src offer.Source
	if offerCat != nil {
		src = catalogSource{offerCat}
	}
	var checkout offer.Checkout
	if patreonOn {
		checkout = patreonCheckout
	}
	offers := offer.New(src, checkout)
	offers.Refresh(context.Background())
	oh := offer.NewHelper(offers, nil)
	tpl, err := template.New("index.html").Funcs(template.FuncMap{
		"t":          h.T,
		"tp":         h.Tp,
		"tn":         h.Tn,
		"langPath":   i18n.LangPath,
		"asset":      func(string) template.HTML { return "" },
		"promoOffer": oh.PromoOffer,
		"trialURL":   oh.TrialURL,
	}).ParseFiles("../../templates/views/donate/index.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	data := struct {
		Lang, CSRF, ErrKey string
		Data               *donateData
	}{Lang: lang, Data: buildCards(cat, patreonOn, true)}
	if err := tpl.ExecuteTemplate(&buf, "main", data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return buf.String()
}

type donateLink struct{ event, tier, cadence, href string }

func donateLinks(out string) []donateLink {
	var ls []donateLink
	for _, c := range offertest.Links(out) {
		ls = append(ls, donateLink{c.Event, c.Attrs["data-umami-event-tier"], c.Attrs["data-umami-event-cadence"], c.Href})
	}
	return ls
}

// The promo plan's trial starts through /trial on /donate too — the plaque
// and the promo card's monthly Join, which on Patreon is the same trial
// checkout. Every other link names a particular plan and stays direct.
func TestDonateStartsThePromoTrialThroughTrial(t *testing.T) {
	cat := catalog(prodPrices()...)
	for _, lang := range []string{"en", "ru"} {
		trial := i18n.LangPath(lang, "/trial") + "?from=donate"
		want := map[donateLink]bool{
			{"donate-patreon-join", "silver", "1", trial}:                                  true,
			{"donate-trial-plaque", "", "", trial}:                                         true,
			{"donate-patreon-join", "silver", "12", patreonCheckout("silver", 365, false)}: true,
			{"donate-patreon-join", "bronze", "1", patreonCheckout("bronze", 30, false)}:   true,
			{"donate-patreon-join", "bronze", "12", patreonCheckout("bronze", 365, false)}: true,
			{"donate-patreon-join", "gold", "1", patreonCheckout("gold", 30, false)}:       true,
			{"donate-patreon-join", "gold", "12", patreonCheckout("gold", 365, false)}:     true,
		}
		got := map[donateLink]bool{}
		for _, l := range donateLinks(renderDonate(t, lang, cat, cat, true)) {
			if !strings.HasPrefix(l.event, "donate-patreon-join") && l.event != "donate-trial-plaque" {
				continue
			}
			got[l] = true
			if !want[l] {
				t.Errorf("%s: unexpected link %+v", lang, l)
			}
			if offertest.IsTrialLink(l.href) {
				if from, ok := offertest.TrialFrom(l.href); !ok || from != offer.FromDonate {
					t.Errorf("%s: %s links to %q, want /trial?from=donate", lang, l.event, l.href)
				}
			}
		}
		for l := range want {
			if !got[l] {
				t.Errorf("%s: missing link %+v", lang, l)
			}
		}
	}
}

// Nothing on /donate links to /trial unless the trial is the promo plan's
// and /trial would start it — the card's own trial checkout otherwise.
func TestDonateKeepsDirectLinksWithoutAPromoTrial(t *testing.T) {
	notPromo := prodPrices()
	notPromo[4].IsPromo = false // silver monthly keeps its trial
	notPromo[1].IsPromo = true  // gold annual is on sale, without one
	cases := []struct {
		name          string
		cat, offerCat *np.Catalog
		patreonOn     bool
		plaque        string // the plaque's href, "" = no plaque
	}{
		// Silver's trial is not what /trial sells: the card keeps its checkout.
		{"trial not on the promo plan", catalog(notPromo...), catalog(notPromo...), true, patreonCheckout("silver", 30, true)},
		// The offers have not seen the promo trial yet (their catalog is
		// refreshed on its own): the card's checkout, not a /trial that
		// would not start the trial.
		{"offers without a catalog", catalog(prodPrices()...), nil, true, patreonCheckout("silver", 30, true)},
		// Patreon off: no trial to start anywhere.
		{"no patreon", catalog(prodPrices()...), catalog(prodPrices()...), false, ""},
	}
	for _, c := range cases {
		out := renderDonate(t, "en", c.cat, c.offerCat, c.patreonOn)
		plaque := ""
		for _, l := range donateLinks(out) {
			if offertest.IsTrialLink(l.href) {
				t.Errorf("%s: %s links to /trial: %q", c.name, l.event, l.href)
			}
			if l.event == "donate-trial-plaque" {
				plaque = l.href
			}
		}
		if plaque != c.plaque {
			t.Errorf("%s: plaque %q, want %q", c.name, plaque, c.plaque)
		}
	}
}
