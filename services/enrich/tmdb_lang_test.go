package enrich

import "testing"

// TMDB has no bare "zh": its Chinese translations are per region, and our
// locale is the Simplified one.
func TestTmdbLang(t *testing.T) {
	for in, want := range map[string]string{"en": "en", "ru": "ru", "pt": "pt-BR", "zh": "zh-CN"} {
		if got := tmdbLang(in); got != want {
			t.Errorf("tmdbLang(%q) = %q, want %q", in, got, want)
		}
	}
}
