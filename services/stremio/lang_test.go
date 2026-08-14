package stremio

import (
	"testing"
)

func TestExtractLanguages(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  []string // language Names in expected order
	}{
		{"empty title", "", nil},
		{"plain english tag", "Some.Movie.2024.1080p.eng.x264", []string{"English"}},
		{"flag emoji", "🇷🇺 Some Movie 2024", []string{"Russian"}},
		{"two-letter code", "Movie.2024.it.1080p", []string{"Italian"}},
		{"multiple langs deduped",
			"Movie.2024.eng.rus.RUS.1080p",
			[]string{"English", "Russian"}},
		{"skip 'no' standalone token",
			"There is no torrent here for it",
			[]string{"Italian"}}, // 'no' must be skipped, 'it' must match
		{"split on commas/parens",
			"Some.Movie (eng,fre)",
			[]string{"English", "French"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractLanguages(tc.title)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d %v, want %d %v", len(got), names(got), len(tc.want), tc.want)
			}
			for i, l := range got {
				if l.Name != tc.want[i] {
					t.Errorf("idx %d: got %q want %q (full: %v)", i, l.Name, tc.want[i], names(got))
				}
			}
		})
	}
}

func names(ls []*Language) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}

func TestLanguageByCode(t *testing.T) {
	if l := LanguageByCode("en"); l == nil || l.Name != "English" {
		t.Fatalf("want English for 'en', got %+v", l)
	}
	if l := LanguageByCode("ru"); l == nil || l.Name != "Russian" {
		t.Fatalf("want Russian for 'ru', got %+v", l)
	}
	if l := LanguageByCode(""); l != nil {
		t.Fatalf("want nil for empty code, got %+v", l)
	}
	if l := LanguageByCode("xx"); l != nil {
		t.Fatalf("want nil for unknown code, got %+v", l)
	}
}

func TestSortVaultFirstByResolution(t *testing.T) {
	// Resolutions are read from stream.Name. Cached items must come first
	// within each resolution bucket; the global resolution order set by
	// PreferredStream upstream must be preserved.
	streams := []StreamItem{
		{Name: "[WT]\nMovie.2024.1080p", Cached: false},
		{Name: "[⚡WT]\nMovie.2024.1080p", Cached: true},
		{Name: "[WT]\nMovie.2024.1080p", Cached: false},
		{Name: "[WT]\nMovie.2024.720p", Cached: false},
		{Name: "[⚡WT]\nMovie.2024.720p", Cached: true},
	}
	sortVaultFirstByResolution(streams)

	// 1080p bucket: cached must be first, but the two non-cached entries
	// keep their relative order.
	if !streams[0].Cached {
		t.Fatalf("1080p[0]: want cached first, got %+v", streams[0])
	}
	if streams[1].Cached || streams[2].Cached {
		t.Fatalf("1080p[1..2]: want non-cached, got %+v %+v", streams[1], streams[2])
	}
	// 720p bucket: cached first.
	if !streams[3].Cached {
		t.Fatalf("720p[0]: want cached first, got %+v", streams[3])
	}
	if streams[4].Cached {
		t.Fatalf("720p[1]: want non-cached after cached, got %+v", streams[4])
	}
}

// TestExtractLanguagesOnTrackerTitles uses titles as they actually arrive
// from a Russian tracker. Found in production: a user whose preferred
// language is Russian got a rutracker release filtered out of their Stremio
// list, because the title was transliterated English and carried no "rus"
// token — only "AVO".
func TestExtractLanguagesOnTrackerTitles(t *testing.T) {
	has := func(langs []*Language, name string) bool {
		for _, l := range langs {
			if l.Name == name {
				return true
			}
		}
		return false
	}

	for _, tt := range []struct {
		title string
		want  string
		not   string
	}{
		// The exact title from the production check.
		{title: "Breaking Bad / S1E1-62 of 62 [2008-2013, BDRip 720p] AVO ( «Goblin» )", want: "Russian"},
		{title: "Silo / S3E1-6 of 10 [2026, WEB-DL] MVO (LostFilm) + Original", want: "Russian"},
		{title: "Some.Release.2024.1080p.DVO", want: "Russian"},
		// Cyrillic anywhere is the tag by itself.
		{title: "Во все тяжкие / Breaking Bad [S01-05] BDRemux 1080p", want: "Russian"},
		// Ukrainian-only letters win over the Russian default.
		{title: "Укриття / Silo / Сезон 3 [укр. озвучення]", want: "Ukrainian", not: "Russian"},
		{title: "Укриття / Сезон 3 [українська озвучка]", want: "Ukrainian", not: "Russian"},
		// Explicit tags still work and must not be crowded out.
		{title: "Movie.2024.1080p.rus.eng", want: "English"},
		// Control: another scene's dub must not become Russian.
		{title: "Film.2024.German.DL.1080p.BluRay", want: "German", not: "Russian"},
		{title: "Movie.2024.1080p.WEB-DL.x264", not: "Russian"},
	} {
		got := ExtractLanguages(tt.title)
		if tt.want != "" && !has(got, tt.want) {
			names := []string{}
			for _, l := range got {
				names = append(names, l.Name)
			}
			t.Errorf("ExtractLanguages(%.50q…) = %v, want it to include %s", tt.title, names, tt.want)
		}
		if tt.not != "" && has(got, tt.not) {
			t.Errorf("ExtractLanguages(%.50q…) wrongly reports %s", tt.title, tt.not)
		}
	}
}
