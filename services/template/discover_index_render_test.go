package template_test

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/web"
)

// TestDiscoverIndexCarriesThePassthroughAnswer renders the Discover page's
// bootstrap with the real json helper: the transcoder's HEVC passthrough
// answer reaches the page as window._passthrough (handlers/discover
// indexData.Passthrough).
func TestDiscoverIndexCarriesThePassthroughAnswer(t *testing.T) {
	h := &web.Helper{}
	funcs := template.FuncMap{
		"t":     func(lang, key string, args ...interface{}) string { return key },
		"json":  h.Json,
		"asset": func(p string) template.HTML { return template.HTML("<script src=\"" + p + "\"></script>") },
	}
	tpl, err := template.New("index.html").Funcs(funcs).ParseFiles("../../templates/views/discover/index.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	type view struct {
		HEVC string `json:"hevc"`
	}
	data := struct {
		Addons      []string
		Indexers    []string
		Prefs       struct{}
		AIEnabled   bool
		Passthrough view
	}{Passthrough: view{HEVC: "unknown"}}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{"Data": data, "Lang": "en"}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := `window._passthrough = {"hevc":"unknown"};`; !strings.Contains(buf.String(), want) {
		t.Errorf("the page does not carry %s:\n%s", want, buf.String())
	}
}
