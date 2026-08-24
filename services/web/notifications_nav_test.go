package web

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

// The notification bell ships in two places -- the desktop bar and the
// mobile burger -- because a previous nav change in this project shipped in
// only one and had to be fixed after review (see task-6-brief.md). Both
// extractors below are pinned to their own unique markers so a change that
// only touches one site cannot silently pass this test by testing the other
// twice.

// desktopBellSnippet isolates the "hidden lg:inline-flex" bell button. Its
// opening tag is unique in nav.html (aria-label uses the translated
// "nav.notifications" key, unlike the mobile row which prints it as text).
func desktopBellSnippet(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../templates/partials/nav.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	marker := `aria-label="{{ t $.Lang "nav.notifications" }}"`
	at := strings.Index(s, marker)
	if at < 0 {
		t.Fatal("nav.html has no desktop notification bell")
	}
	start := strings.LastIndex(s[:at], "<a ")
	if start < 0 {
		t.Fatal("could not find the start of the desktop bell's <a> tag")
	}
	end := strings.Index(s[start:], "</a>")
	if end < 0 {
		t.Fatal("desktop bell <a> tag is unterminated")
	}
	return s[start : start+end+len("</a>")]
}

// mobileBellSnippet isolates the burger-menu row. Its href is shared with
// the desktop button, but only the mobile row wraps it in an <li>, which is
// what makes the marker unique.
func mobileBellSnippet(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../templates/partials/nav.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	marker := `<li><a href="{{ langPath $.Lang "/notifications" }}"`
	start := strings.Index(s, marker)
	if start < 0 {
		t.Fatal("nav.html has no mobile notification bell row")
	}
	start += len("<li>")
	end := strings.Index(s[start:], "</a>")
	if end < 0 {
		t.Fatal("mobile bell <a> tag is unterminated")
	}
	return s[start : start+end+len("</a>")]
}

type navBellCtx struct {
	Lang                string
	UnreadNotifications int
}

func renderBell(t *testing.T, snippet string, unread int) string {
	t.Helper()
	h := i18n.NewHelper(i18n.New(os.DirFS("../../locales")))
	tmpl := template.Must(template.New("bell").Funcs(template.FuncMap{
		"t":        h.T,
		"langPath": func(lang, path string) string { return "[" + lang + "]" + path },
	}).Parse(snippet))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &navBellCtx{Lang: "en", UnreadNotifications: unread}); err != nil {
		t.Fatalf("execute (unread=%d): %v", unread, err)
	}
	return buf.String()
}

// TestNavBellBadgeReflectsUnreadCount is the guard this project has already
// gotten wrong once, in this exact template (see task-6-brief.md's warning).
// It must observe a non-zero badge count with unread notifications and a
// zero count with none, for BOTH nav sites -- a guard that hides the badge
// unconditionally would read as "zero, zero" here and must fail the test.
func TestNavBellBadgeReflectsUnreadCount(t *testing.T) {
	for _, site := range []struct {
		name    string
		snippet func(*testing.T) string
	}{
		{"desktop", desktopBellSnippet},
		{"mobile", mobileBellSnippet},
	} {
		t.Run(site.name, func(t *testing.T) {
			snippet := site.snippet(t)

			withUnread := renderBell(t, snippet, 3)
			gotWith := strings.Count(withUnread, "notification-badge")
			if gotWith == 0 {
				t.Fatalf("%s: expected a non-zero badge count with 3 unread notifications, got 0 in:\n%s", site.name, withUnread)
			}
			if !strings.Contains(withUnread, ">3<") {
				t.Errorf("%s: badge did not carry the unread count 3:\n%s", site.name, withUnread)
			}

			withoutUnread := renderBell(t, snippet, 0)
			gotWithout := strings.Count(withoutUnread, "notification-badge")
			if gotWithout != 0 {
				t.Errorf("%s: expected a zero badge count with no unread notifications, got %d in:\n%s", site.name, gotWithout, withoutUnread)
			}

			// The bell icon itself (not just the badge) must survive in both
			// states -- the feature is the bell, the badge is a detail on it.
			if !strings.Contains(withoutUnread, "<svg") {
				t.Errorf("%s: bell icon missing when there are no unread notifications", site.name)
			}
		})
	}
}
