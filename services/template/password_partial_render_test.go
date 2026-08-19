package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

// TestPasswordPartialRenders parses and executes the profile administrator-
// password section standalone, for the same reason as the subscriptions and
// torznab partial tests: get.html includes it lazily behind the SelfHosted
// gate, so nothing exercises it at startup, and a template error only
// surfaces the first time a self-hosted instance actually renders /profile.
//
// It also pins the context shape this partial actually receives: get.html
// includes it with `{{ template "profile/password" $ }}` (bare context, like
// profile/webdav), not `withContext`, so fields must be `.Data.X` / `$.Lang`
// / `$.CSRF` directly rather than `.Ctx.X`.
func TestPasswordPartialRenders(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))

	funcs := template.FuncMap{
		"t":        helper.T,
		"langPath": func(lang, p string) string { return p },
	}
	tpl, err := template.New("password.html").Funcs(funcs).
		ParseFiles("../../templates/partials/profile/password.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}

	for _, tt := range []struct {
		name           string
		data           map[string]interface{}
		wantContains   []string
		wantNotContain []string
		wantCSRF       bool
	}{
		{
			name: "no password set yet",
			data: map[string]interface{}{
				"PasswordSet":        false,
				"PasswordManagedEnv": false,
				"ErrKey":             "",
			},
			wantContains:   []string{`name="new"`, "Set a password"},
			wantNotContain: []string{`name="current"`},
			wantCSRF:       true,
		},
		{
			name: "password already set",
			data: map[string]interface{}{
				"PasswordSet":        true,
				"PasswordManagedEnv": false,
				"ErrKey":             "",
			},
			wantContains: []string{`name="current"`, `name="new"`, "Enter your current password"},
			wantCSRF:     true,
		},
		{
			name: "wrong current password redirected back",
			data: map[string]interface{}{
				"PasswordSet":        true,
				"PasswordManagedEnv": false,
				"ErrKey":             "auth.password.wrongCurrent",
			},
			wantContains: []string{"not the current password", `name="current"`},
			wantCSRF:     true,
		},
		{
			name: "managed by ADMIN_PASSWORD",
			data: map[string]interface{}{
				"PasswordSet":        true,
				"PasswordManagedEnv": true,
				"ErrKey":             "",
			},
			wantContains:   []string{"Managed by the ADMIN_PASSWORD"},
			wantNotContain: []string{"<form", `name="new"`},
			wantCSRF:       false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := map[string]interface{}{
				"Lang": "en",
				"CSRF": "test-csrf-token",
				"Data": tt.data,
			}
			var buf bytes.Buffer
			if err := tpl.ExecuteTemplate(&buf, "profile/password", ctx); err != nil {
				t.Fatalf("failed to render partial: %v", err)
			}
			out := buf.String()
			if strings.Contains(out, "<no value>") {
				t.Errorf("a template parameter did not arrive:\n%s", out)
			}
			if strings.Contains(out, "profile.password.") {
				t.Errorf("an untranslated message key reached the page:\n%s", out)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(out, want) {
					t.Errorf("output is missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(out, notWant) {
					t.Errorf("output unexpectedly contains %q:\n%s", notWant, out)
				}
			}
			if tt.wantCSRF && !strings.Contains(out, "test-csrf-token") {
				t.Errorf("CSRF token did not reach the form:\n%s", out)
			}
		})
	}
}
