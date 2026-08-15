package stremio

import "testing"

func TestMatchesRequestedSeason(t *testing.T) {
	for _, tt := range []struct {
		title  string
		season int
		want   bool
	}{
		// The case that started this: an indexer ignoring season= and
		// answering an S3 request with the first season.
		{"Укрытие / Silo / Сезон: 1 / Серии: 1-10 из 10 [2023 WEB-DL]", 3, false},
		{"Укрытие / Silo / Сезон: 3 / Серии: 1-4 из 10 [2026 WEB-DL]", 3, true},
		// Multi-season packs are a legitimate answer when they cover it.
		{"Укрытие / Silo / Сезоны: 1-3 [2023-2026]", 3, true},
		{"Укрытие / Silo / Сезоны: 1-2 [2023-2024]", 3, false},
		{"Silo S01-S03 COMPLETE 1080p", 3, true},
		{"Silo S01-S02 1080p", 3, false},
		// Latin spellings.
		{"Silo.S03E01.2160p.WEB-DL", 3, true},
		{"Silo.S01E01.2160p.WEB-DL", 3, false},
		{"Silo Season 3 1080p", 3, true},
		{"Silo Season 1 1080p", 3, false},
		// A title that names no season is kept: dropping it would lose
		// good results to a parsing miss.
		{"Silo 2160p ATVP WEB-DL DDP5 1 Atmos DoVi HDR", 3, true},
		{"", 3, true},
		// No season requested (a movie) filters nothing.
		{"Anything at all S01", 0, true},
		// Two-digit seasons must not be truncated.
		{"Long Show S10E02", 10, true},
		{"Long Show S10E02", 1, false},
		// Number-first word order — what RuTor, Kinozal and NNM-Club use.
		// Every one of these passed the filter before the pattern existed.
		{"Мандалорец / The Mandalorian [3 сезон: 1-8 серии из 8] (2023)", 3, true},
		{"Мандалорец / The Mandalorian [3 сезон: 1-8 серии из 8] (2023)", 1, false},
		{"Ведьмак / The Witcher (2 сезон) 2021 WEB-DL", 4, false},
		{"Во все тяжкие 5 сезон", 5, true},
		{"Во все тяжкие 5 сезон", 2, false},
		{"Сваты (1-7 сезоны) 2008-2021", 3, true},
		{"Сваты (1-7 сезоны) 2008-2021", 8, false},
		{"Клиника 3-й сезон", 3, true},
		{"Клиника 3-й сезон", 1, false},
		// A count is a pack of 1..N, not season N. Reading "10 сезонов" as
		// season 10 would drop a complete-series pack from every request but
		// the tenth.
		{"Друзья / Friends [10 сезонов] (1994-2004)", 3, true},
		{"Друзья / Friends [10 сезонов] (1994-2004)", 10, true},
		{"Друзья / Friends [10 сезонов] (1994-2004)", 11, false},
		{"The Office (US) 9 seasons complete", 4, true},
		{"The Office (US) 9 seasons complete", 10, false},
		// Episode counts must not be read as seasons.
		{"Silo / 1-8 серии из 10 [2023]", 3, true},
	} {
		if got := matchesRequestedSeason(tt.title, tt.season); got != tt.want {
			t.Errorf("matchesRequestedSeason(%q, %d) = %v, want %v", tt.title, tt.season, got, tt.want)
		}
	}
}

// TestMatchesRequestedSeasonOnRealTitles runs the guard over titles captured
// from a live Jackett (all indexers, plain "Silo" query, 2026-08-14). Hand
// -written cases cover the shapes I thought of; this covers the shapes a
// tracker actually uses — including "[S03E01-06 of 10]" in a bracketed
// Russian title, which none of the invented cases looked like.
func TestMatchesRequestedSeasonOnRealTitles(t *testing.T) {
	const wantSeason = 3
	for _, tt := range []struct {
		title string
		want  bool
	}{
		{"Silo / S1E1-10 of 10 [2023, HDR10+, Dolby Vision, WEB-DL 2160p] [Hybrid] Dub + 3 x MVO (LostFilm, HDRezka, TVShows) + Original + Sub (Rus, Ukr, Eng)", false},
		{"Silo / S1E1-10 of 10 [2023, WEB-DL 1080p] Dub + Original + Sub (Rus, Eng, Heb, Ukr)", false},
		{"Silo / S2E1-10 of 10 [2024, Dolby Vision, HDR10+, WEB-DL 2160p] 7 x MVO (HDRezka, NewComers, LostFilm) + 2 x Ukr + Original", false},
		{"Silo / S2E1-10 of 10 [2024, WEB-DL 720p] 7 x MVO + VO + Original + Sub (Rus, Eng)", false},
		{"Silo / S3E1-6 of 10 [2026, HDR10, HDR10+, Dolby Vision, WEB-DL 2160p] MVO (HDRezka Studio) + Original + Sub (Rus, Ukr, Eng, Heb)", true},
		{"Silo / S3E1-6 of 10 [2026, WEB-DL 1080p] MVO (HDRezka Studio) + Original + Sub (Rus, Ukr, Eng, Heb)", true},
		{"Укрытие / Бункер / Silo [S03E01-06 of 10] (2026) WEB-DL 1080p | HDrezka Studio", true},
		{"Укрытие / Бункер / Silo [S01] (2023) WEB-DL 1080p-Невафильм", false},
		{"Укрытие / Бункер / Silo [S02] (2024) UHD WEB-DL 2160p | 4K | HDR10+ | Dolby Vision Profile 8 | P, L", false},
	} {
		if got := matchesRequestedSeason(tt.title, wantSeason); got != tt.want {
			t.Errorf("matchesRequestedSeason(%.60q…, %d) = %v, want %v", tt.title, wantSeason, got, tt.want)
		}
	}
}
