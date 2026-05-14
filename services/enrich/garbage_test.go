package enrich

import "testing"

func TestIsGarbageTitle(t *testing.T) {
	cases := []struct {
		in   string
		want bool
		why  string
	}{
		// === Positive: should flag as garbage ===
		{"1427", true, "pure digits (H1)"},
		{"5030812089929695863", true, "pure digit run (H1/H4)"},
		{"dhbdhdhdhdjdhdhdh", true, "consonant-only single token (H3)"},
		{"tjqkzcgkrvos", true, "consonant-only single token (H3)"},
		{"384434dadea54ae9", true, "hex hash (H4)"},
		{"9mjau4u3wwg5rq9s", true, "16-char random alphanumeric (H6)"},
		{"055266908f91de7f7b9", true, "hex-like random (H6)"},
		{"stephb81", true, "username+id, single token, ≥25% digits (H7)"},
		{"blurry1008", true, "username+id, single token (H7)"},
		{"portia002", true, "username+id, single token (H7)"},
		{"0glm12mjcparl", true, "random low-vowel single token (H3/H7)"},

		// === Negative: should NOT flag — real titles ===
		{"Star Wars", false, "real 2-word title"},
		{"M", false, "single-letter movie title (1931 Fritz Lang)"},
		{"It", false, "2-letter title"},
		{"Up", false, "2-letter Pixar movie"},
		{"Bosch", false, "5-letter single-word show"},
		{"Fight Club", false, "real movie"},
		{"The Matrix", false, "real movie"},
		{"Friends", false, "TV show, single token"},
		{"Inception", false, "real movie, single token"},
		{"Naruto", false, "anime title, single token"},
		{"Nigashita Sakana wa Ookikatta", false, "romanized JP anime title"},
		{"Sambavam Adhyayam Onnu", false, "Tamil/Malayalam transliteration"},
		{"Ane wa Yanmama Junyuuchuu", false, "romanized JP title with unusual phonotactics"},
		{"Zeta Gundam", false, "anime title"},
		{"Дюна", false, "Cyrillic title — out of scope"},
		{"진격의 거인", false, "Korean — out of scope"},
		{"鬼滅の刃", false, "Japanese — out of scope"},
		{"عمر مختار", false, "Arabic — out of scope"},
		{"Avengers Endgame 2019", false, "real title with year"},
		{"Argentinacasting Melisa 2024", false, "adult studio + year (flagged by other rules, not garbage)"},
		{"S01E07", false, "season-episode shape — short but parseable"},
		{"Friends S01 1994", false, "TV show shape"},
		// Short single-token release-group salt (intentionally NOT
		// flagged — H2 was dropped because it false-positived "M",
		// "Up", "Ali", "Saw" etc. Tail-loss accepted; these strings
		// appear in <1% of corpus and the Group field in the parser
		// strips most ETRG/RARBG/YIFY suffixes upstream anyway.)
		{"etrg", false, "release-group salt (intentional FN, H2 dropped)"},
		{"frgo", false, "release-group salt (intentional FN, H2 dropped)"},
		{"btq", false, "3-char ID (intentional FN, H2 dropped)"},
	}
	for _, c := range cases {
		got := isGarbageTitle(c.in)
		if got != c.want {
			t.Errorf("isGarbageTitle(%q) = %v, want %v (%s)", c.in, got, c.want, c.why)
		}
	}
}
