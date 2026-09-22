package scripts

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/offer"
)

// The no-peers modal has three variants keyed on Reason; each must resolve in
// every locale, say the right thing, and offer the retry form.
func TestNoPeersModalRenders(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	var promo *offer.Offer
	tpl, err := template.New("no_peers.html").Funcs(template.FuncMap{
		"t":          helper.T,
		"tp":         helper.Tp,
		"tn":         helper.Tn,
		"langPath":   func(lang, p string) string { return p },
		"promoOffer": func() *offer.Offer { return promo },
	}).ParseFiles("../../templates/views/action/errors/no_peers.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	trial := &offer.Offer{Tier: "silver", PeriodDays: 30, RateMbps: 50, VaultPoints: 250, HasVault: true, TrialDays: 7, URL: "https://checkout.example/trial"}
	type ctx struct {
		Lang string
		Data *NoPeersData
	}
	base := NoPeersData{TierName: "free", Endpoint: "/stream-video", ResourceID: "abc", ItemID: "i1", LogTargetID: "i1"}

	cases := []struct {
		reason string
		offer  *offer.Offer
		data   func() NoPeersData
		want   []string
		banned []string
	}{
		// A dead swarm sells Vault, straight at the promo plan's trial.
		{"dead", trial, func() NoPeersData { d := base; d.Reason = "dead"; d.ElapsedSec = 60; return d },
			[]string{"no active seeders", "donate-no-peers", "https://checkout.example/trial", "Try free for 7 days", "Vault saves the torrent", "another torrent", "no-peers-retry", `reason: 'dead'`},
			[]string{"received from", "Mbps"}},
		// Nothing to sell (no catalog): no button, no Vault promise, and the
		// alternative stands on its own instead of dangling after an "or".
		{"dead", nil, func() NoPeersData { d := base; d.Reason = "dead"; d.ElapsedSec = 60; return d },
			[]string{"no active seeders", "Try another torrent with more seeders", "no-peers-retry"},
			[]string{"donate-no-peers", "Vault saves the torrent", "or try another torrent"}},
		// A promo plan without Vault has nothing to say about a dead swarm.
		{"dead", &offer.Offer{Tier: "bronze", RateMbps: 20, HasVault: false}, func() NoPeersData { d := base; d.Reason = "dead"; return d },
			[]string{"no active seeders"},
			[]string{"donate-no-peers", "Vault saves the torrent"}},
		// Paying: the instruction, never an upsell.
		{"dead", trial, func() NoPeersData { d := base; d.TierName = "silver"; d.Reason = "dead"; return d },
			[]string{"Save it to Vault"},
			[]string{"donate-no-peers", "Try free"}},
		{"slow", trial, func() NoPeersData {
			d := base
			d.Reason, d.Peers, d.Seeders, d.Leechers, d.Bytes, d.BytesRaw, d.ElapsedSec = "slow", 3, 1, 2, "340 KiB", 340*1024, 120
			return d
		}, []string{"crawling", "not your plan", "340 KiB", "from 3 peers", "in 120\u00a0s", "no-peers-retry", `reason: 'slow'`}, []string{"donate-no-peers", "another torrent"}},
		{"timeout", trial, func() NoPeersData { d := base; d.Reason = "timeout"; d.ElapsedSec = 180; return d },
			[]string{"not enough data", "no-peers-retry", `reason: 'timeout'`}, []string{"donate-no-peers", "another torrent"}},
	}
	for _, c := range cases {
		promo = c.offer
		d := c.data()
		out := renderModal(t, tpl, "en", &ctx{Lang: "en", Data: &d})
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q:\n%s", c.reason, w, out)
			}
		}
		for _, b := range c.banned {
			if strings.Contains(out, b) {
				t.Errorf("%s: must not contain %q", c.reason, b)
			}
		}
		for _, lang := range i18n.SupportedLangs {
			if o := renderModal(t, tpl, lang, &ctx{Lang: lang, Data: &d}); strings.Contains(o, "action.noPeers.") || strings.Contains(o, "offer.") {
				t.Errorf("%s lang=%s: unresolved key", c.reason, lang)
			}
		}
	}
}

func renderModal(t *testing.T, tpl *template.Template, lang string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", data); err != nil {
		t.Fatalf("execute lang=%s: %v", lang, err)
	}
	return buf.String()
}
