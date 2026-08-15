package template

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/i18n"
)

// TestSubscriptionsPartialRenders parses and executes the profile
// subscriptions section standalone, for the reason spelled out in
// TestTorznabIndexersPartialRenders: a bad partial takes the process down at
// startup, and a nil deref inside a range only surfaces at execute time.
//
// The rows below are the four shapes the table actually produces: a season
// waiting for its first poll, an active season, a movie with no poster and
// no title (metadata lookup failed on subscribe), and a finished one.
func TestSubscriptionsPartialRenders(t *testing.T) {
	// Real translation helpers for the same reason as the banner test: a
	// key-echoing stub cannot tell `t` from `tp`, and the difference is
	// what puts "<no value>" on the page.
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))

	funcs := template.FuncMap{
		"t":           helper.T,
		"tp":          helper.Tp,
		"langPath":    func(lang, p string) string { return p },
		"asset":       func(p string) template.HTML { return template.HTML("<script src=\"" + p + "\"></script>") },
		"isPaid":      func(_ interface{}) bool { return false },
		"withContext": func(ctx, data interface{}) interface{} { return data },
		"timeAgoLang": func(lang string, tm time.Time) string { return "1 hour ago" },
	}
	tpl, err := template.New("subscriptions.html").Funcs(funcs).
		ParseFiles("../../templates/partials/profile/subscriptions.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}

	season := int16(3)
	title := "The Boys"
	poster := "https://example.com/poster.jpg"
	checked := time.Now().Add(-time.Hour)

	for _, tt := range []struct {
		name string
		data []models.ReleaseSubscription
	}{
		{name: "empty list"},
		{
			name: "season awaiting first poll",
			data: []models.ReleaseSubscription{{
				ID:      uuid.NewV4(),
				Kind:    models.ReleaseSubscriptionKindSeason,
				VideoID: "tt1190634",
				Season:  &season,
				Title:   &title,
				State:   models.ReleaseSubscriptionStatePendingBaseline,
				Enabled: true,
			}},
		},
		{
			name: "active season with poster and a poll behind it",
			data: []models.ReleaseSubscription{{
				ID:            uuid.NewV4(),
				Kind:          models.ReleaseSubscriptionKindSeason,
				VideoID:       "tt1190634",
				Season:        &season,
				Title:         &title,
				PosterURL:     &poster,
				State:         models.ReleaseSubscriptionStateActive,
				Enabled:       true,
				LastCheckedAt: &checked,
			}},
		},
		{
			// Nothing but the id: the metadata lookup failed when this row
			// was written, so title and poster are both nil.
			name: "movie without metadata",
			data: []models.ReleaseSubscription{{
				ID:      uuid.NewV4(),
				Kind:    models.ReleaseSubscriptionKindMovie,
				VideoID: "tt0111161",
				State:   models.ReleaseSubscriptionStateActive,
				Enabled: false,
			}},
		},
		{
			name: "completed season",
			data: []models.ReleaseSubscription{{
				ID:      uuid.NewV4(),
				Kind:    models.ReleaseSubscriptionKindSeason,
				VideoID: "tt1190634",
				Season:  &season,
				Title:   &title,
				State:   models.ReleaseSubscriptionStateCompleted,
				Enabled: true,
			}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := map[string]interface{}{
				"Lang":   "en",
				"Claims": nil,
				"Data": map[string]interface{}{
					"Subscriptions":     tt.data,
					"SubscriptionLimit": 3,
				},
			}
			var buf bytes.Buffer
			if err := tpl.ExecuteTemplate(&buf, "profile/subscriptions", ctx); err != nil {
				t.Fatalf("failed to render partial: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, "Release subscriptions") {
				t.Errorf("rendered output is missing the section heading:\n%s", out)
			}
			if strings.Contains(out, "<no value>") {
				t.Errorf("a template parameter did not arrive:\n%s", out)
			}
			if strings.Contains(out, "profile.subscriptions.") {
				t.Errorf("an untranslated message key reached the page:\n%s", out)
			}
		})
	}
}
