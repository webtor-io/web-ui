package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

// TestUnsubscribeConfirmRenders executes the confirm page standalone, for
// the reason the other render tests exist: a bad view takes the process
// down at startup, and a t-vs-tp mix-up only surfaces as "<no value>" at
// execute time. Both data shapes run — a subscription with a title and one
// whose metadata lookup failed on subscribe.
func TestUnsubscribeConfirmRenders(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))

	funcs := template.FuncMap{
		"t":        helper.T,
		"tp":       helper.Tp,
		"langPath": func(lang, p string) string { return p },
	}
	tpl, err := template.New("unsubscribe_confirm.html").Funcs(funcs).
		ParseFiles("../../templates/views/subscription/unsubscribe_confirm.html")
	if err != nil {
		t.Fatalf("failed to parse view: %v", err)
	}

	type confirmData struct {
		Title string
		Token string
	}
	type ctx struct {
		Lang string
		CSRF string
		Data *confirmData
	}

	for _, tt := range []struct {
		name string
		data *confirmData
		want string
	}{
		{"with a title", &confirmData{Title: "The Boys", Token: "tok-123"}, "The Boys"},
		{"without a title", &confirmData{Token: "tok-123"}, "subscription"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tpl.ExecuteTemplate(&buf, "main", &ctx{Lang: "en", CSRF: "csrf-token", Data: tt.data}); err != nil {
				t.Fatalf("execute: %v", err)
			}
			out := buf.String()
			if strings.Contains(out, "<no value>") {
				t.Error("the page rendered a missing parameter")
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("page does not mention %q:\n%s", tt.want, out)
			}
			if !strings.Contains(out, `method="post"`) || !strings.Contains(out, "/subscription/unsubscribe/tok-123") {
				t.Error("the confirm form must POST back to the token URL — the GET deletes nothing")
			}
			if !strings.Contains(out, `name="_csrf"`) {
				t.Error("the form carries no CSRF token")
			}
		})
	}
}
