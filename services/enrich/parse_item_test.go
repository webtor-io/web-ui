package enrich

import (
	"context"
	"reflect"
	"testing"

	"github.com/webtor-io/web-ui/models"
	ra "github.com/webtor-io/rest-api/services"
)

// titleAwareMapper returns metadata keyed on the VideoContent.Title
// it's asked about. Used by TestMapMetadata_PathTitleFallback to
// verify that mapMetadata walks each parser-harvested path-segment
// title as a search-key candidate before reaching for the AI fallback.
type titleAwareMapper struct {
	name string
	byT  map[string]*models.VideoMetadata
}

func (m *titleAwareMapper) GetName() string { return m.name }
func (m *titleAwareMapper) Map(_ context.Context, vc *models.VideoContent, _ models.ContentType, _ bool) (*models.VideoMetadata, error) {
	return m.byT[vc.Title], nil
}
func (m *titleAwareMapper) MapByID(_ context.Context, _ string, _ models.ContentType, _ bool) (*models.VideoMetadata, error) {
	return nil, nil
}

var _ MetadataMapper = (*titleAwareMapper)(nil)
var _ DirectMapper = (*titleAwareMapper)(nil)

// TestMapMetadata_PathTitleFallback locks in the "try the path-segment
// titles before falling through to Claude" branch in mapMetadata. The
// Freaks and Geeks case (real torrent 08b450441e) is the canonical
// example: the canonical Title ("Discos and Dragons" — the episode
// subtitle, before parseItem's series-shape fix) misses TMDB, but
// "Freaks and Geeks" — extracted from the root folder and stored in
// Metadata["path_titles"] — hits it. With the fallback the resource
// is enriched WITHOUT a Claude call.
//
// Asserts via a no-AI Enricher (aiResolver=nil): the only way the
// metadata can resolve is through the path-title iteration.
func TestMapMetadata_PathTitleFallback(t *testing.T) {
	mapper := &titleAwareMapper{
		name: "TMDB",
		byT: map[string]*models.VideoMetadata{
			"Freaks and Geeks": {
				VideoID:   "tt0193676",
				Title:     "Freaks and Geeks",
				PosterURL: "https://image.tmdb.org/p.jpg",
			},
		},
	}
	en := &Enricher{mappers: []MetadataMapper{mapper}}

	// Primary Title is the episode subtitle — misses. The path-title
	// list holds the series name as the root candidate. mapMetadata
	// must walk to that.
	vc := &models.VideoContent{
		Title: "Discos and Dragons",
		Metadata: map[string]any{
			"path_titles": []interface{}{
				"Freaks and Geeks",
				"Season 1",
				"Discos and Dragons",
			},
		},
	}
	md, err := en.mapMetadata(context.Background(), vc, models.ContentTypeSeries, false, "", "", nil)
	if err != nil {
		t.Fatalf("mapMetadata: %v", err)
	}
	if md == nil {
		t.Fatal("expected metadata resolved via path-title candidate; got nil")
	}
	if md.Title != "Freaks and Geeks" {
		t.Errorf("Title = %q, want %q", md.Title, "Freaks and Geeks")
	}
}

// TestMapMetadata_PathTitleAdultGate locks in the safety net that
// adult-flagged paths skip the path-title fallback. Without the gate,
// short site-tokens like "FC2" / "SLR" / "Brazzers" can accidentally
// hit obscure same-name films in TMDB and stamp wrong metadata onto
// an adult torrent (observed in prod 2026-05-13: candidate=FC2 →
// tmdb24376, a 2007 Japanese film unrelated to the JAV resource).
func TestMapMetadata_PathTitleAdultGate(t *testing.T) {
	mapper := &titleAwareMapper{
		name: "TMDB",
		byT: map[string]*models.VideoMetadata{
			"FC2":  {VideoID: "tmdb24376", Title: "FC2"},
			"PPV":  {VideoID: "tmdbXXXX", Title: "Some other film"},
			"Test": {VideoID: "tmdbYYYY", Title: "Test"},
		},
	}
	en := &Enricher{mappers: []MetadataMapper{mapper}}
	vc := &models.VideoContent{
		Title: "nonexistent",
		Metadata: map[string]any{
			"path_titles": []interface{}{"FC2", "PPV", "Test"},
		},
	}
	// Adult-flagged path — must NOT resolve via path-title candidates.
	// "Brazzers" is in the adult-studio alternation so isAdultPath
	// unambiguously fires on the path segment.
	md, err := en.mapMetadata(context.Background(), vc, models.ContentTypeMovie, false, "", "/Brazzers/some-scene.mp4", nil)
	if err != nil {
		t.Fatalf("mapMetadata: %v", err)
	}
	if md != nil {
		t.Fatalf("expected nil (path-title fallback gated by Adult); got %+v", md)
	}
}

// TestMapMetadata_PathTitleMinLength filters out path-segment titles
// shorter than 3 characters. Single-letter folder names ("D", "X")
// have TMDB collision risk against obscure shorts.
func TestMapMetadata_PathTitleMinLength(t *testing.T) {
	mapper := &titleAwareMapper{
		name: "TMDB",
		byT: map[string]*models.VideoMetadata{
			"D":      {VideoID: "tt0223152", Title: "D"},
			"Drama":  {VideoID: "tt9999999", Title: "Drama"},
		},
	}
	en := &Enricher{mappers: []MetadataMapper{mapper}}
	vc := &models.VideoContent{
		Title: "nothing in mapper",
		Metadata: map[string]any{
			// 1-letter "D" must be skipped; "Drama" must be tried.
			"path_titles": []interface{}{"D", "Drama"},
		},
	}
	md, err := en.mapMetadata(context.Background(), vc, models.ContentTypeMovie, false, "", "/some/path.mkv", nil)
	if err != nil {
		t.Fatalf("mapMetadata: %v", err)
	}
	if md == nil || md.VideoID != "tt9999999" {
		t.Fatalf("expected resolution via 'Drama' (>=3 chars); got %+v", md)
	}
}

// Sanity: when the primary Title already hits, the path-title loop
// must NOT run (we don't want to overwrite a primary mapper hit
// with a candidate match from a different title).
func TestMapMetadata_PrimaryTitleWins(t *testing.T) {
	mapper := &titleAwareMapper{
		name: "TMDB",
		byT: map[string]*models.VideoMetadata{
			"Apollo 13":  {VideoID: "tt0112384", Title: "Apollo 13"},
			"Some Other": {VideoID: "tt9999999", Title: "Some Other"},
		},
	}
	en := &Enricher{mappers: []MetadataMapper{mapper}}

	vc := &models.VideoContent{
		Title: "Apollo 13",
		Metadata: map[string]any{
			"path_titles": []interface{}{"Some Other", "Apollo 13"},
		},
	}
	md, err := en.mapMetadata(context.Background(), vc, models.ContentTypeMovie, false, "", "", nil)
	if err != nil {
		t.Fatalf("mapMetadata: %v", err)
	}
	if md == nil || md.Title != "Apollo 13" {
		t.Fatalf("expected Apollo 13 from primary search; got %+v", md)
	}
}

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
