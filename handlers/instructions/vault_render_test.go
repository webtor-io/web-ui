package instructions

import (
	"bytes"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

var allLangs = []string{"en", "ru", "es", "de", "fr", "pt", "it", "pl", "tr", "nl", "cs"}

func renderVault(t *testing.T, lang string, v *VaultData) string {
	t.Helper()
	ih := i18n.NewHelper(i18n.New(os.DirFS("../../locales")))
	wh := &web.Helper{}
	tpl, err := template.New("vault.html").Funcs(template.FuncMap{
		"t": ih.T, "tHTML": ih.THTML, "tpHTML": ih.TpHTML,
		"duration": wh.Duration, "langPath": i18n.LangPath,
	}).ParseFiles("../../templates/views/instructions/vault.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, "main", &web.Context{Lang: lang, Data: &Data{Vault: v}}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return b.String()
}

// The help page names the button by the label it has (vault.keepAvailable,
// put in at render time: a copy of it in the step drifted once, to "Keep
// This Torrent Available", a label gone since), and says the periods in the
// page's language (durafmt said "1 day" on every one). No freeze
// configured, no step about it.
func TestVaultHelpNamesTheRealButton(t *testing.T) {
	ih := i18n.NewHelper(i18n.New(os.DirFS("../../locales")))
	wh := &web.Helper{}
	v := &VaultData{FreezePeriod: 24 * time.Hour, ExpirePeriod: 7 * 24 * time.Hour, TransferTimeoutPeriod: 7 * 24 * time.Hour}
	for _, lang := range allLangs {
		out := renderVault(t, lang, v)
		// tpHTML puts the label in as it is (a translation, trusted).
		label := ih.T(lang, "vault.keepAvailable")
		if !regexp.MustCompile(`<li>[^<]*<strong>[^<]*` + regexp.QuoteMeta(label) + `[^<]*</strong>`).MatchString(out) {
			t.Errorf("%s: the first step does not name the button %q", lang, label)
		}
		for _, bad := range []string{"Keep This Torrent Available", "<no value>", "{{", "day"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: %q on the page", lang, bad)
			}
		}
		for _, d := range []time.Duration{v.FreezePeriod, v.ExpirePeriod} {
			if s := wh.Duration(lang, d); !strings.Contains(out, "<strong>"+s+"</strong>") {
				t.Errorf("%s: the period %q is not on the page", lang, s)
			}
		}
	}
	v.FreezePeriod = 0
	if out := renderVault(t, "en", v); strings.Contains(out, "frozen for") {
		t.Error("no freeze configured, but the steps describe one")
	}
}
