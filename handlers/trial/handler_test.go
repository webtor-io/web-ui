package trial

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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
	for _, path := range []string{"/trial", "/ru/trial", "/trial?utm_source=stremio&utm_medium=video&utm_campaign=paywall"} {
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
	for _, path := range []string{"/trial", "/ru/trial"} {
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
// one is counted, by where it went and whether it came from the clip.
func TestTrialVisitsAreCounted(t *testing.T) {
	get := func(target, campaign string) float64 {
		return metricstest.Counter(t, "webui_trial_shortlink_total", map[string]string{"target": target, "campaign": campaign})
	}
	checkoutPaywall, donateNone, none := get("checkout", "paywall"), get("donate", "none"), get("none", "none")

	serve(t, trialPlan, "/trial?utm_campaign=paywall")
	serve(t, &offer.Offer{Tier: "silver"}, "/trial")
	serve(t, nil, "/trial")

	if d := get("checkout", "paywall") - checkoutPaywall; d != 1 {
		t.Errorf("checkout/paywall moved by %v, want 1", d)
	}
	if d := get("donate", "none") - donateNone; d != 1 {
		t.Errorf("donate/none moved by %v, want 1", d)
	}
	if d := get("none", "none") - none; d != 1 {
		t.Errorf("none/none moved by %v, want 1", d)
	}
}
