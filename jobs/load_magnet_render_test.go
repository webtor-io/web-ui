package j

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

// The magnet card has two faces: a dead magnet offers the ten-minute retry
// (and says so differently after that retry failed too); a broken link
// offers nothing to retry. Every locale must resolve every key.
func TestMagnetErrorCardRenders(t *testing.T) {
	locales, err := os.OpenRoot("../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	tpl, err := template.New("magnet.html").Funcs(template.FuncMap{
		"t":        helper.T,
		"tp":       helper.Tp,
		"tn":       helper.Tn,
		"langPath": func(lang, p string) string { return p },
	}).ParseFiles("../templates/views/load/errors/magnet.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	type ctx struct {
		Lang string
		Data *MagnetErrorData
	}
	render := func(lang string, d *MagnetErrorData) string {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "main", &ctx{Lang: lang, Data: d}); err != nil {
			t.Fatalf("render %s: %v", lang, err)
		}
		return buf.String()
	}
	dead := render("en", &MagnetErrorData{Kind: "dead", WaitedSec: 60, Query: "magnet:?xt=urn:btih:00"})
	for _, w := range []string{"Nobody is sharing", "for a minute", "60 s", `name="magnet-wait" value="long"`, `data-async-target="#log-load"`, "magnet-retry-long", "Try again for 10 minutes", `kind: 'dead'`} {
		if !strings.Contains(dead, w) {
			t.Errorf("dead: missing %q", w)
		}
	}
	long := render("en", &MagnetErrorData{Kind: "dead", WaitedSec: 600, Long: true, Query: "magnet:?xt=urn:btih:00"})
	if !strings.Contains(long, "Ten minutes") || !strings.Contains(long, "magnet-retry-long") {
		t.Error("after the long attempt the card must say so and still offer a retry")
	}
	invalid := render("en", &MagnetErrorData{Kind: "invalid"})
	if !strings.Contains(invalid, "magnet link is broken") || strings.Contains(invalid, "magnet-retry-long") || strings.Contains(invalid, "Searched for") {
		t.Errorf("invalid: must explain the link and offer no retry:\n%s", invalid)
	}
	for _, lang := range []string{"ru", "es", "de", "fr", "pt", "it", "pl", "tr", "nl", "cs"} {
		out := render(lang, &MagnetErrorData{Kind: "dead", WaitedSec: 60, Query: "m"})
		if strings.Contains(out, "load.magnet.") {
			t.Errorf("%s: unresolved key in %s", lang, out)
		}
	}
}
