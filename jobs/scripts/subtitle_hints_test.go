package scripts

import (
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
)

func TestSubtitleHintsPrefersEmbedSetting(t *testing.T) {
	h := subtitleHints("tt0000001", &models.VideoMetadata{VideoID: "tt0000002"}, models.ContentTypeMovie, &ra.ListItem{PathStr: "/Movie.2020.1080p.mkv"}, nil)
	if h.ImdbID != "tt0000001" {
		t.Fatalf("got %+v", h)
	}
}

// An embed/API caller passing imdb-id knows what it passes — the id is
// sent as-is and the path's season/episode ride along, whatever the
// stored enrichment says the resource is.
func TestSubtitleHintsEmbedSettingKeepsEpisodeAndIgnoresContentType(t *testing.T) {
	h := subtitleHints("tt0903747", nil, "", &ra.ListItem{PathStr: "/Breaking.Bad.S01/Breaking.Bad.S01E03.1080p.mkv"}, nil)
	if h.ImdbID != "tt0903747" || h.Season != 1 || h.Episode != 3 {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsFromEnrichment(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0109424"}, models.ContentTypeMovie, &ra.ListItem{PathStr: "/Movie.2020.1080p.mkv"}, nil)
	if h.ImdbID != "tt0109424" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsIgnoresTmdbOnlyID(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tmdb12345"}, models.ContentTypeMovie, &ra.ListItem{PathStr: "/Movie.mkv"}, nil)
	if h.ImdbID != "" {
		t.Fatalf("tmdb id must not be sent as imdb-id: %+v", h)
	}
}

func TestSubtitleHintsEpisodeFromPath(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, models.ContentTypeSeries, &ra.ListItem{PathStr: "/Breaking.Bad.S01/Breaking.Bad.S01E03.1080p.mkv"}, nil)
	if h.ImdbID != "tt0903747" || h.Season != 1 || h.Episode != 3 {
		t.Fatalf("got %+v", h)
	}
}

// The enrichment row for a series carries the SHOW's tt id. Sent
// without season+episode, OpenSubtitles reads it as a movie lookup and
// answers with subtitles for some arbitrary part of the show — worse
// than no subtitles. Only a fully-parsed SxxEyy earns the hint.
func TestSubtitleHintsSeriesWithoutSeasonSendsNothing(t *testing.T) {
	// episode known, season genuinely absent from the whole path
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, models.ContentTypeSeries, &ra.ListItem{PathStr: "/Show/Show - 03 - title.mkv"}, nil)
	if h.ImdbID != "" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("series with unknown season must send no hint: %+v", h)
	}
}

// The "Season 1" folder layout used to be the suppression case above:
// the parser read the folder as a title, the season stayed 0, and the
// hint was dropped for the most common series layout there is.
func TestSubtitleHintsSeasonFolderLayout(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, models.ContentTypeSeries, &ra.ListItem{PathStr: "/Season 1/Show - 03 - title.mkv"}, nil)
	if h.ImdbID != "tt0903747" || h.Season != 1 || h.Episode != 3 {
		t.Fatalf("the season folder is a season now: %+v", h)
	}
}

// The persisted ref is the identity watch history uses for this exact
// file: its show id and season/episode beat both the pack-level metadata
// row and a fresh parse of the file name.
func TestSubtitleHintsPreferThePersistedRef(t *testing.T) {
	ref := &models.VideoRef{Kind: models.VideoRefKindEpisode, VideoID: "tt1111111", Season: 4, Episode: 7}
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, models.ContentTypeSeries, &ra.ListItem{PathStr: "/Show/S01E03.mkv"}, ref)
	if h.ImdbID != "tt1111111" || h.Season != 4 || h.Episode != 7 {
		t.Fatalf("the episode row wins: %+v", h)
	}

	movie := &models.VideoRef{Kind: models.VideoRefKindMovie, VideoID: "tt2222222"}
	h = subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, models.ContentTypeMovie, &ra.ListItem{PathStr: "/Movie.S01E01.Special.mkv"}, movie)
	if h.ImdbID != "tt2222222" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("a movie ref sends the id and nothing else: %+v", h)
	}

	// A tmdb-synthetic ref cannot address OpenSubtitles: fall through to
	// the metadata row as before.
	synth := &models.VideoRef{Kind: models.VideoRefKindMovie, VideoID: "tmdb12345"}
	h = subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, models.ContentTypeMovie, &ra.ListItem{PathStr: "/Movie.mkv"}, synth)
	if h.ImdbID != "tt0903747" {
		t.Fatalf("a synthetic ref must not silence the enrichment fallback: %+v", h)
	}

	// The embed id still wins over everything, with the ref's numbers.
	h = subtitleHints("tt3333333", &models.VideoMetadata{VideoID: "tt0903747"}, models.ContentTypeSeries, &ra.ListItem{PathStr: "/Show/S01E03.mkv"}, ref)
	if h.ImdbID != "tt3333333" || h.Season != 4 || h.Episode != 7 {
		t.Fatalf("embed id with the ref's season/episode: %+v", h)
	}
}

func TestSubtitleHintsSeriesWithMovieLikePathSendsNothing(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, models.ContentTypeSeries, &ra.ListItem{PathStr: "/Some.Extra.Feature.1080p.mkv"}, nil)
	if h.ImdbID != "" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("series with an unparseable episode must send no hint: %+v", h)
	}
}

// Mirror image: a movie whose filename happens to look episodic must
// not turn into an episode lookup against a movie's imdb id.
func TestSubtitleHintsMovieNeverSendsSeasonEpisode(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0109424"}, models.ContentTypeMovie, &ra.ListItem{PathStr: "/Movie.S01E01.mkv"}, nil)
	if h.ImdbID != "tt0109424" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsNilSafe(t *testing.T) {
	h := subtitleHints("", nil, "", nil, nil)
	if h.ImdbID != "" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("got %+v", h)
	}
}

// Nothing enriched: no content type, so nothing to say even if a
// caller passed metadata along.
func TestSubtitleHintsWithoutContentTypeSendsNothing(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0109424"}, "", &ra.ListItem{PathStr: "/Movie.2020.1080p.mkv"}, nil)
	if h.ImdbID != "" {
		t.Fatalf("got %+v", h)
	}
}
