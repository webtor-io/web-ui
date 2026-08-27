package notification

import (
	"os"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/i18n"
)

// The in-app feed prints a stored notification body as markup rather than as
// text (handlers/notifications.Item, templates/views/notifications/get.html).
// That is only safe because of one property of this package: bodies come out
// of html/template, so the parts of them a user controls are escaped before
// they are ever stored.
//
// The names below are the two shapes that matter. The first escapes an HTML
// text node, the second escapes out of a quoted attribute value -- a body
// interpolates a resource name into both (<strong>{{ .Name }}</strong> and
// <a href="{{ .URL }}">), and an escaper that only handled text nodes would
// pass the first test and fail the second.
const (
	scriptName = `<script>alert(1)</script>`
	attrName   = `" onmouseover="alert(1)`
)

// A resource name reaches the template unmodified -- nothing between the
// torrent and the notification sanitises it -- which is what makes the
// escaping the only thing standing between an attacker-chosen name and the
// reader's browser.
func hostileResource(name string) *vaultModels.Resource {
	return &vaultModels.Resource{
		ResourceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:       name,
	}
}

// TestRenderedBodyEscapesHostileNames is the guard the feed's safety rests
// on. Negative control: make render use text/template (or drop the escaping
// any other way) and this test must fail -- if it still passes, it is not
// testing what it claims to.
func TestRenderedBodyEscapesHostileNames(t *testing.T) {
	for _, tt := range []struct {
		name string
		// resource name the attacker controls
		resource string
		// the escaped form that must appear in the stored body
		wantEscaped string
		// the live markup that must not
		wantAbsent []string
	}{
		{
			name:        "script tag in a text node",
			resource:    scriptName,
			wantEscaped: "&lt;script&gt;",
			wantAbsent:  []string{"<script>alert(1)</script>", "<script>"},
		},
		{
			name:        "quote breaking out of an attribute",
			resource:    attrName,
			wantEscaped: "&#34;",
			// Note what is NOT asserted: the word "onmouseover" survives, as
			// text. What must not survive is the quote that would end the
			// attribute before it, which is why the needle carries one.
			wantAbsent: []string{`" onmouseover="alert(1)`, `onmouseover="`},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmplDir := "../../templates/notification"
			locales, err := os.OpenRoot("../../locales")
			if err != nil {
				t.Fatalf("locales: %v", err)
			}
			defer locales.Close()
			store := &mockStore{}
			mail := &mockMailer{}
			// The real bundle, not nil: since the templates were localized,
			// a hostile name reaches the body through a translated sentence
			// (go-i18n's text/template interpolates it, html/template
			// escapes it on output). A nil bundle would drop the name
			// entirely -- trivially safe, and testing nothing.
			svc := NewWith(store, mail, i18n.New(locales.FS()), "https://webtor.io", tmplDir)

			if err := svc.SendVaulted("user@example.com", uuid.NewV4(), hostileResource(tt.resource)); err != nil {
				t.Fatalf("send: %v", err)
			}
			if store.created == nil {
				t.Fatal("nothing was stored, so there is nothing to check")
			}

			body := store.created.Body
			if !strings.Contains(body, tt.wantEscaped) {
				t.Errorf("stored body does not contain the escaped form %q:\n%s", tt.wantEscaped, body)
			}
			for _, bad := range tt.wantAbsent {
				if strings.Contains(body, bad) {
					t.Errorf("stored body contains live markup %q -- the feed renders this as HTML:\n%s", bad, body)
				}
			}

			// The letter is the same fragment inside the layout, so it must
			// be inert for the same reason. Checked separately because
			// wrapEmail is the one place that hands a string to a template
			// as template.HTML, and a mistake there would show up only here.
			if len(mail.calls) != 1 {
				t.Fatalf("letters sent: got %d, want 1", len(mail.calls))
			}
			for _, bad := range tt.wantAbsent {
				if strings.Contains(mail.calls[0].body, bad) {
					t.Errorf("the letter contains live markup %q:\n%s", bad, mail.calls[0].body)
				}
			}
		})
	}
}

