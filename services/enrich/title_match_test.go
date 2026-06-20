package enrich

import (
	"testing"

	"github.com/webtor-io/web-ui/services/tmdb"
)

func TestIsWeakSearchTitle(t *testing.T) {
	cases := []struct {
		in   string
		want bool
		why  string
	}{
		// === Weak: leading-zero / empty part-numbers ===
		{"", true, "empty"},
		{"01", true, "filename part-number 01.mp4 -> 0187 UFO FP"},
		{"007", true, "leading-zero numeric"},
		{"0006", true, "leading-zero numeric"},
		{"00", true, "leading-zero numeric"},
		{"0", true, "bare zero"},
		{"  01 ", true, "trimmed leading-zero numeric"},

		// === Not weak: real titles, incl. legit numeric ===
		{"1917", false, "real numeric film, no leading zero"},
		{"300", false, "real numeric film"},
		{"9", false, "real numeric film (2009)"},
		{"21", false, "real numeric film"},
		{"2026", false, "bare year-shaped numeric, no leading zero"},
		{"Star Wars", false, "real title"},
		{"The Matrix", false, "real title"},
		{"01 Up", false, "has a non-digit token"},
	}
	for _, c := range cases {
		if got := isWeakSearchTitle(c.in); got != c.want {
			t.Errorf("isWeakSearchTitle(%q) = %v, want %v (%s)", c.in, got, c.want, c.why)
		}
	}
}

func TestAcceptableTMDBMatch(t *testing.T) {
	cases := []struct {
		query  string
		result string
		want   bool
		why    string
	}{
		// === Reject: low-signal query + fuzzy collision (the bug) ===
		{"01", "0187 UFO", false, "numeric query, no shared token"},
		{"0006", "Five Couple To Marry, Or Not?", false, "numeric query, no shared token"},
		{"00005", "Is There an Empty Room Here? huh?", false, "numeric query, no shared token"},
		{"0", "ZERO TIMES ZERO", false, "numeric query, no shared token"},
		{"00", "Music Videos That Defined the 00s", false, "numeric query, no shared token"},
		{"R", "Dhurandhar: The Revenge", false, "single short query, no shared token"},
		{"v34", "Creep", false, "single short query, no shared token"},

		// === Accept: confident (multi-token) query — localized / aka
		// matches MUST survive even with zero token overlap ===
		{"A Freira", "The Nun", true, "PT->EN localized title"},
		{"La guerre des boutons", "War of the Buttons", true, "FR->EN localized title"},
		{"Shou Trumana", "The Truman Show", true, "RU translit -> EN"},
		{"Vecinos Invasores", "Over the Hedge", true, "ES->EN localized title"},
		{"Sicario Day of the Soldado", "Sicario", true, "multi-token query trusted (confident)"},

		// === Accept: real title matches, regardless of popularity ===
		{"1917", "1917", true, "exact numeric"},
		{"300", "300", true, "exact numeric"},
		{"Up", "Up", true, "exact short title"},
		{"Matrix", "The Matrix", true, "query token subset of result"},
		{"The Matrix", "The Matrix", true, "exact with article"},
		{"Fight Club", "Fight Club", true, "exact two-word"},
		{"Sicario 2015", "Sicario", true, "year token dropped, full coverage"},
		{"Spiderman", "Spider-Man", true, "compound-word split (squash)"},
		{"Spiderman 2", "Spider-Man 2", true, "compound-word split with number"},
		{"Amelie", "Amélie", true, "diacritics folded"},
		{"Tron Legacy", "TRON: Legacy", true, "punctuation + case"},
		{"WALL E", "WALL·E", true, "interpunct normalized"},

		// === Non-Latin: gate skipped, accept as before ===
		{"Дюна", "Dune", true, "cyrillic query — gate skipped"},
		{"鬼滅の刃", "Demon Slayer", true, "japanese query — gate skipped"},
		{"Dune", "Дюна", true, "cyrillic result — gate skipped"},
	}
	for _, c := range cases {
		sr := &tmdb.SearchResult{Title: c.result}
		if got := acceptableTMDBMatch(c.query, sr); got != c.want {
			t.Errorf("acceptableTMDBMatch(%q, %q) = %v, want %v (%s)",
				c.query, c.result, got, c.want, c.why)
		}
	}
	if acceptableTMDBMatch("anything", nil) {
		t.Errorf("acceptableTMDBMatch with nil result = true, want false")
	}
}

func TestIsRejectableMatch(t *testing.T) {
	cases := []struct {
		query  string
		result string
		want   bool
		why    string
	}{
		// The production false positive, both detection paths.
		{"01", "0187 UFO", true, "leading-zero numeric + no overlap"},
		{"0006", "Five Couple To Marry, Or Not?", true, "leading-zero numeric"},
		{"v34", "Creep", true, "short single-token query, no overlap"},
		// Legit matches must be kept.
		{"1917", "1917", false, "exact numeric"},
		{"The Matrix", "The Matrix", false, "exact"},
		{"Spiderman", "Spider-Man", false, "confident single-word query"},
		{"A Freira", "The Nun", false, "confident multi-token query — localized match kept"},
		{"Дюна", "Dune", false, "non-latin — guard skipped"},
	}
	for _, c := range cases {
		if got := IsRejectableMatch(c.query, c.result); got != c.want {
			t.Errorf("IsRejectableMatch(%q, %q) = %v, want %v (%s)",
				c.query, c.result, got, c.want, c.why)
		}
	}
}
