package notifications

import (
	"bytes"
	"context"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/notification"
)

// captureStore keeps whatever the notification service stored, which is what
// the feed later reads back. Driving the real service rather than writing a
// body by hand is the point: this test asserts a property of the pipeline
// (renderer -> stored body -> feed page), and a hand-written body would only
// assert a property of the string in this file.
type captureStore struct {
	created *models.Notification
}

func (s *captureStore) GetLastMailedByKeyAndUser(context.Context, string, uuid.UUID) (*models.Notification, error) {
	return nil, nil
}
func (s *captureStore) GetLastByKeyAndUser(context.Context, string, uuid.UUID) (*models.Notification, error) {
	return nil, nil
}
func (s *captureStore) Create(_ context.Context, n *models.Notification) error {
	s.created = n
	return nil
}
func (s *captureStore) MarkMailed(context.Context, uuid.UUID) error { return nil }
func (s *captureStore) CountUnread(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (s *captureStore) ListByUser(context.Context, uuid.UUID, int) ([]models.Notification, error) {
	return nil, nil
}
func (s *captureStore) MarkAllRead(context.Context, uuid.UUID) error  { return nil }
func (s *captureStore) PruneKeepingNewest(context.Context, int) error { return nil }

// storedBodyFor sends a "vaulted" notification for a resource with the given
// name and returns the body that landed in the journal -- byte for byte what
// the feed will be handed.
func storedBodyFor(t *testing.T, resourceName string) string {
	t.Helper()
	store := &captureStore{}
	// No mailer: this test is about the feed, and mail is a separate
	// destination for the same body.
	svc := notification.NewWith(store, nil, nil, "https://webtor.io", "../../templates/notification")
	err := svc.SendVaulted("user@example.com", uuid.NewV4(), &vaultModels.Resource{
		ResourceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:       resourceName,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if store.created == nil {
		t.Fatal("nothing was stored, so the feed would have nothing to show")
	}
	return store.created.Body
}

// renderFeed executes the real feed view over the real Item conversion.
func renderFeed(t *testing.T, items []Item) string {
	t.Helper()
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
		"timeAgoLang": func(lang string, at time.Time) string { return "just now" },
	}
	tpl, err := template.New("get.html").Funcs(funcs).
		ParseFiles("../../templates/views/notifications/get.html")
	if err != nil {
		t.Fatalf("parse view: %v", err)
	}

	type ctx struct {
		Lang string
		CSRF string
		Data *Data
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", &ctx{Lang: "en", CSRF: "csrf", Data: &Data{Notifications: items}}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return buf.String()
}

// TestFeedShowsTheMessageNotTheDocument is the defect this change exists to
// fix. The feed used to print the stored body as text, and every stored body
// was a whole HTML document, so the row read
// "<!DOCTYPE html> <html> <body> <p>Your resource ...".
//
// Negative control: put the document wrapper back into
// templates/notification/vaulted.html (or print .Body as a plain string
// instead of markup) and this test must fail.
func TestFeedShowsTheMessageNotTheDocument(t *testing.T) {
	body := storedBodyFor(t, "Ubuntu 24.04 ISO")

	out := renderFeed(t, []Item{NewItem(models.Notification{
		Title:     "Your resource Ubuntu 24.04 ISO has been vaulted!",
		Body:      body,
		CreatedAt: time.Now(),
	})})

	if !strings.Contains(out, "has been vaulted") {
		t.Errorf("the feed does not show the message:\n%s", out)
	}
	if !strings.Contains(out, "Ubuntu 24.04 ISO") {
		t.Errorf("the feed does not name the resource:\n%s", out)
	}
	// The two halves of "shown as markup, not as text". The first is the
	// symptom the user reported; the second is the same thing said about the
	// tags the fragment does carry -- a body printed as text escapes its own
	// <p>, so &lt;p&gt; appearing here means the fix is not in place either.
	if strings.Contains(out, "DOCTYPE") {
		t.Errorf("the feed row carries a document wrapper:\n%s", out)
	}
	if strings.Contains(out, "&lt;p&gt;") {
		t.Errorf("the feed printed the message's own tags as text:\n%s", out)
	}
	// A <p> nested in a <p> is invalid and browsers close the outer one at
	// the first inner tag, which silently breaks the row's styling.
	if strings.Contains(out, `<p class="text-sm text-w-sub`) {
		t.Error("the body is wrapped in a <p>, but the fragment contains <p> elements")
	}
}

// TestFeedIsInertWithAHostileResourceName is the reason the feed is allowed
// to print the body as markup at all. The name below is chosen by whoever
// made the torrent; it reaches the template unmodified, and html/template is
// the only thing between it and the reader's browser.
//
// Negative control: switch services/notification.render to text/template and
// this must fail.
func TestFeedIsInertWithAHostileResourceName(t *testing.T) {
	for _, tt := range []struct {
		name       string
		resource   string
		wantEscape string
		wantAbsent []string
	}{
		{
			name:       "script tag",
			resource:   `<script>alert(1)</script>`,
			wantEscape: "&lt;script&gt;",
			wantAbsent: []string{"<script>alert(1)</script>", "<script>"},
		},
		{
			name:       "attribute breakout",
			resource:   `" onmouseover="alert(1)`,
			wantEscape: "&#34;",
			wantAbsent: []string{`" onmouseover="alert(1)`, `onmouseover="`},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := storedBodyFor(t, tt.resource)

			out := renderFeed(t, []Item{NewItem(models.Notification{
				Title:     "vaulted",
				Body:      body,
				CreatedAt: time.Now(),
			})})

			if !strings.Contains(out, tt.wantEscape) {
				t.Errorf("the page does not carry the escaped form %q:\n%s", tt.wantEscape, out)
			}
			for _, bad := range tt.wantAbsent {
				if strings.Contains(out, bad) {
					t.Errorf("the page carries live markup %q from a resource name:\n%s", bad, out)
				}
			}
		})
	}
}
