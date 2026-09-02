package i18n

import (
	"sort"
	"testing"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// helperWith builds a Helper over an in-memory bundle so the fallback
// behaviour can be tested without depending on which keys happen to be
// missing from locales/ on any given day.
func helperWith(t *testing.T, files map[string]string) *Helper {
	t.Helper()
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", unmarshalStrippingAtKeys)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := bundle.ParseMessageFileBytes([]byte(files[name]), name); err != nil {
			t.Fatalf("ParseMessageFileBytes(%s): %v", name, err)
		}
	}
	return NewHelper(&Service{Bundle: bundle})
}

// TestMissingKeyFallsBackToEnglishNotTheKey pins the property that a locale
// which is behind on translations shows English, never a raw identifier.
//
// go-i18n returns *MessageNotFoundErr even when it successfully rendered the
// default-language template (i18n/localizer.go, getMessageTemplate: the
// "Fallback to default language in bundle" branch returns a non-nil template
// AND the error). Helper.T used to read err != nil as "nothing to show" and
// return the key, so a Russian visitor to /notifications saw the literal
// strings notifications.title and nav.notifications.
func TestMissingKeyFallsBackToEnglishNotTheKey(t *testing.T) {
	h := helperWith(t, map[string]string{
		"en.json": `{
			"notifications.title": "Notifications",
			"nav.notifications": "Notifications",
			"profile.email.currentHint": "Notifications are currently sent to {{.Email}}."
		}`,
		"ru.json": `{
			"nav.profile": "Профиль"
		}`,
	})

	if got := h.T("ru", "nav.profile"); got != "Профиль" {
		t.Errorf("translated key: got %q, want %q", got, "Профиль")
	}

	for _, key := range []string{"notifications.title", "nav.notifications"} {
		got := h.T("ru", key)
		if got == key {
			t.Errorf("T(ru, %q) returned the key itself — the English text was available and should have been used", key)
		}
		if got != "Notifications" {
			t.Errorf("T(ru, %q): got %q, want %q", key, got, "Notifications")
		}
	}

	// Same property for the parameterized form.
	got := h.Tp("ru", "profile.email.currentHint", "Email", "a@example.com")
	if want := "Notifications are currently sent to a@example.com."; got != want {
		t.Errorf("Tp(ru, profile.email.currentHint): got %q, want %q", got, want)
	}

	// A key that no locale defines still degrades to the key — there is
	// genuinely nothing else to show, and callers such as FaqSchema key off
	// that exact sentinel.
	if got := h.T("ru", "does.not.exist.anywhere"); got != "does.not.exist.anywhere" {
		t.Errorf("wholly unknown key: got %q, want the key back", got)
	}
	if got := h.T("en", "does.not.exist.anywhere"); got != "does.not.exist.anywhere" {
		t.Errorf("wholly unknown key in default lang: got %q, want the key back", got)
	}
}

// Russian splits counts three ways; the plural helper must pick the form the
// language's rules say, not the "other" form for everything.
func TestPluralFormsFollowLanguageRules(t *testing.T) {
	h := helperWith(t, map[string]string{
		"en.json": `{"seeders": {"one": "{{.Count}} seeder", "other": "{{.Count}} seeders"}}`,
		"ru.json": `{"seeders": {"one": "{{.Count}} сид", "few": "{{.Count}} сида", "many": "{{.Count}} сидов", "other": "{{.Count}} сида"}}`,
	})
	cases := []struct {
		lang string
		n    int
		want string
	}{
		{"ru", 1, "1 сид"}, {"ru", 2, "2 сида"}, {"ru", 5, "5 сидов"}, {"ru", 21, "21 сид"}, {"ru", 12, "12 сидов"},
		{"en", 1, "1 seeder"}, {"en", 2, "2 seeders"}, {"en", 0, "0 seeders"},
	}
	for _, c := range cases {
		if got := h.Tn(c.lang, "seeders", c.n); got != c.want {
			t.Errorf("%s %d: got %q, want %q", c.lang, c.n, got, c.want)
		}
	}
}
