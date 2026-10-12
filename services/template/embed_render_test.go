package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

func embedViewFuncs(t *testing.T) (template.FuncMap, *i18n.Helper) {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locales.Close() })
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	return template.FuncMap{
		"t":      helper.T,
		"tpHTML": helper.TpHTML,
		"domain": func() string { return "https://tv.example.lan" },
	}, helper
}

func renderEmbedView(t *testing.T, funcs template.FuncMap, file, lang string) string {
	t.Helper()
	tpl, err := template.New(file).Funcs(funcs).ParseFiles("../../templates/views/embed/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, map[string]any{"Lang": lang}); err != nil {
		t.Fatalf("%s %s: %v", file, lang, err)
	}
	return buf.String()
}

// What a visitor of another site sees in the frame when this instance does
// not serve embeds: a page of its own, in their language, telling the
// site's owner where embeds are served from -- and nothing that runs.
func TestEmbedOffPage(t *testing.T) {
	funcs, helper := embedViewFuncs(t)
	for _, lang := range []string{"en", "ru"} {
		out := renderEmbedView(t, funcs, "off.html", lang)
		if !strings.HasPrefix(out, "<!DOCTYPE html>") {
			t.Errorf("%s: the page does not start with its doctype:\n%.80s", lang, out)
		}
		for _, want := range []string{
			template.HTMLEscapeString(helper.T(lang, "embed.off.title")),
			`<a href="https://github.com/webtor-io/self-hosted"`,
			template.HTMLEscapeString(helper.T(lang, "embed.off.link")) + "</a>",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q:\n%s", lang, want, out)
			}
		}
		if strings.Contains(out, "<script") {
			t.Errorf("%s: the off page runs a script:\n%s", lang, out)
		}
	}
}

// A domain off the allowed list is sent to this instance's profile, where
// the list lives -- not to webtor.io's.
func TestEmbedUnauthorizedLinksThisInstance(t *testing.T) {
	funcs, helper := embedViewFuncs(t)
	for _, lang := range []string{"en", "ru"} {
		out := renderEmbedView(t, funcs, "unauthorized.html", lang)
		if !strings.Contains(out, `href="https://tv.example.lan/profile"`) {
			t.Errorf("%s: no link to this instance's profile:\n%s", lang, out)
		}
		if strings.Contains(out, "webtor.io") {
			t.Errorf("%s: names webtor.io:\n%s", lang, out)
		}
		if title := template.HTMLEscapeString(helper.T(lang, "embed.unauthorized.title")); !strings.Contains(out, title) {
			t.Errorf("%s: missing %q:\n%s", lang, title, out)
		}
	}
}
