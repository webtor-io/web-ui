package i18n

import (
	"testing"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// TestAtKeysNotExposedAsMessages verifies that "@"-prefixed metadata keys in
// a locale JSON file are stripped before go-i18n registers them as messages.
// Regression guard: without the custom unmarshal func, translator-context
// entries like "@support.work" would be returned verbatim as translations.
func TestAtKeysNotExposedAsMessages(t *testing.T) {
	data := []byte(`{
		"support.work": "Work",
		"@support.work": "Title of the copyrighted creative work (movie/book/music).",
		"nav.login": "Login"
	}`)

	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", unmarshalStrippingAtKeys)
	if _, err := bundle.ParseMessageFileBytes(data, "en.json"); err != nil {
		t.Fatalf("ParseMessageFileBytes: %v", err)
	}

	loc := i18n.NewLocalizer(bundle, "en")

	if got, _ := loc.Localize(&i18n.LocalizeConfig{MessageID: "support.work"}); got != "Work" {
		t.Errorf("support.work: got %q, want %q", got, "Work")
	}

	// An "@" key must NOT round-trip through the bundle — it should be
	// absent, so Localize returns the ID itself (go-i18n's fallback) plus
	// a "message not found" error.
	got, err := loc.Localize(&i18n.LocalizeConfig{MessageID: "@support.work"})
	if err == nil {
		t.Errorf("@support.work: expected lookup error, got message %q", got)
	}
}
