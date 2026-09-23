package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

// /ext/download is where the extension hands over the .torrent it
// intercepted. When it does not (missing, outdated, a page it does not
// recognise), ext/download.js reveals #ext-fallback instead of leaving the
// page blank. The block has to be there from the server, hidden, in the
// visitor's language, with a working upload form: nothing else on the page
// renders text.
func TestExtDownloadHasAHiddenFallback(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locales.Close() }()
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	funcs := template.FuncMap{
		"t": helper.T,
		"langPath": func(lang, p string) string {
			if lang == "en" {
				return p
			}
			return "/" + lang + p
		},
		"asset": func(p string) template.HTML { return template.HTML(`<script src="/assets/` + p + `"></script>`) },
	}
	tpl, err := template.New("download.html").Funcs(funcs).ParseFiles("../../templates/views/ext/download.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "ru"} {
		ctx := map[string]any{
			"Lang":      lang,
			"CSRF":      "csrf-token",
			"SessionID": "sid",
			"Data":      struct{ DownloadID int }{42},
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, ctx); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		out := buf.String()
		i := strings.Index(out, `<div id="ext-fallback" hidden`)
		if i < 0 {
			t.Fatalf("%s: no hidden #ext-fallback:\n%s", lang, out)
		}
		block := out[i:]
		msg := helper.T(lang, "ext.download.failed")
		for _, want := range []string{
			template.HTMLEscapeString(msg),
			`action="` + map[string]string{"en": "/", "ru": "/ru/"}[lang] + `"`,
			`enctype="multipart/form-data"`,
			`name="_csrf" value="csrf-token"`,
			`name="_sessionID" value="sid"`,
			`type="file" name="resource"`,
			template.HTMLEscapeString(helper.T(lang, "ext.download.upload")),
		} {
			if !strings.Contains(block, want) {
				t.Errorf("%s: fallback lacks %q:\n%s", lang, want, block)
			}
		}
		if msg == "ext.download.failed" || !strings.Contains(out, `<html lang="`+lang+`">`) {
			t.Errorf("%s: untranslated page:\n%s", lang, out)
		}
		if !strings.Contains(strings.Join(strings.Fields(out), " "), "window._downloadID = 42 ;") {
			t.Errorf("%s: the download id is gone:\n%s", lang, out)
		}
	}
}
