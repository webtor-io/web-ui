package user_subtitle

import "testing"

// A <track kind="subtitles"> is required by HTML to carry srclang, and user
// uploads were the only subtitle source rendering it empty. Desktop Chrome
// tolerates that; other media stacks need not.
func TestLangFromName(t *testing.T) {
	for _, tt := range []struct {
		name string
		want string
	}{
		// Language carried in the second extension, the convention players
		// and rest-api's sidecar handling already use.
		{"Coyote.vs.Acme.en.srt", "en"},
		{"movie.ru.ass", "ru"},
		{"show.s01e01.pt-BR.vtt", "pt-BR"},
		// Three-letter codes canonicalise to their two-letter form.
		{"movie.eng.srt", "en"},

		// Negative controls: nothing parseable must yield "und" rather than
		// an empty attribute or a guess.
		{"movie.srt", "und"},
		{"movie.1080p.srt", "und"},
		{"movie.xyzzy.srt", "und"},
		{"subtitles.srt", "und"},
		{"", "und"},
		// A bare release tag must not be mistaken for a language.
		{"Coyote.vs.Acme.2026.1080p.HEVC.srt", "und"},
	} {
		if got := LangFromName(tt.name); got != tt.want {
			t.Errorf("LangFromName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
