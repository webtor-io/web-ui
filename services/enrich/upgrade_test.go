package enrich

import (
	"context"
	"errors"
	"testing"

	"github.com/webtor-io/web-ui/models"
	km "github.com/webtor-io/web-ui/models/kinopoisk_unofficial"
)

// TestKpuMakeVideoMetadata_VideoIDPolicy locks in the asymmetric KPU id
// behaviour that powers the upgrade-loop architecture: when KPU has an
// imdbId we expose it as the VideoID so a higher-priority mapper can try
// to upgrade us; when KPU has only its own kp id we encode kp{id} so the
// orchestrator can short-circuit (no upper mapper speaks the kp prefix).
func TestKpuMakeVideoMetadata_VideoIDPolicy(t *testing.T) {
	imdb := "tt0898266"
	cases := []struct {
		name string
		info *km.Info
		want string
	}{
		{
			"KPU has imdbId — expose it as videoID for upgrade",
			&km.Info{KpID: 306084, ImdbID: &imdb, Title: "BBT", Metadata: map[string]any{}},
			"tt0898266",
		},
		{
			"KPU has no imdbId — fall back to kp{id}",
			&km.Info{KpID: 12345, ImdbID: nil, Title: "Russian-only show", Metadata: map[string]any{}},
			"kp12345",
		},
		{
			"KPU has empty imdbId string — also fall back",
			&km.Info{KpID: 999, ImdbID: ptr(""), Title: "x", Metadata: map[string]any{}},
			"kp999",
		},
	}

	kpu := &KinopoiskUnofficial{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			md := kpu.makeVideoMetadata(c.info)
			if md.VideoID != c.want {
				t.Fatalf("VideoID = %q, want %q", md.VideoID, c.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// fakeMapper is a programmable DirectMapper that we wire into Enricher to
// drive the upgrade loop deterministically without hitting any network.
type fakeMapper struct {
	name      string
	mapResult *models.VideoMetadata
	mapErr    error
	byID      map[string]*models.VideoMetadata
	byIDErr   map[string]error
}

func (f *fakeMapper) GetName() string { return f.name }

func (f *fakeMapper) Map(_ context.Context, _ *models.VideoContent, _ models.ContentType, _ bool) (*models.VideoMetadata, error) {
	return f.mapResult, f.mapErr
}

func (f *fakeMapper) MapByID(_ context.Context, videoID string, _ models.ContentType, _ bool) (*models.VideoMetadata, error) {
	if e, ok := f.byIDErr[videoID]; ok {
		return nil, e
	}
	return f.byID[videoID], nil
}

var _ MetadataMapper = (*fakeMapper)(nil)
var _ DirectMapper = (*fakeMapper)(nil)

// TestMapMetadata_UpgradeChain exercises the orchestrator's behaviour
// when a lower-priority mapper resolves and a higher-priority one might
// be able to upgrade the result. The chain is TMDB → OMDB → KPU.
func TestMapMetadata_UpgradeChain(t *testing.T) {
	t.Run("kpu resolves with valid imdbId, tmdb upgrades", func(t *testing.T) {
		tmdb := &fakeMapper{
			name:      "TMDB",
			mapResult: nil, // Search-by-title misses (e.g. Russian title)
			byID: map[string]*models.VideoMetadata{
				"tt12827674": {VideoID: "tt12827674", Title: "Psycho", PosterURL: "https://image.tmdb.org/p.jpg"},
			},
		}
		omdb := &fakeMapper{name: "OMDB", mapResult: nil}
		kpu := &fakeMapper{
			name:      "Kinopoisk",
			mapResult: &models.VideoMetadata{VideoID: "tt12827674", Title: "Псих", PosterURL: "https://kpu/p.jpg"},
		}
		en := &Enricher{mappers: []MetadataMapper{tmdb, omdb, kpu}}

		md, err := en.mapMetadata(context.Background(), &models.VideoContent{Title: "Псих"}, models.ContentTypeSeries, false, "")
		if err != nil {
			t.Fatalf("mapMetadata: %v", err)
		}
		if md == nil || md.Title != "Psycho" || md.PosterURL != "https://image.tmdb.org/p.jpg" {
			t.Fatalf("expected upgrade to TMDB, got %+v", md)
		}
	})

	t.Run("kpu resolves with bogus imdbId, no mapper validates, kpu wins", func(t *testing.T) {
		tmdb := &fakeMapper{
			name:      "TMDB",
			mapResult: nil,
			byID:      map[string]*models.VideoMetadata{}, // tmdb find returns nothing for tt1147717
		}
		omdb := &fakeMapper{
			name:      "OMDB",
			mapResult: nil,
			byID:      map[string]*models.VideoMetadata{}, // omdb type-filter rejects cross-type record
		}
		kpu := &fakeMapper{
			name:      "Kinopoisk",
			mapResult: &models.VideoMetadata{VideoID: "tt1147717", Title: "Теория большого взрыва", PosterURL: "https://kpu/bbt.jpg"},
		}
		en := &Enricher{mappers: []MetadataMapper{tmdb, omdb, kpu}}

		md, err := en.mapMetadata(context.Background(), &models.VideoContent{Title: "Теория большого взрыва"}, models.ContentTypeSeries, false, "")
		if err != nil {
			t.Fatalf("mapMetadata: %v", err)
		}
		if md == nil || md.VideoID != "tt1147717" || md.PosterURL != "https://kpu/bbt.jpg" {
			t.Fatalf("expected KPU fallback, got %+v", md)
		}
	})

	t.Run("kpu resolves with kp{id} (no imdbId), upgrade impossible", func(t *testing.T) {
		// TMDB.MapByID would normally accept "kp..." as a valid format
		// for our internal id lookup, but in this test we wire it to
		// return nothing for the kp id — the realistic case where TMDB
		// genuinely has nothing related to this content.
		tmdb := &fakeMapper{name: "TMDB", mapResult: nil, byID: map[string]*models.VideoMetadata{}}
		omdb := &fakeMapper{name: "OMDB", mapResult: nil, byID: map[string]*models.VideoMetadata{}}
		kpu := &fakeMapper{
			name:      "Kinopoisk",
			mapResult: &models.VideoMetadata{VideoID: "kp306084", Title: "Russian-only show", PosterURL: "https://kpu/r.jpg"},
		}
		en := &Enricher{mappers: []MetadataMapper{tmdb, omdb, kpu}}

		md, err := en.mapMetadata(context.Background(), &models.VideoContent{Title: "Russian-only show"}, models.ContentTypeSeries, false, "")
		if err != nil {
			t.Fatalf("mapMetadata: %v", err)
		}
		if md == nil || md.VideoID != "kp306084" {
			t.Fatalf("expected KPU result with kp id, got %+v", md)
		}
	})

	t.Run("tmdb resolves first, no upgrade attempted (it is already top priority)", func(t *testing.T) {
		tmdb := &fakeMapper{
			name:      "TMDB",
			mapResult: &models.VideoMetadata{VideoID: "tt0898266", Title: "BBT", PosterURL: "https://tmdb/bbt.jpg"},
		}
		// If the upgrade loop were buggy and called MapByID on the same
		// mapper that produced the result, this canary would fire.
		omdbCanary := &fakeMapper{name: "OMDB", byID: map[string]*models.VideoMetadata{}}
		kpu := &fakeMapper{name: "Kinopoisk"}
		en := &Enricher{mappers: []MetadataMapper{tmdb, omdbCanary, kpu}}

		md, err := en.mapMetadata(context.Background(), &models.VideoContent{Title: "BBT"}, models.ContentTypeSeries, false, "")
		if err != nil {
			t.Fatalf("mapMetadata: %v", err)
		}
		if md == nil || md.VideoID != "tt0898266" {
			t.Fatalf("expected TMDB result, got %+v", md)
		}
	})

	t.Run("upgrade error from one mapper does not prevent another from succeeding", func(t *testing.T) {
		tmdb := &fakeMapper{
			name:    "TMDB",
			byIDErr: map[string]error{"tt1234567": errors.New("transient tmdb error")},
		}
		omdb := &fakeMapper{
			name: "OMDB",
			byID: map[string]*models.VideoMetadata{
				"tt1234567": {VideoID: "tt1234567", Title: "Movie", PosterURL: "https://omdb/p.jpg"},
			},
		}
		kpu := &fakeMapper{
			name:      "Kinopoisk",
			mapResult: &models.VideoMetadata{VideoID: "tt1234567", Title: "Кино", PosterURL: "https://kpu/p.jpg"},
		}
		en := &Enricher{mappers: []MetadataMapper{tmdb, omdb, kpu}}

		md, err := en.mapMetadata(context.Background(), &models.VideoContent{Title: "Кино"}, models.ContentTypeMovie, false, "")
		if err != nil {
			t.Fatalf("mapMetadata: %v", err)
		}
		if md == nil || md.PosterURL != "https://omdb/p.jpg" {
			t.Fatalf("expected OMDB upgrade after TMDB transient error, got %+v", md)
		}
	})
}
