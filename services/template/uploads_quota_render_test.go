package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/helpers"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/uploads"
)

// TestUploadsQuotaPartialRenders runs the profile's upload quota line with
// the real translations and size formatter: the sizes are tp parameters, and
// a parameter that does not arrive prints "<no value>".
func TestUploadsQuotaPartialRenders(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	tpl, err := template.New("uploads_quota.html").Funcs(template.FuncMap{
		"t":             helper.T,
		"tp":            helper.Tp,
		"bitsForHumans": func(b int64) string { return helpers.Bytes(uint64(b)) },
	}).ParseFiles("../../templates/partials/profile/uploads_quota.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}
	render := func(t *testing.T, u *uploads.Usage) string {
		t.Helper()
		var buf bytes.Buffer
		// withContext's shape: the page context and the partial's data.
		if err := tpl.ExecuteTemplate(&buf, "profile/uploads_quota", map[string]any{
			"Ctx":  map[string]any{"Lang": "en"},
			"Data": u,
		}); err != nil {
			t.Fatalf("failed to render partial: %v", err)
		}
		return buf.String()
	}

	t.Run("usage", func(t *testing.T) {
		out := render(t, &uploads.Usage{Used: 3 << 30, Cap: 10 << 30})
		for _, want := range []string{
			"Temporary uploads: 3.0\u00a0GB of 10\u00a0GB used",
			"Space from deleted files and folders is freed within 20 minutes.",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("no usage", func(t *testing.T) {
		if out := render(t, nil); strings.TrimSpace(out) != "" {
			t.Errorf("rendered %q without a usage", out)
		}
	})
}
