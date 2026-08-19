package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

// TestOpenInstanceBannerRenders parses and executes the open-instance banner
// partial standalone, the same way TestPasswordPartialRenders exercises
// profile/password: nothing in the normal test suite renders
// templates/layouts/main.html end to end, so a template error (a bad
// accessor, a missing i18n key) would otherwise only surface the first time
// a real self-hosted instance without a password serves a page.
//
// It also pins the context shape main.html actually passes: main.html calls
// `{{ template "open_instance_banner" $ }}` with the bare *web.Context (via
// `.` at the layout's top level, which is the same value as `$` there), so
// the partial must read `$.OpenInstance` / `$.Lang` directly, matching how
// nav.html and footer.html already read `$.Lang`.
func TestOpenInstanceBannerRenders(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))

	var gotLangPath []string
	funcs := template.FuncMap{
		"t": helper.T,
		"langPath": func(lang, p string) string {
			gotLangPath = append(gotLangPath, lang+":"+p)
			return p
		},
	}
	tpl, err := template.New("open_instance_banner.html").Funcs(funcs).
		ParseFiles("../../templates/partials/open_instance_banner.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}

	t.Run("open instance shows the banner", func(t *testing.T) {
		gotLangPath = nil
		ctx := map[string]interface{}{
			"Lang":         "en",
			"OpenInstance": true,
		}
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "open_instance_banner", ctx); err != nil {
			t.Fatalf("failed to render partial: %v", err)
		}
		out := buf.String()
		if strings.Contains(out, "<no value>") {
			t.Errorf("a template parameter did not arrive:\n%s", out)
		}
		if strings.Contains(out, "banner.") {
			t.Errorf("an untranslated message key reached the page:\n%s", out)
		}
		for _, want := range []string{
			`id="open-instance-banner"`,
			"This instance is open",
			"Set password",
			"Hide for now",
			"sessionStorage.setItem('open-instance-banner-hidden','1')",
			"sessionStorage.getItem('open-instance-banner-hidden')",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output is missing %q:\n%s", want, out)
			}
		}
		if len(gotLangPath) == 0 || gotLangPath[0] != "en:/profile" {
			t.Errorf("langPath was not called with (Lang, /profile): got %v", gotLangPath)
		}
	})

	t.Run("gated instance renders nothing", func(t *testing.T) {
		ctx := map[string]interface{}{
			"Lang":         "en",
			"OpenInstance": false,
		}
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "open_instance_banner", ctx); err != nil {
			t.Fatalf("failed to render partial: %v", err)
		}
		out := strings.TrimSpace(buf.String())
		if out != "" {
			t.Errorf("expected no output when OpenInstance is false, got:\n%s", out)
		}
	})

	t.Run("Russian translation resolves", func(t *testing.T) {
		ctx := map[string]interface{}{
			"Lang":         "ru",
			"OpenInstance": true,
		}
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "open_instance_banner", ctx); err != nil {
			t.Fatalf("failed to render partial: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "Инстанс открыт") {
			t.Errorf("Russian banner text missing:\n%s", out)
		}
	})
}
