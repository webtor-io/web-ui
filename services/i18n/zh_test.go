package i18n

import (
	"os"
	"testing"

	gi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

// Exercise real locale discovery and regional Chinese lookup, not a copy of
// the dictionary: both are required for a zh-CN browser to receive Chinese.
func TestSimplifiedChineseLocale(t *testing.T) {
	oldLangs, oldGlobal := SupportedLangs, globalService
	t.Cleanup(func() { SupportedLangs, globalService = oldLangs, oldGlobal })
	svc := New(os.DirFS("../../locales"))
	found := false
	for _, lang := range SupportedLangs {
		if lang == "zh" {
			found = true
		}
	}
	if !found || LangNames["zh"] != "简体中文" {
		t.Fatal("Simplified Chinese is absent from the language selector")
	}
	for _, lang := range []string{"zh", "zh-CN", "zh-Hans"} {
		loc := gi18n.NewLocalizer(svc.Bundle, lang)
		got, err := loc.Localize(&gi18n.LocalizeConfig{MessageID: "player.play"})
		if err != nil || got != "播放" {
			t.Fatalf("%s player.play = %q, %v", lang, got, err)
		}
		for _, n := range []int{0, 1, 3} {
			got, err = loc.Localize(&gi18n.LocalizeConfig{
				MessageID: "resource.status.seeders", PluralCount: n,
				TemplateData: map[string]any{"Count": n},
			})
			if err != nil || got == "" {
				t.Fatalf("%s seeders(%d) = %q, %v", lang, n, got, err)
			}
		}
	}
}
