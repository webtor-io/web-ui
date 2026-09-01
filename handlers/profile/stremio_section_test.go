package profile

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

type stremioTestCtx struct {
	Ctx  struct{ Lang string }
	Data string
}

func newStremioRenderer(t *testing.T) *template.Template {
	t.Helper()
	h := i18n.NewHelper(i18n.New(os.DirFS("../../locales")))
	tmpl, err := template.New("stremio.html").Funcs(template.FuncMap{
		"t":                     h.T,
		"tp":                    h.Tp,
		"langPath":              func(lang, p string) string { return "[" + lang + "]" + p },
		"domain":                func() string { return "https://webtor.io" },
		"domainWithoutProtocol": func() string { return "webtor.io" },
		"json":                  func(v any) template.JS { return template.JS(`"` + strings.ReplaceAll(v.(string), `"`, `\"`) + `"`) },
		"asset":                 func(name string) template.HTML { return template.HTML("<!-- asset " + name + " -->") },
	}).ParseFiles("../../templates/partials/profile/stremio.html")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func renderStremio(t *testing.T, tmpl *template.Template, lang, token string) string {
	t.Helper()
	d := &stremioTestCtx{Data: token}
	d.Ctx.Lang = lang
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "profile/stremio", d); err != nil {
		t.Fatalf("lang=%s: %v", lang, err)
	}
	return buf.String()
}

// Fresh account: Install is the primary action and mints the token through
// the generate form; the link-only path stays available; every locale resolves.
func TestStremioBlockFreshOffersOneClickInstall(t *testing.T) {
	tmpl := newStremioRenderer(t)
	for _, lang := range i18n.SupportedLangs {
		out := renderStremio(t, tmpl, lang, "")
		for _, want := range []string{
			`data-stremio-generate`, `data-stremio-install`,
			`data-umami-event="stremio-install-addon" data-umami-event-stage="fresh"`,
			`data-umami-event="stremio-generate-addon-url"`,
			`https://www.stremio.com/downloads`,
			`/stremio/url/generate`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("lang=%s: fresh block lacks %s", lang, want)
			}
		}
		if strings.Contains(out, "profile.stremio.") {
			t.Errorf("lang=%s: unresolved translation key:\n%s", lang, out)
		}
		if strings.Contains(out, "data-stremio-deeplink") {
			t.Errorf("lang=%s: no token, no deep link", lang)
		}
	}
}

// Token state: the deep link is the primary action and carries the marker the
// module uses to auto-open it; copy and rotation remain.
func TestStremioBlockTokenLeadsWithDeepLink(t *testing.T) {
	tmpl := newStremioRenderer(t)
	out := renderStremio(t, tmpl, "en", "/s/abc/manifest.json")
	for _, want := range []string{
		`href="stremio://webtor.io/s/abc/manifest.json" data-stremio-deeplink`,
		`data-umami-event-stage="token"`,
		`value="https://webtor.io/s/abc/manifest.json"`,
		`/stremio/url/regenerate`,
		`stremio-copy-addon-url`,
		`https://www.stremio.com/downloads`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("token block lacks %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "data-stremio-generate") {
		t.Error("token state must not offer the generate form")
	}
}
