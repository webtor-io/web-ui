package enrich

import (
	"testing"

	"github.com/webtor-io/web-ui/models"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
	ra "github.com/webtor-io/rest-api/services"
)

func mkInfo(title string, season, episode, scene int) *TorrentInfo {
	return &TorrentInfo{
		TorrentInfo: &ptn.TorrentInfo{
			Title:   title,
			Season:  season,
			Episode: episode,
			Scene:   scene,
		},
	}
}

func mkMovieInfo(title string, year int, path string) *TorrentInfo {
	return &TorrentInfo{
		TorrentInfo: &ptn.TorrentInfo{Title: title, Year: year},
		ListItem:    &ra.ListItem{PathStr: path},
	}
}

// Each case mirrors a real-world torrent we hit in production. The
// classifier was historically too eager to declare SeriesSingleSeason
// whenever the parser saw any episode-like number — which then routed
// movies through the series-metadata code path and let Kinopoisk
// Unofficial overwrite them with kp{id} entries. These cases lock in
// the corrected behaviour: scrappy episode hints alone don't flip a
// torrent into a series.
func TestGetMediaType(t *testing.T) {
	cases := []struct {
		name  string
		infos []*TorrentInfo
		want  models.MediaInfoMediaType
	}{
		{
			"single-file movie, no markers",
			[]*TorrentInfo{mkInfo("Interstellar", 0, 0, 0)},
			models.MediaInfoMediaTypeMovieSingle,
		},
		{
			"movie with sample (2 files, same title)",
			[]*TorrentInfo{
				mkInfo("Some Movie", 0, 0, 0),
				mkInfo("Some Movie", 0, 0, 0),
			},
			models.MediaInfoMediaTypeMovieSingle,
		},
		{
			"single-file movie with parser-leaked episode (e.g. - 1046 from filename)",
			[]*TorrentInfo{mkInfo("Interestelar", 0, 1046, 0)},
			models.MediaInfoMediaTypeMovieSingle,
		},
		{
			"multi-movie pack, sameTitle=false with leaked episode part numbers (Le Hobbit + LOTR)",
			[]*TorrentInfo{
				mkInfo("Le Hobbit", 0, 1, 0),
				mkInfo("Le Seigneur des Anneaux", 0, 1, 0),
				mkInfo("Le Hobbit", 0, 2, 0),
				mkInfo("Le Seigneur des Anneaux", 0, 2, 0),
				mkInfo("Le Hobbit", 0, 3, 0),
				mkInfo("Le Seigneur des Anneaux", 0, 3, 0),
			},
			models.MediaInfoMediaTypeMovieMultiple,
		},
		{
			"trilogy of distinct movies, no episode tags (Home Alone)",
			[]*TorrentInfo{
				mkInfo("Home Alone", 0, 0, 0),
				mkInfo("Home Alone 2 Lost in New York", 0, 0, 0),
				mkInfo("Home Alone 3", 0, 0, 0),
			},
			models.MediaInfoMediaTypeMovieMultiple,
		},
		{
			"single-season series with explicit S01EXX",
			[]*TorrentInfo{
				mkInfo("Granica Mirov", 2, 1, 0),
				mkInfo("Granica Mirov", 2, 2, 0),
				mkInfo("Granica Mirov", 2, 3, 0),
			},
			models.MediaInfoMediaTypeSeriesSingleSeason,
		},
		{
			"multi-season series",
			[]*TorrentInfo{
				mkInfo("BBT", 1, 1, 0),
				mkInfo("BBT", 1, 2, 0),
				mkInfo("BBT", 2, 1, 0),
				mkInfo("BBT", 2, 2, 0),
			},
			models.MediaInfoMediaTypeSeriesMultipleSeasons,
		},
		{
			"season-less anime fansub, sameTitle + sequential episodes (>=3 files)",
			[]*TorrentInfo{
				mkInfo("Naruto", 0, 1, 0),
				mkInfo("Naruto", 0, 2, 0),
				mkInfo("Naruto", 0, 3, 0),
				mkInfo("Naruto", 0, 4, 0),
			},
			models.MediaInfoMediaTypeSeriesSingleSeason,
		},
		{
			"split-scenes torrent",
			[]*TorrentInfo{
				mkInfo("Some Adult Pack", 0, 0, 1),
				mkInfo("Some Adult Pack", 0, 0, 2),
				mkInfo("Some Adult Pack", 0, 0, 3),
			},
			models.MediaInfoMediaTypeSeriesSplitScenes,
		},
	}

	en := &Enricher{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := en.getMediaType(c.infos)
			if got != c.want {
				t.Fatalf("getMediaType: want %v, got %v", c.want, got)
			}
		})
	}
}

// Home Alone Trilogy is the canonical multi-movie pack: a single torrent
// hash carries three distinct films with different titles and years. The
// enricher must produce three Movie rows so each can resolve to its own
// TMDB id (tt0099785, tt0104431, tt0120402) — historically the whole
// torrent fell into SeriesCompilation and got no metadata at all.
func TestMakeMovies_HomeAloneTrilogy(t *testing.T) {
	hash := "deadbeef"
	en := &Enricher{}
	infos := []*TorrentInfo{
		mkMovieInfo("Home Alone", 1990, "/Trilogy/Home Alone (1990).mkv"),
		mkMovieInfo("Home Alone 2 Lost in New York", 1992, "/Trilogy/Home Alone 2 Lost in New York (1992).mkv"),
		mkMovieInfo("Home Alone 3", 1997, "/Trilogy/Home Alone 3 (1997).mkv"),
	}

	movies, err := en.makeMovies(infos, hash)
	if err != nil {
		t.Fatalf("makeMovies: %v", err)
	}
	if len(movies) != 3 {
		t.Fatalf("expected 3 movies, got %d", len(movies))
	}

	expect := []struct {
		title string
		year  int16
	}{
		{"Home Alone", 1990},
		{"Home Alone 2 Lost in New York", 1992},
		{"Home Alone 3", 1997},
	}
	for i, m := range movies {
		if m.Title != expect[i].title {
			t.Errorf("movie[%d].Title = %q, want %q", i, m.Title, expect[i].title)
		}
		if m.Year == nil || *m.Year != expect[i].year {
			t.Errorf("movie[%d].Year = %v, want %d", i, m.Year, expect[i].year)
		}
		if m.ResourceID != hash {
			t.Errorf("movie[%d].ResourceID = %q, want %q", i, m.ResourceID, hash)
		}
	}
}

// Multi-disc / multi-audio packs share a parsed title across files;
// makeMovies must collapse them into a single Movie row, otherwise the
// same film gets enriched twice and we end up with duplicate cards.
func TestMakeMovies_DeduplicatesByTitle(t *testing.T) {
	en := &Enricher{}
	infos := []*TorrentInfo{
		mkMovieInfo("The Movie", 2020, "/path/disc1.mkv"),
		mkMovieInfo("The Movie", 2020, "/path/disc2.mkv"),
		mkMovieInfo("Bonus Feature", 2020, "/path/extras.mkv"),
	}
	movies, err := en.makeMovies(infos, "h")
	if err != nil {
		t.Fatalf("makeMovies: %v", err)
	}
	if len(movies) != 2 {
		t.Fatalf("expected 2 movies (deduplicated), got %d", len(movies))
	}
	if movies[0].Title != "The Movie" || movies[1].Title != "Bonus Feature" {
		t.Fatalf("unexpected dedup order: %v / %v", movies[0].Title, movies[1].Title)
	}
}
