package profile

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/notification"
)

// The partial reads claims through two field paths and two helpers; the test
// supplies the same shapes by hand so it exercises the template, not the
// claims client.
type tierTestClaims struct {
	Context struct{ Tier struct{ Name string } }
	Claims  struct{ Connection struct{ Rate *uint64 } }
}

type tierTestCtx struct {
	Lang   string
	Claims *tierTestClaims
	Data   *Data
}

func newTierRenderer(t *testing.T) *template.Template {
	t.Helper()
	h := i18n.NewHelper(i18n.New(os.DirFS("../../locales")))
	tmpl, err := template.New("tier.html").Funcs(template.FuncMap{
		"t":        h.T,
		"tp":       h.Tp,
		"langPath": func(lang, p string) string { return "[" + lang + "]" + p },
		"tierName": func(c *tierTestClaims) string { return c.Context.Tier.Name },
		"hasAds":   func(*tierTestClaims) bool { return false },
	}).ParseFiles("../../templates/partials/profile/tier.html")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func renderTier(t *testing.T, tmpl *template.Template, lang, tier string, billing notification.Billing) string {
	t.Helper()
	cl := &tierTestClaims{}
	cl.Context.Tier.Name = tier
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "profile/tier", &tierTestCtx{Lang: lang, Claims: cl, Data: &Data{Billing: billing}}); err != nil {
		t.Fatalf("lang=%s tier=%s: %v", lang, tier, err)
	}
	return buf.String()
}

var patreonBilling = notification.Billing{Provider: "Patreon", ManageURL: "https://www.patreon.com/settings/memberships", TrialDays: 7}

func TestTierCardNamesTheBillingProviderOnPaidTiers(t *testing.T) {
	tmpl := newTierRenderer(t)
	for _, lang := range i18n.SupportedLangs {
		out := renderTier(t, tmpl, lang, "silver", patreonBilling)
		if !strings.Contains(out, patreonBilling.ManageURL) || !strings.Contains(out, `data-umami-event="profile-billing-manage"`) {
			t.Errorf("lang=%s: paid card must link the provider's manage page", lang)
		}
		if strings.Count(out, "Patreon") < 2 {
			t.Errorf("lang=%s: both the sentence and the button must name the provider", lang)
		}
		if strings.Contains(out, "profile.billing.") || strings.Contains(out, "{{.Provider}}") {
			t.Errorf("lang=%s: unresolved translation:\n%s", lang, out)
		}
	}
}

// Nothing to manage on the free tier, and nothing to say where no provider is
// configured — a self-hosted instance handing out tiers has no Patreon page.
func TestTierCardStaysSilentOnFreeTierAndWithoutProvider(t *testing.T) {
	tmpl := newTierRenderer(t)
	for name, tc := range map[string]struct {
		tier    string
		billing notification.Billing
	}{
		"free tier":   {"free", patreonBilling},
		"no provider": {"silver", notification.Billing{}},
	} {
		out := renderTier(t, tmpl, "en", tc.tier, tc.billing)
		if strings.Contains(out, "profile-billing-manage") || strings.Contains(out, "patreon.com") {
			t.Errorf("%s: billing line must not render:\n%s", name, out)
		}
	}
}
