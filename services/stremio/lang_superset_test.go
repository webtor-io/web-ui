package stremio

import (
	"sort"
	"strings"
	"testing"
)

// translateServiceLangs is the code list of `langNames` in the
// subtitle-translate service (`services/langs.go`), copied here on
// 2026-09-16. It is the set of languages that service will accept, and
// every one of them has to exist in Languages: the ladder only offers a
// translation when `LanguageByCode(lang) != nil`
// (`handlers/action.applyLadder`), so a code the service knows and this
// table does not is a viewer whose preferred language silently never gets
// the AI item — no chip, no lock, no explanation.
//
// To refresh: read `langNames` in
// `subtitle-translate/services/langs.go`, paste its keys here in any
// order, and add whatever this test then reports as missing to
// `Languages` (append, never reorder — the Stremio settings list and the
// picker's language row read that order) and to the mirror list in
// `assets/src/js/lib/discover/lang.js`.
const translateServiceLangs = `
en ru es de fr pt it pl tr nl cs uk zh ja ko ar hi id vi th sv no da fi el
he hu ro bg sr hr sk sl lt lv et fa ms bn ta kk ka hy az ca
`

func TestLanguagesCoverTheTranslateService(t *testing.T) {
	want := strings.Fields(translateServiceLangs)
	if len(want) != 45 {
		t.Fatalf("the service list has %d codes, expected 45 — refresh both sides deliberately", len(want))
	}
	var missing []string
	for _, code := range want {
		if LanguageByCode(code) == nil {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("subtitle-translate accepts %v, and Languages has no entry for them: "+
			"the ladder would never offer a translation to a viewer whose preferred "+
			"language is one of these (see the refresh note above this test)", missing)
	}
}

// TestLanguagesAreWellFormed guards the two properties the table's own
// lookups depend on, both of which fail silently: langMap is keyed by every
// alias and by the flag, so a repeat in either shadows the earlier entry and
// the language above loses its lookup. (🇮🇳 for Tamil and 🇪🇸 for Catalan are
// exactly the collisions this caught.)
func TestLanguagesAreWellFormed(t *testing.T) {
	codes := map[string]bool{}
	flags := map[string]string{}
	aliases := map[string]string{}
	for _, l := range Languages {
		if l.Code == "" || l.Name == "" {
			t.Errorf("%+v: a code and a name are the minimum", l)
		}
		if codes[l.Code] {
			t.Errorf("%s: duplicate code", l.Code)
		}
		codes[l.Code] = true
		if prev, ok := flags[l.Flag]; ok {
			t.Errorf("%s reuses %s's flag %s: langMap is keyed by flag, so one shadows the other", l.Code, prev, l.Flag)
		}
		flags[l.Flag] = l.Code
		for _, a := range l.Aliases {
			if prev, ok := aliases[a]; ok {
				t.Errorf("%s reuses %s's alias %q: langMap is keyed by alias too", l.Code, prev, a)
			}
			aliases[a] = l.Code
		}
	}
	// Deliberately NOT asserted: that every Code is also one of its own
	// Aliases. Two are not -- "uk" (Ukrainian, aliased "ua") and "cs"
	// (Czech, aliased "cz") -- and that is right rather than an oversight:
	// aliases are matched against torrent-title tokens, where "UK" means
	// the United Kingdom far more often than Ukrainian. LanguageByCode
	// walks Codes and is unaffected; only title detection is, and there
	// the omission is the safer answer. Asserted here once so the next
	// reader does not "fix" it.
}

// TestSkippedCodesStayOutOfTitleDetection: "et" and "ca" are listed as
// aliases (the settings list and LanguageByCode need them) but must never
// be read out of a torrent title — "et" is the French and Latin
// conjunction, "ca" a region code and an abbreviation for circa. Missing a
// tag costs one filter chip; inventing one puts a release in a language
// nobody asked for.
func TestSkippedCodesStayOutOfTitleDetection(t *testing.T) {
	for _, title := range []string{
		"Le Fabuleux Destin et la Suite 2001 1080p",
		"Some.Movie.2019.CA.WEB-DL.x264",
	} {
		for _, l := range ExtractLanguages(title) {
			if l.Code == "et" || l.Code == "ca" {
				t.Errorf("%q: detected %s from a skipped token", title, l.Name)
			}
		}
	}
	// Negative control: the languages are still reachable by their full
	// names, so skipping the two-letter form does not remove them.
	for word, code := range map[string]string{"Estonian": "et", "Catalan": "ca", "Tamil": "ta"} {
		got := ExtractLanguages("Movie 2019 " + word + " 1080p")
		if len(got) != 1 || got[0].Code != code {
			t.Errorf("%s: got %+v, want the %s entry", word, got, code)
		}
	}
}
