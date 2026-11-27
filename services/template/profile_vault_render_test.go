package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/vault"
)

// TestProfileVaultSplitRenders runs the profile's Vault block with the real
// translations: the funded points split into the account's own files and
// torrents, and only when some of them are the account's own files.
func TestProfileVaultSplitRenders(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	tpl, err := template.New("vault.html").Funcs(template.FuncMap{
		"t":            helper.T,
		"tp":           helper.Tp,
		"langPath":     func(_, p string) string { return p },
		"derefFloat64": func(f *float64) float64 { return *f },
	}).ParseFiles("../../templates/partials/profile/vault.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}
	render := func(t *testing.T, s *vault.UserStats) string {
		t.Helper()
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "profile/vault", map[string]any{
			"Ctx":  map[string]any{"Lang": "en"},
			"Data": s,
		}); err != nil {
			t.Fatalf("failed to render partial: %v", err)
		}
		return buf.String()
	}
	// row is the table row that names label.
	row := func(out, label string) string {
		for _, r := range strings.Split(out, "<tr>") {
			if strings.Contains(r, ">"+label+"<") {
				return r
			}
		}
		return ""
	}

	out := render(t, &vault.UserStats{FundedOwnFiles: 9, FundedTorrents: 2})
	for label, want := range map[string]string{"My files": "9.00", "Torrents": "2.00"} {
		if r := row(out, label); !strings.Contains(r, ">"+want+"<") {
			t.Errorf("no row %q with %s:\n%s", label, want, out)
		}
	}

	if out := render(t, &vault.UserStats{FundedTorrents: 2}); strings.Contains(out, "My files") {
		t.Errorf("no own files funded, but the split is there:\n%s", out)
	}
}
