package notification

import (
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"
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