// TestRenderedBodyEscapesHostileAttributes covers the context the test above
// cannot reach through a resource name: an href. subscription-update.html
// writes <a href="{{ .URL }}"> from a link an indexer supplied, so the value
// is third-party and lands inside a quoted attribute -- the one place where
// text-node escaping alone would not be enough.
func TestRenderedBodyEscapesHostileAttributes(t *testing.T) {
	svc := newTestService(nil, nil, "../../templates/notification")

	d := svc.subscriptionData(SubscriptionView{ID: uuid.NewV4(), Title: "Dune"})
	d.Releases = []ReleaseView{{
		Name:     "Dune.2021.1080p",
		InfoHash: "aa",
		URL:      `" onmouseover="alert(1)`,
	}}

	body, err := svc.render("subscription-update.html", "en", d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(body, `onmouseover="`) {
		t.Errorf("a release URL broke out of its attribute:\n%s", body)
	}
	if strings.Contains(body, `href="" `) {
		t.Errorf("the href was terminated early by attacker input:\n%s", body)
	}
}

// TestRenderedBodyIsAFragment pins the split itself, on every template that
// reaches the feed. A body that carries a document is the defect this test
// exists for: the feed prints the body, and a DOCTYPE in a list row is what
// the user saw.
func TestRenderedBodyIsAFragment(t *testing.T) {
	svc := newTestService(nil, nil, "../../templates/notification")

	res := &vaultModels.Resource{ResourceID: "bbbbbbbb", Name: "Some Torrent"}

	for _, tt := range []struct {
		template string
		data     any
	}{
		{"vaulted.html", svc.resourceData(res)},
		{"expired.html", svc.resourceData(res)},
		{"transfer-timeout.html", map[string]any{"Name": res.Name, "URL": "https://webtor.io/x", "Domain": "https://webtor.io", "Timeout": "2 days"}},
		{"expiring.html", map[string]any{"Days": 3, "Resources": []expiringResource{{Name: res.Name, URL: "https://webtor.io/x"}}, "Domain": "https://webtor.io"}},
		{"subscription-on.html", svc.subscriptionData(SubscriptionView{ID: uuid.NewV4(), Title: "Dune"})},
		{"subscription-off.html", svc.subscriptionData(SubscriptionView{ID: uuid.NewV4(), Title: "Dune"})},
		{"subscription-update.html", svc.subscriptionData(SubscriptionView{ID: uuid.NewV4(), Title: "Dune"})},
		// Not a feed template (it goes through mailOnly), but it is mailed,
		// so it has to be a fragment too or the layout would nest documents.
		{"verify-email.html", map[string]any{"Link": "https://webtor.io/profile/email/verify/tok"}},
	} {
		t.Run(tt.template, func(t *testing.T) {
			body, err := svc.render(tt.template, "en", tt.data)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			for _, tag := range []string{"DOCTYPE", "<html", "<body"} {
				if strings.Contains(body, tag) {
					t.Errorf("body carries %q -- these templates are fragments, the document belongs to layout.html:\n%s", tag, body)
				}
			}
			if strings.Contains(body, "<no value>") {
				t.Errorf("rendered body has an unresolved field:\n%s", body)
			}
		})
	}
}

// TestWrappedLetterIsADocument is the other half: the split must not have
// cost the email its document, or mail clients get a bare fragment.
func TestWrappedLetterIsADocument(t *testing.T) {
	svc := newTestService(nil, nil, "../../templates/notification")

	letter, err := svc.wrapEmail("<p>hello</p>", "en")
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	for _, want := range []string{"<!DOCTYPE html>", "<html>", "<body>", "<p>hello</p>", "</html>"} {
		if !strings.Contains(letter, want) {
			t.Errorf("letter is missing %q:\n%s", want, letter)
		}
	}
	// The fragment goes in as markup, not as text. If it were escaped the
	// letter would show the tags, which is the feed's old defect moved to
	// the wire.
	if strings.Contains(letter, "&lt;p&gt;") {
		t.Errorf("the layout escaped the fragment instead of embedding it:\n%s", letter)
	}
}
