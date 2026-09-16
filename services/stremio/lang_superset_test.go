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
		for _, a := range l.TitleAliases {
			if prev, ok := aliases[a]; ok {
				t.Errorf("%s reuses %s's title alias %q: langMap is keyed by alias too", l.Code, prev, a)
			}
			aliases[a] = l.Code
		}
	}
	// Deliberately NOT asserted: that every Code is also one of its own
	// TitleAliases. Two of the detectable rows are not -- "uk"
	// (Ukrainian, aliased "ua") and "cs" (Czech, aliased "cz") -- and
	// that is right rather than an oversight: title aliases are matched
	// against torrent-title tokens, where "UK" means the United Kingdom
	// far more often than Ukrainian. Twelve more rows have no title
	// aliases at all, which is the same judgement made wholesale. Written
	// down so the next reader does not "fix" it.
}

// TestAppendedLanguagesAreNotDetectedFromTitles is the review's C1 table,
// measured against the first version of this branch: every one of these
// titles answered with one of the twelve appended languages, and the last
// row is the damaging one — a false positive pre-empts the Cyrillic
// fallback (ExtractLanguages runs it only when nothing else matched), and
// LangFilterStream keeps only matching streams, so a Russian viewer lost
// that release from their Stremio list without a word.
//
// "KAT" is KickassTorrents branding and "[" / "]" are splitter characters;
// "EST" is the Electronic-Sell-Through release tag. Neither is a language,
// and neither was measured before being made one.
func TestAppendedLanguagesAreNotDetectedFromTitles(t *testing.T) {
	for _, c := range []struct {
		title string
		want  []string // language codes, in order
	}{
		{"[KAT] Movie 2019 1080p BluRay x264", nil},
		{"Movie.2019.1080p.WEB-DL.KAT", nil},
		{"Movie.2019.EST.WEB-DL.x264", nil},
		{"Movie.2019.LAV.1080p", nil},
		{"Movie 2019 TAM HDRip", nil},
		{"Movie.2019.SK.1080p.WEB", nil},
		{"Movie 2019 AZ 1080p", nil},
		{"Movie.2019.FA.1080p", nil},
		{"Фильм 2019 [KAT] 1080p", []string{"ru"}},
		// The words themselves are not detected either: with no title
		// aliases at all there is no token that means these languages.
		{"Movie 2019 Estonian 1080p", nil},
		{"Movie 2019 Catalan 1080p", nil},
		// Control: the rows that always had aliases still work, and the
		// Cyrillic fallback is intact.
		{"Movie.2019.RUS.1080p", []string{"ru"}},
		{"Фильм 2019 1080p", []string{"ru"}},
		{"Фільм 2019 1080p", []string{"uk"}},
		{"Movie 2019 ITA ENG 1080p", []string{"it", "en"}},
	} {
		var got []string
		for _, l := range ExtractLanguages(c.title) {
			got = append(got, l.Code)
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v want %v", c.title, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: got %v want %v", c.title, got, c.want)
				break
			}
		}
	}
}

// TestDetectableSplitsIdentityFromDetection: the twelve appended rows are
// nameable and choosable (LanguageByCode, NewLangDisplay, the Stremio
// dropdown) and invisible to detection — including by their flag, which is
// a langMap key for every row that has aliases.
func TestDetectableSplitsIdentityFromDetection(t *testing.T) {
	for _, code := range []string{"sk", "lt", "lv", "et", "fa", "bn", "ta", "kk", "ka", "hy", "az", "ca"} {
		l := LanguageByCode(code)
		if l == nil {
			t.Fatalf("%s: must stay in the table — it is what the dropdown offers", code)
		}
		if l.Detectable() {
			t.Errorf("%s: appended rows carry no title aliases until someone measures them", code)
		}
		if got := ExtractLanguages("Movie 2019 " + l.Flag + " 1080p"); len(got) != 0 {
			t.Errorf("%s: its flag reached detection anyway: %+v", code, got)
		}
		if d := NewLangDisplay(code); d.Name != l.Name || d.Flag != l.Flag {
			t.Errorf("%s: still has to render as a name and a flag, got %+v", code, d)
		}
	}
	// And the rows that do carry aliases are unchanged.
	for _, code := range []string{"en", "ru", "pt", "ms"} {
		if l := LanguageByCode(code); l == nil || !l.Detectable() {
			t.Errorf("%s: must stay detectable", code)
		}
	}
}
