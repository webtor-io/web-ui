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

// The log host partial is called with the root context ($) — a view's dot is
// its Data, and "$.Lang" inside the partial once resolved against PostData
// and failed at request time. Render it the way index.html and the retry
// layout do.
func TestLoadProgressPartialRenders(t *testing.T) {
	tpl, err := template.New("progress.html").Funcs(template.FuncMap{
		"t":             func(lang, key string, args ...interface{}) string { return key },
		"makeJobLogURL": func(lang string, j interface{}) string { return "/" + lang + "/queue/load/job/x/log" },
		"asset":         func(p string) template.HTML { return template.HTML("<script src=\"" + p + "\"></script>") },
	}).ParseFiles("../templates/partials/load/progress.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	type postData struct{ Job interface{} }
	ctx := struct {
		Lang string
		Data postData
	}{Lang: "ru", Data: postData{Job: struct{}{}}}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "load/progress", &ctx); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, w := range []string{`data-async-progress-log="/ru/queue/load/job/x/log"`, "home.gotIt", "load.js", `class="log-target"`} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	if strings.Contains(out, "<form") {
		t.Error("the host must be a div: cards inside carry their own forms")
	}
}
