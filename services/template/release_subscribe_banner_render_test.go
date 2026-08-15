package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

// TestReleaseSubscribeBannerRenders executes the resource-page banner in all
// three of its states. Same reason as the other partial render tests: the
// template manager parses every partial at startup, so a broken one takes
// the process down instead of failing one page, and a nil field inside a
// branch only shows up at execute time.
func TestReleaseSubscribeBannerRenders(t *testing.T) {
	// The real translation helpers, not stubs. A stub that echoes the key
	// hides the whole class of bug this catches: a message with a
	// {{.Season}} parameter rendered through `t` instead of `tp` reaches the
	// page as "<no value>", and every key-echoing fake reports it as fine.
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
		"asset":    func(p string) template.HTML { return template.HTML("") },
	}
	tpl, err := template.New("release_subscribe_banner.html").Funcs(funcs).
		ParseFiles("../../templates/partials/resource/release_subscribe_banner.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}

	type banner struct {
		SeriesTitle    string
		SeriesVideoID  string
		Season         int
		Subscribed     bool
		SubscriptionID string
		Anonymous      bool
	}

	for _, tt := range []struct {
		name    string
		banner  any
		want    []string
		notWant []string
	}{
		{
			// Not an airing season, or not a series at all: the slot renders
			// nothing rather than an empty box.
			name:    "no banner",
			banner:  nil,
			notWant: []string{"release-subscribe-banner"},
		},
		{
			name:   "offer",
			banner: &banner{SeriesTitle: "The Boys", SeriesVideoID: "tt1190634", Season: 3},
			// "Season 3 is still airing" — the number has to survive the
			// translation, which is what tp is for.
			want:    []string{"/subscription/add", `name="season" value="3"`, "resource_banner", "Season 3 is still airing", "Notify me"},
			notWant: []string{"/subscription/delete/", "/login"},
		},
		{
			name:    "already following",
			banner:  &banner{SeriesTitle: "The Boys", SeriesVideoID: "tt1190634", Season: 3, Subscribed: true, SubscriptionID: "8c1f0d24-0000-0000-0000-000000000000"},
			want:    []string{"/subscription/delete/8c1f0d24-0000-0000-0000-000000000000", "You are following season 3", "Stop notifying"},
			notWant: []string{"/subscription/add"},
		},
		{
			// No account, no form: a POST would 401, so the button becomes a
			// login link that comes back to this page.
			name:   "anonymous",
			banner: &banner{SeriesTitle: "The Boys", SeriesVideoID: "tt1190634", Season: 3, Anonymous: true},
			// The path is escaped by html/template in a query context — the
			// login page decodes it back.
			want:    []string{"/login?return-url=%2fabc123", "Notify me"},
			notWant: []string{"<form", "/subscription/add"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := map[string]interface{}{
				"Lang": "en",
				"CSRF": "csrf-token",
				"Data": map[string]interface{}{
					"ReleaseSubBanner": tt.banner,
					"Resource":         map[string]interface{}{"ID": "abc123"},
				},
			}
			var buf bytes.Buffer
			if err := tpl.ExecuteTemplate(&buf, "resource/release_subscribe_banner", ctx); err != nil {
				t.Fatalf("failed to render partial: %v", err)
			}
			out := buf.String()
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("output is missing %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(out, notWant) {
					t.Errorf("output unexpectedly contains %q:\n%s", notWant, out)
				}
			}
			// A parameter that never arrived, or a key that has no
			// translation — both reach the reader as noise.
			if strings.Contains(out, "<no value>") {
				t.Errorf("a template parameter did not arrive:\n%s", out)
			}
			if strings.Contains(out, "release_sub.") {
				t.Errorf("an untranslated message key reached the page:\n%s", out)
			}
		})
	}
}
