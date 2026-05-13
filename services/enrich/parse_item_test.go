package enrich

import (
	"reflect"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

// TestParseItem_MultiSegmentPath locks in how the per-path-segment
// parser merges fields across a torrent file path. Each path level
// often carries different metadata — the root folder usually names
// the series/movie, the season/episode markers live in a sub-folder
// or filename, the technical tags (resolution, codec, group) sit in
// the filename. parseItem must surface ALL of those without one
// segment's parse OVERWRITING another's contribution.
//
// Cases driven by real torrents in production telemetry.
func TestParseItem_MultiSegmentPath(t *testing.T) {
	cases := []struct {
		name string
		path string

		// Whole-info expectations. Only non-zero fields are asserted;
		// zero values mean "we don't care for this case".
		wantTitle      string
		wantSeason     int
		wantEpisode    int
		wantYear       int
		wantContainer  string
		wantQuality    string
		wantResolution string
		wantPathTitles []string // titles from each segment, root-first
	}{
		{
			// Real torrent 08b450441e — Freaks and Geeks. Root folder
			// holds the SERIES name, the file holds the EPISODE title.
			// Pre-fix the file's title overwrote the root one, so the
			// final Title was "Discos and Dragons" (an episode title)
			// — useless as a TMDB key. Fix preserves the root title
			// and exposes ALL parsed segment titles via PathTitles so
			// the enricher can try each as a metadata candidate.
			name:           "series episode under series root folder",
			path:           "/Freaks and Geeks/Season 1/Episode 18 - Discos and Dragons.mkv",
			wantTitle:      "Freaks and Geeks",
			wantEpisode:    18,
			wantContainer:  "mkv",
			wantPathTitles: []string{"Freaks and Geeks", "Season 1", "Discos and Dragons"},
		},
		{
			// Movie at filesystem root — no parent folder. Title comes
			// from the only segment; PathTitles has a single entry.
			// Quality value "BRRip" is the MapTransformer canonical
			// form for raw "BrRip" (see qualityTransformer).
			name:           "single-segment movie",
			path:           "/Hercules (2014) 1080p BrRip H264 - YIFY.mkv",
			wantTitle:      "Hercules",
			wantYear:       2014,
			wantResolution: "1080p",
			wantQuality:    "BRRip",
			wantContainer:  "mkv",
			wantPathTitles: []string{"Hercules"},
		},
		{
			// Two-segment series with explicit SxxExx in filename.
			// Title from root, Season/Episode from file. Earlier code
			// would set Title="Pilot" (the episode name leaking).
			name:           "S01E01 series episode under series root",
			path:           "/Breaking Bad/Breaking.Bad.S01E01.Pilot.720p.BluRay.x264.mkv",
			wantTitle:      "Breaking Bad",
			wantSeason:     1,
			wantEpisode:    1,
			wantResolution: "720p",
			wantQuality:    "BluRay",
			wantContainer:  "mkv",
			// Both segments parse to Title="Breaking Bad" (the "Pilot"
			// token leaks into Extra, not Title), and the dedup in
			// parseItem collapses the duplicate. Single entry expected.
			wantPathTitles: []string{"Breaking Bad"},
		},
		{
			// Movie in a GENERIC parent folder ("Movies" / "Downloads"
			// / "TV Shows"). The root-title-wins rule must NOT trigger
			// here — the file segment has no Episode marker, so the
			// file-level Title is the canonical movie title.
			name:           "movie in generic parent folder",
			path:           "/Movies/Hercules (2014) 1080p BluRay x264.mkv",
			wantTitle:      "Hercules",
			wantYear:       2014,
			wantResolution: "1080p",
			wantQuality:    "BluRay",
			wantContainer:  "mkv",
			wantPathTitles: []string{"Movies", "Hercules"},
		},
		{
			// Series with SxxExx in filename inside a generic parent.
			// Title comes from the FILE segment (it parsed Season=1
			// AND Episode=1 alongside the series Title), so the
			// root-title-wins rule must NOT fire (the file's Title
			// is already the series name, not just an episode name).
			name:           "S01E01 series under generic parent folder",
			path:           "/TV Shows/Breaking.Bad.S01E01.Pilot.720p.BluRay.x264.mkv",
			wantTitle:      "Breaking Bad",
			wantSeason:     1,
			wantEpisode:    1,
			wantResolution: "720p",
			wantQuality:    "BluRay",
			wantContainer:  "mkv",
			wantPathTitles: []string{"TV Shows", "Breaking Bad"},
		},
		{
			// Anime fansub structure: group tag in root, episode in file.
			// PathTitles should expose both "Dies Irae" (file Title)
			// and the fansub root if it parses to anything.
			name:           "anime fansub series",
			path:           "/[Cleo] Dies Irae [BD1080p_x265]/[Cleo]Dies_Irae_-_ONA_01_(Dual Audio_10bit_BD1080p_x265).mkv",
			wantTitle:      "Dies Irae",
			wantEpisode:    1,
			wantContainer:  "mkv",
			wantResolution: "1080p",
			wantQuality:    "BluRay",
			wantPathTitles: []string{"Dies Irae"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item := &ra.ListItem{PathStr: c.path}
			ti, err := parseItem(item)
			if err != nil {
				t.Fatalf("parseItem(%q): %v", c.path, err)
			}
			if ti.Title != c.wantTitle {
				t.Errorf("Title = %q, want %q", ti.Title, c.wantTitle)
			}
			if c.wantSeason != 0 && ti.Season != c.wantSeason {
				t.Errorf("Season = %d, want %d", ti.Season, c.wantSeason)
			}
			if c.wantEpisode != 0 && ti.Episode != c.wantEpisode {
				t.Errorf("Episode = %d, want %d", ti.Episode, c.wantEpisode)
			}
			if c.wantYear != 0 && ti.Year != c.wantYear {
				t.Errorf("Year = %d, want %d", ti.Year, c.wantYear)
			}
			if c.wantContainer != "" && ti.Container != c.wantContainer {
				t.Errorf("Container = %q, want %q", ti.Container, c.wantContainer)
			}
			if c.wantQuality != "" && ti.Quality != c.wantQuality {
				t.Errorf("Quality = %q, want %q", ti.Quality, c.wantQuality)
			}
			if c.wantResolution != "" && ti.Resolution != c.wantResolution {
				t.Errorf("Resolution = %q, want %q", ti.Resolution, c.wantResolution)
			}
			if c.wantPathTitles != nil {
				if !reflect.DeepEqual(ti.PathTitles, c.wantPathTitles) {
					t.Errorf("PathTitles = %q, want %q", ti.PathTitles, c.wantPathTitles)
				}
			}
		})
	}
}
