package notification

import (
	"os"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/i18n"
)

// TestSubscriptionTemplatesRender executes every subscription email against
// the data its sender actually passes.
//
// Nothing else catches a broken one: templates are parsed at send time, in a
// cron job, and a missing field or a helper that isn't in the FuncMap
// surfaces as a mail that never goes out — hours later, in a log.
func TestSubscriptionTemplatesRender(t *testing.T) {
	// No i18n bundle on purpose: the helpers must fall back to returning
	// the message key rather than failing, so a locale gap costs a clumsy
	// email instead of no email.
	s := &Service{templateDir: "../../templates/notification", domain: "https://webtor.io"}

	sub := SubscriptionView{
		ID:             uuid.NewV4(),
		Title:          "The Boys",
		Season:         3,
		Lang:           "ru",
		UnsubscribeURL: "https://webtor.io/subscription/unsubscribe/token",
	}
	movie := SubscriptionView{ID: uuid.NewV4(), Title: "The Shawshank Redemption"}

	for _, tt := range []struct {
		name     string
		template string
		data     any
		want     string
	}{
		{
			name:     "confirmation for a season",
			template: "subscription-on.html",
			data:     s.subscriptionData(sub),
			want:     "email.subscription.on.text",
		},
		{
			name:     "confirmation for a movie",
			template: "subscription-on.html",
			data:     s.subscriptionData(movie),
			want:     "email.subscription.on.text",
		},
		{
			name:     "removal notice",
			template: "subscription-off.html",
			data:     s.subscriptionData(sub),
			want:     "email.subscription.off.text",
		},
		{
			name:     "completion notice",
			template: "subscription-off.html",
			data: func() subscriptionMailData {
				d := s.subscriptionData(sub)
				d.Completed = true
				return d
			}(),
			want: "email.subscription.off.completed",
		},
		{
			name:     "new releases",
			template: "subscription-update.html",
			data: func() subscriptionMailData {
				d := s.subscriptionData(sub)
				d.Releases = []ReleaseView{
					{Name: "The.Boys.S03E05.1080p", InfoHash: "aa", URL: "https://webtor.io/magnet:?xt=urn:btih:aa", Source: "RuTracker.org"},
					// A release whose source is unknown: the row still has
					// to render, without an empty "via" line.
					{Name: "The.Boys.S03E05.2160p", InfoHash: "bb", URL: "https://webtor.io/magnet:?xt=urn:btih:bb"},
				}
				return d
			}(),
			want: "The.Boys.S03E05.1080p",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, err := s.render(tt.template, "ru", tt.data)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(body, tt.want) {
				t.Errorf("rendered body is missing %q:\n%s", tt.want, body)
			}
			if strings.Contains(body, "<no value>") {
				t.Errorf("rendered body has an unresolved field:\n%s", body)
			}
		})
	}
}

// TestSubscriptionUpdateOmitsEmptySource: an addon that gave no label must
// not produce a dangling "via" line.
func TestSubscriptionUpdateOmitsEmptySource(t *testing.T) {
	s := &Service{templateDir: "../../templates/notification", domain: "https://webtor.io"}
	d := s.subscriptionData(SubscriptionView{ID: uuid.NewV4(), Title: "Dune"})
	d.Releases = []ReleaseView{{Name: "Dune.2021.1080p", InfoHash: "aa", URL: "https://webtor.io/x"}}

	body, err := s.render("subscription-update.html", "en", d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(body, "email.subscription.source") {
		t.Error("a release with no source rendered the source line anyway")
	}
}

// TestVaultTemplatesRenderLocalized drives every vault letter through the
// real templates and the real Russian locale -- the pair of things
// TestSubscriptionTemplatesRender deliberately does not cover (it pins the
// no-bundle fallback instead). One Russian word per template is enough: it
// proves the body went through the bundle, without turning the test into a
// second copy of ru.json.
func TestVaultTemplatesRenderLocalized(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	s := &Service{
		templateDir: "../../templates/notification",
		domain:      "https://webtor.io",
		i18n:        i18n.New(locales.FS()),
	}
	res := &vaultModels.Resource{ResourceID: "abc123", Name: "My Torrent"}

	for _, tt := range []struct {
		template string
		data     any
		want     []string
	}{
		{"vaulted.html", s.resourceData(res), []string{"сохранён в Vault", "My Torrent", "https://webtor.io/abc123"}},
		{"expired.html", s.resourceData(res), []string{"истёк", "My Torrent"}},
		{"transfer-timeout.html", func() map[string]any {
			d := s.resourceData(res)
			d["Timeout"] = "2 days"
			return d
		}(), []string{"сидеров", "My Torrent", "2 days"}},
		{"expiring.html", map[string]any{
			"Days":      3,
			"Resources": []expiringResource{{Name: "My Torrent", URL: "https://webtor.io/abc123"}},
			"Domain":    "https://webtor.io",
		}, []string{"исчезнут", "My Torrent"}},
	} {
		t.Run(tt.template, func(t *testing.T) {
			body, err := s.render(tt.template, "ru", tt.data)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("body lacks %q:\n%s", w, body)
				}
			}
			if strings.Contains(body, "email.") {
				t.Errorf("body carries a raw message key -- a key is missing from the bundle:\n%s", body)
			}
		})
	}
}
