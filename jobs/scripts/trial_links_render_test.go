package scripts

import (
	"html/template"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/offer/offertest"
	"github.com/webtor-io/web-ui/services/payments"
)

// trialSurface is one job-rendered surface that sells the promo plan, with
// the data under which it shows its button to a free viewer.
type trialSurface struct {
	from  string
	files []string
	data  func(lang string) any
}

func jobTrialSurfaces() []trialSurface {
	type fileCtx struct {
		Lang string
		Data *FileDownload
	}
	type slowCtx struct {
		Lang string
		Data *SlowDownloadData
	}
	type noPeersCtx struct {
		Lang string
		Data *NoPeersData
	}
	return []trialSurface{
		{offer.FromDownloadNudge, []string{"../../templates/views/action/download_file.html"}, func(lang string) any {
			return &fileCtx{Lang: lang, Data: &FileDownload{URL: "u", TierName: "free", RateMbps: 5, SizeBytes: 4 << 30}}
		}},
		{offer.FromLimitModal, []string{"../../templates/views/action/errors/slow_download.html", "../../templates/partials/icons.html"}, func(lang string) any {
			return &slowCtx{Lang: lang, Data: &SlowDownloadData{TierName: "free", MeasuredSpeedMbps: 5, RequiredSpeedMbps: 9, IsRateLimited: true, RateLimitMbps: 5}}
		}},
		{offer.FromNoPeers, []string{"../../templates/views/action/errors/no_peers.html"}, func(lang string) any {
			return &noPeersCtx{Lang: lang, Data: &NoPeersData{TierName: "free", Reason: "dead", ElapsedSec: 60, Endpoint: "/stream-video", ResourceID: "abc", ItemID: "i1", LogTargetID: "i1"}}
		}},
	}
}

func renderSurface(t *testing.T, s trialSurface, funcs template.FuncMap, lang string) string {
	t.Helper()
	tpl, err := template.New(s.files[0][strings.LastIndex(s.files[0], "/")+1:]).Funcs(funcs).ParseFiles(s.files...)
	if err != nil {
		t.Fatalf("%s: parse: %v", s.from, err)
	}
	return renderModal(t, tpl, lang, s.data(lang))
}

// The guard for "every button that starts the trial goes through /trial"
// (docs/offers.md): with a promo plan whose trial the checkout can start,
// each element of these surfaces whose umami target is "trial" links to
// /trial in the page's language, naming its own surface. Without such a
// trial — the checkout cannot start it, there is no checkout at all, or
// nothing is on sale — nothing links to /trial and each surface keeps the
// link it had.
func TestJobSurfacesStartTheTrialThroughTrial(t *testing.T) {
	trialCheckout := func(tier string, _ int, trial bool) string {
		if trial {
			return "https://checkout.example/" + tier + "?trial"
		}
		return "https://checkout.example/" + tier
	}
	noTrialCheckout := func(tier string, _ int, trial bool) string {
		if trial {
			return ""
		}
		return "https://checkout.example/" + tier
	}
	cases := []struct {
		name     string
		cat      *payments.Catalog
		checkout offer.Checkout
		// target and href of the one CTA each surface must render; "" = none.
		target, href string
	}{
		{"trial", prodCatalog(), trialCheckout, "trial", "/trial"},
		{"checkout cannot start the trial", prodCatalog(), noTrialCheckout, "checkout", "https://checkout.example/silver"},
		{"no membership provider", prodCatalog(), nil, "donate", "/donate"},
		{"nothing on sale", nil, trialCheckout, "", ""},
	}
	for _, c := range cases {
		funcs := offerFuncsWith(t, c.cat, c.checkout)
		for _, s := range jobTrialSurfaces() {
			for _, lang := range i18n.SupportedLangs {
				out := renderSurface(t, s, funcs, lang)
				var ctas []offertest.CTA
				for _, l := range offertest.Links(out) {
					if l.Target != "" {
						ctas = append(ctas, l)
					}
					if c.target != "trial" && offertest.IsTrialLink(l.Href) {
						t.Errorf("%s/%s/%s: %s links to /trial without a trial to start: %q", c.name, s.from, lang, l.Event, l.Href)
					}
				}
				if c.target == "" {
					if len(ctas) != 0 {
						t.Errorf("%s/%s/%s: nothing on sale, yet %+v", c.name, s.from, lang, ctas)
					}
					continue
				}
				if len(ctas) != 1 {
					t.Errorf("%s/%s/%s: want exactly one CTA, got %+v", c.name, s.from, lang, ctas)
					continue
				}
				got := ctas[0]
				if got.Target != c.target {
					t.Errorf("%s/%s/%s: target %q, want %q", c.name, s.from, lang, got.Target, c.target)
				}
				if c.target == "trial" {
					from, ok := offertest.TrialFrom(got.Href)
					if want := i18n.LangPath(lang, offer.TrialPath(s.from)); !ok || from != s.from || got.Href != want {
						t.Errorf("%s/%s/%s: trial CTA links to %q, want %q", c.name, s.from, lang, got.Href, want)
					}
					continue
				}
				if got.Href != c.href {
					t.Errorf("%s/%s/%s: href %q, want %q (the surface's own link)", c.name, s.from, lang, got.Href, c.href)
				}
			}
		}
	}
}
