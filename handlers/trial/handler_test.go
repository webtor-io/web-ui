package trial

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/metrics/metricstest"
	"github.com/webtor-io/web-ui/services/offer"
	"github.com/webtor-io/web-ui/services/web"
)

type fakePromo struct{ o *offer.Offer }

func (f fakePromo) Promo() *offer.Offer { return f.o }

const checkout = "https://www.patreon.com/checkout/example?rid=1&is_free_trial=true"

var trialPlan = &offer.Offer{Tier: "silver", PeriodDays: 30, TrialDays: 7, URL: checkout}

// serve runs a request through what sits in front of /trial in serve.go:
// the i18n HTTP middleware (strips /ru/), its gin half (the language), the
// default noindex, and a resource catch-all like handlers/resource's, which
// must not win over /trial.
func serve(t *testing.T, o *offer.Offer, target string) *httptest.ResponseRecorder {
	t.Helper()
	root, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(web.NoindexDefault(false))
	r.Use(i18n.GinMiddleware(i18n.New(root.FS())))
	h := &Handler{offers: fakePromo{o}}
	r.GET("/trial", h.trial)
	r.GET("/:resource_id", func(c *gin.Context) { c.String(http.StatusTeapot, "resource") })
	w := httptest.NewRecorder()
	i18n.HTTPMiddleware(nil)(r).ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestTrialGoesStraightToTheCheckout(t *testing.T) {
	for _, path := range []string{"/trial", "/ru/trial", "/trial?utm_source=stremio&utm_medium=video&utm_campaign=paywall", "/ru/trial?from=grace", "/trial?from=reddit"} {
		t.Run(path, func(t *testing.T) {
			w := serve(t, trialPlan, path)
			if w.Code != http.StatusFound || w.Header().Get("Location") != checkout {
				t.Errorf("%d %q, want 302 to the plan's checkout", w.Code, w.Header().Get("Location"))
			}
		})
	}
}

// The plan cannot be bought directly (no checkout link): /donate in the
// visit's language, labelled with the clip's utm parameters unless the visit
// brought its own.
func TestTrialWithoutACheckoutGoesToDonate(t *testing.T) {
	plan := &offer.Offer{Tier: "silver", PeriodDays: 30}
	cases := []struct {
		path, wantPath string
		wantUTM        map[string]string
	}{
		{"/trial", "/donate", map[string]string{"utm_source": "stremio", "utm_medium": "video", "utm_campaign": "paywall"}},
		{"/ru/trial", "/ru/donate", map[string]string{"utm_source": "stremio", "utm_medium": "video", "utm_campaign": "paywall"}},
		{"/trial?utm_source=newsletter&utm_content=b&other=x", "/donate", map[string]string{"utm_source": "newsletter", "utm_medium": "video", "utm_campaign": "paywall", "utm_content": "b"}},
		// A button on the site is not the clip: no clip labels, its own utm
		// (if any) only, and ?from itself is not passed on.
		{"/ru/trial?from=grace", "/ru/donate", map[string]string{}},
		{"/trial?from=reddit&utm_campaign=reddit-x", "/donate", map[string]string{"utm_campaign": "reddit-x"}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			w := serve(t, plan, tc.path)
			if w.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", w.Code)
			}
			u, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if u.Host != "" || u.Path != tc.wantPath {
				t.Errorf("Location = %q, want %s on this site", w.Header().Get("Location"), tc.wantPath)
			}
			q := u.Query()
			if len(q) != len(tc.wantUTM) {
				t.Errorf("query %v, want exactly %v — only utm parameters are passed on", q, tc.wantUTM)
			}
			for k, v := range tc.wantUTM {
				if q.Get(k) != v {
					t.Errorf("%s = %q, want %q", k, q.Get(k), v)
				}
			}
		})
	}
}

// Nothing on sale — no catalog, as on a self-hosted instance: no trial to
// point at, and the route is a plain 404, not the resource catch-all.
func TestTrialWithoutAnOfferIs404(t *testing.T) {
	for _, path := range []string{"/trial", "/ru/trial", "/trial?from=grace"} {
		w := serve(t, nil, path)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, w.Code)
		}
	}
}

