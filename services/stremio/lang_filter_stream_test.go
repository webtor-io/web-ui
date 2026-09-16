package stremio

import "testing"

func stream(hash, title string) StreamItem {
	return StreamItem{InfoHash: hash, Name: "Torrentio", Title: title}
}

func hashes(streams []StreamItem) []string {
	out := make([]string, 0, len(streams))
	for _, s := range streams {
		out = append(out, s.InfoHash)
	}
	return out
}

// TestFilterStreamsByLanguageKeepsEverythingWithoutAUsableLanguage: the
// filter is exclusive, so a preference it cannot apply must switch it off
// rather than empty the list. Two ways to have no usable language, one
// answer — a code the table does not know, and a code it knows but can
// never read out of a title (the twelve rows appended in 2026-09 carry no
// TitleAliases, so Catalan matches nothing, ever).
func TestFilterStreamsByLanguageKeepsEverythingWithoutAUsableLanguage(t *testing.T) {
	streams := []StreamItem{
		stream("aa", "The.Boys.S03E05.1080p"),
		stream("bb", "The.Boys.S03E05\n🇷🇺 Русский"),
	}
	for _, c := range []struct {
		name string
		want *Language
	}{
		{"no preference", nil},
		{"a code the table does not know", LanguageByCode("zz")}, // nil
		{"a language with no title aliases", LanguageByCode("ca")},
		{"another one", LanguageByCode("et")},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := filterStreamsByLanguage(streams, c.want)
			if len(got) != len(streams) {
				t.Fatalf("kept %v of %v: an inapplicable setting must not empty the list",
					hashes(got), hashes(streams))
			}
		})
	}
}

// TestFilterStreamsByLanguageFiltersOnADetectableLanguage is the negative
// control for the guard above: where the language *can* be read out of a
// title, the filter still filters.
func TestFilterStreamsByLanguageFiltersOnADetectableLanguage(t *testing.T) {
	streams := []StreamItem{
		stream("aa", "The.Boys.S03E05\n🇷🇺 Русский"),
		stream("bb", "The.Boys.S03E05\n🇬🇧 English"),
		{InfoHash: "cc", Name: "Webtor", Title: "Anything at all",
			BehaviorHints: &StreamBehaviorHints{BingeGroup: libraryBingeGroupPrefix + "x"}},
	}
	got := hashes(filterStreamsByLanguage(streams, LanguageByCode("ru")))
	// "cc" is a Library stream: already in the viewer's Vault, so it
	// bypasses the filter whatever its title says.
	if len(got) != 2 || got[0] != "aa" || got[1] != "cc" {
		t.Fatalf("kept %v, want [aa cc]", got)
	}
}
