package scripts

import (
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
)

func TestSubtitleHintsPrefersEmbedSetting(t *testing.T) {
	h := subtitleHints("tt0000001", &models.VideoMetadata{VideoID: "tt0000002"}, &ra.ListItem{PathStr: "/Movie.2020.1080p.mkv"})
	if h.ImdbID != "tt0000001" {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsFromEnrichment(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0109424"}, &ra.ListItem{PathStr: "/Movie.2020.1080p.mkv"})
	if h.ImdbID != "tt0109424" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsIgnoresTmdbOnlyID(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tmdb12345"}, &ra.ListItem{PathStr: "/Movie.mkv"})
	if h.ImdbID != "" {
		t.Fatalf("tmdb id must not be sent as imdb-id: %+v", h)
	}
}

func TestSubtitleHintsEpisodeFromPath(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, &ra.ListItem{PathStr: "/Breaking.Bad.S01/Breaking.Bad.S01E03.1080p.mkv"})
	if h.ImdbID != "tt0903747" || h.Season != 1 || h.Episode != 3 {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsNilSafe(t *testing.T) {
	h := subtitleHints("", nil, nil)
	if h.ImdbID != "" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("got %+v", h)
	}
}