func TestTrialIsNoindex(t *testing.T) {
	w := serve(t, trialPlan, "/trial")
	if got := w.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag = %q: a redirect to a checkout is not a page to index", got)
	}
}

// The visit is the measurement (the provider drops utm parameters), so each
// one is counted, by where it went, whether it came from the clip and which
// button on the site it came from.
func TestTrialVisitsAreCounted(t *testing.T) {
	get := func(target, campaign, from string) float64 {
		return metricstest.Counter(t, "webui_trial_shortlink_total", map[string]string{"target": target, "campaign": campaign, "from": from})
	}
	type series struct{ target, campaign, from string }
	cases := []struct {
		o    *offer.Offer
		path string
		want series
	}{
		// The clip's QR code, exactly as scripts/stremio_paywall_video prints
		// it: still campaign=paywall, and no surface.
		{trialPlan, "/ru/trial?" + PaywallUTM, series{"checkout", "paywall", "none"}},
		{trialPlan, "/trial", series{"checkout", "none", "none"}},
		{trialPlan, "/trial?from=" + offer.FromGrace, series{"checkout", "none", "grace"}},
		{trialPlan, "/de/trial?from=" + offer.FromPromoBanner, series{"checkout", "none", "promo-banner"}},
		{trialPlan, "/trial?from=reddit", series{"checkout", "none", "other"}},
		{&offer.Offer{Tier: "silver"}, "/trial", series{"donate", "none", "none"}},
		{&offer.Offer{Tier: "silver"}, "/trial?from=" + offer.FromDonate, series{"donate", "none", "donate"}},
		{nil, "/trial", series{"none", "none", "none"}},
	}
	for _, c := range cases {
		before := get(c.want.target, c.want.campaign, c.want.from)
		serve(t, c.o, c.path)
		if d := get(c.want.target, c.want.campaign, c.want.from) - before; d != 1 {
			t.Errorf("%s: %v moved by %v, want 1", c.path, c.want, d)
		}
	}
}

// ?from arrives from the client: whatever it holds, the counter gets a known
// surface, none or other — never a new series per value.
func TestTrialFromIsBounded(t *testing.T) {
	for i := 0; i < 50; i++ {
		serve(t, trialPlan, "/trial?from=spam-"+strconv.Itoa(i))
	}
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{offer.TrialFromNone: true, offer.TrialFromOther: true}
	for _, f := range offer.TrialFroms {
		allowed[f] = true
	}
	for _, f := range families {
		if f.GetName() != "webui_trial_shortlink_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "from" && !allowed[l.GetValue()] {
					t.Errorf("from=%q became a label", l.GetValue())
				}
			}
		}
	}
}

// The log line names the surface like the counter does; an unknown one is
// kept verbatim, cut short, so a hand-placed link can be found and named.
func TestTrialLogsTheSurface(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	long := strings.Repeat("я", 40) // 80 bytes
	cases := []struct {
		path, from, raw string
	}{
		{"/trial?from=grace", "grace", ""},
		{"/trial?" + PaywallUTM, "none", ""},
		{"/trial?from=reddit-piracy", "other", "reddit-piracy"},
		{"/trial?from=" + url.QueryEscape(long), "other", strings.Repeat("я", 32)},
	}
	for _, c := range cases {
		hook.Reset()
		serve(t, trialPlan, c.path)
		var e *log.Entry
		for _, x := range hook.AllEntries() {
			if x.Message == "trial shortlink" {
				e = x
			}
		}
		if e == nil {
			t.Fatalf("%s: no trial shortlink line", c.path)
		}
		if e.Data["from"] != c.from {
			t.Errorf("%s: from=%v, want %q", c.path, e.Data["from"], c.from)
		}
		raw, has := e.Data["from_raw"]
		if c.raw == "" && has {
			t.Errorf("%s: from_raw=%v for a known surface", c.path, raw)
		}
		if c.raw != "" && raw != c.raw {
			t.Errorf("%s: from_raw=%q, want %q", c.path, raw, c.raw)
		}
	}
}
