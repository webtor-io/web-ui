package user_video_status

import (
	"context"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// --- Mock store ---

type mockStore struct {
	movies   map[string]*models.MovieStatus  // key: videoID
	series   map[string]*models.SeriesStatus // key: videoID
	episodes map[episodeKey]*models.EpisodeStatus
	// total episodes known per series video_id (set by test)
	episodeTotals map[string]int

	upsertSeriesErr error
	upsertMovieErr  error
	upsertEpisodeErr error
}

type episodeKey struct {
	videoID string
	season  int16
	episode int16
}

func newMockStore() *mockStore {
	return &mockStore{
		movies:        map[string]*models.MovieStatus{},
		series:        map[string]*models.SeriesStatus{},
		episodes:      map[episodeKey]*models.EpisodeStatus{},
		episodeTotals: map[string]int{},
	}
}

func (m *mockStore) UpsertMovieStatus(_ context.Context, s *models.MovieStatus) error {
	if m.upsertMovieErr != nil {
		return m.upsertMovieErr
	}
	m.movies[s.VideoID] = s
	return nil
}

func (m *mockStore) DeleteMovieStatus(_ context.Context, _ uuid.UUID, videoID string) error {
	delete(m.movies, videoID)
	return nil
}

func (m *mockStore) UpsertSeriesStatus(_ context.Context, s *models.SeriesStatus) error {
	if m.upsertSeriesErr != nil {
		return m.upsertSeriesErr
	}
	m.series[s.VideoID] = s
	return nil
}

func (m *mockStore) DeleteSeriesStatus(_ context.Context, _ uuid.UUID, videoID string) error {
	delete(m.series, videoID)
	return nil
}

func (m *mockStore) DeleteSeriesStatusBySource(_ context.Context, _ uuid.UUID, videoID string, source models.UserVideoSource) error {
	if s, ok := m.series[videoID]; ok && s.Source == source {
		delete(m.series, videoID)
	}
	return nil
}

func (m *mockStore) UpsertEpisodeStatus(_ context.Context, s *models.EpisodeStatus) error {
	if m.upsertEpisodeErr != nil {
		return m.upsertEpisodeErr
	}
	m.episodes[episodeKey{videoID: s.VideoID, season: s.Season, episode: s.Episode}] = s
	return nil
}

func (m *mockStore) DeleteEpisodeStatus(_ context.Context, _ uuid.UUID, videoID string, season, episode int16) error {
	delete(m.episodes, episodeKey{videoID: videoID, season: season, episode: episode})
	return nil
}

func (m *mockStore) CountWatchedEpisodes(_ context.Context, _ uuid.UUID, videoID string) (int, error) {
	n := 0
	for k, s := range m.episodes {
		if k.videoID == videoID && s.Watched {
			n++
		}
	}
	return n, nil
}

func (m *mockStore) CountEpisodeMetadataByVideoID(_ context.Context, videoID string) (int, error) {
	return m.episodeTotals[videoID], nil
}

func (m *mockStore) SetWatchHistoryWatchedForMovie(_ context.Context, _ uuid.UUID, _ string, _ bool) error {
	return nil
}

func (m *mockStore) SetWatchHistoryWatchedForEpisode(_ context.Context, _ uuid.UUID, _ string, _, _ int16, _ bool) error {
	return nil
}

func (m *mockStore) FilterWatchedMovieIDs(_ context.Context, _ uuid.UUID, _ []string) ([]string, error) {
	return nil, nil
}

func (m *mockStore) FilterWatchedSeriesIDs(_ context.Context, _ uuid.UUID, _ []string) ([]string, error) {
	return nil, nil
}

// --- Tests ---

func newTestService(store userVideoStatusStore) *Service {
	return &Service{store: store}
}

func TestMarkMovieWatched(t *testing.T) {
	store := newMockStore()
	svc := newTestService(store)
	userID := uuid.NewV4()

	err := svc.MarkMovieWatched(context.Background(), userID, "tt0133093", models.UserVideoSourceManual)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := store.movies["tt0133093"]
	if got == nil {
		t.Fatal("expected movie status to be upserted")
	}
	if !got.Watched {
		t.Error("expected watched=true")
	}
	if got.Source != models.UserVideoSourceManual {
		t.Errorf("expected source 'manual', got %s", got.Source)
	}
	if got.WatchedAt == nil {
		t.Error("expected watched_at to be set")
	}
}

func TestMarkMovieWatched_EmptyVideoID(t *testing.T) {
	svc := newTestService(newMockStore())
	err := svc.MarkMovieWatched(context.Background(), uuid.NewV4(), "", models.UserVideoSourceManual)
	if err == nil {
		t.Fatal("expected error for empty videoID")
	}
}

func TestUnmarkMovie(t *testing.T) {
	store := newMockStore()
	svc := newTestService(store)
	userID := uuid.NewV4()
	store.movies["tt0133093"] = &models.MovieStatus{VideoID: "tt0133093"}

	if err := svc.UnmarkMovie(context.Background(), userID, "tt0133093"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := store.movies["tt0133093"]; exists {
		t.Error("expected movie status to be deleted")
	}
}

func TestMarkEpisodeWatched_DoesNotAutoMarkSeriesWhenIncomplete(t *testing.T) {
	store := newMockStore()
	store.episodeTotals["tt1234567"] = 10 // series has 10 episodes
	svc := newTestService(store)
	userID := uuid.NewV4()

	err := svc.MarkEpisodeWatched(context.Background(), userID, "tt1234567", 1, 1, models.UserVideoSourceAuto90pct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := store.episodes[episodeKey{videoID: "tt1234567", season: 1, episode: 1}]; !exists {
		t.Error("expected episode status to be upserted")
	}
	if _, exists := store.series["tt1234567"]; exists {
		t.Error("expected series NOT to be auto-marked when only 1/10 episodes watched")
	}
}

func TestMarkEpisodeWatched_AutoMarksSeriesWhenAllEpisodesComplete(t *testing.T) {
	store := newMockStore()
	store.episodeTotals["tt1234567"] = 3
	svc := newTestService(store)
	userID := uuid.NewV4()
	ctx := context.Background()

	// Mark first two episodes — series should NOT auto-mark yet
	if err := svc.MarkEpisodeWatched(ctx, userID, "tt1234567", 1, 1, models.UserVideoSourceAuto90pct); err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkEpisodeWatched(ctx, userID, "tt1234567", 1, 2, models.UserVideoSourceAuto90pct); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.series["tt1234567"]; exists {
		t.Fatal("expected series not marked after 2/3 episodes")
	}

	// Mark final episode — series should auto-mark now
	if err := svc.MarkEpisodeWatched(ctx, userID, "tt1234567", 1, 3, models.UserVideoSourceAuto90pct); err != nil {
		t.Fatal(err)
	}
	got := store.series["tt1234567"]
	if got == nil {
		t.Fatal("expected series to be auto-marked after all episodes watched")
	}
	if got.Source != models.UserVideoSourceAutoAllEpisodes {
		t.Errorf("expected source 'auto_all_episodes', got %s", got.Source)
	}
	if !got.Watched {
		t.Error("expected watched=true")
	}
}

func TestMarkEpisodeWatched_SkipsAutoMarkWhenNoEpisodeMetadata(t *testing.T) {
	store := newMockStore()
	// No episodeTotals entry → CountEpisodeMetadataByVideoID returns 0
	svc := newTestService(store)
	userID := uuid.NewV4()

	err := svc.MarkEpisodeWatched(context.Background(), userID, "tt9999999", 1, 1, models.UserVideoSourceAuto90pct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := store.series["tt9999999"]; exists {
		t.Error("expected series not to be auto-marked when no episode_metadata exists")
	}
}

func TestUnmarkEpisode_RemovesAutoSeriesRow(t *testing.T) {
	store := newMockStore()
	store.episodeTotals["tt1234567"] = 2
	svc := newTestService(store)
	userID := uuid.NewV4()
	ctx := context.Background()

	// Mark all episodes → series auto-marks
	_ = svc.MarkEpisodeWatched(ctx, userID, "tt1234567", 1, 1, models.UserVideoSourceAuto90pct)
	_ = svc.MarkEpisodeWatched(ctx, userID, "tt1234567", 1, 2, models.UserVideoSourceAuto90pct)
	if _, exists := store.series["tt1234567"]; !exists {
		t.Fatal("precondition: series should be auto-marked")
	}

	// Unmark one episode → auto series row should be removed
	if err := svc.UnmarkEpisode(ctx, userID, "tt1234567", 1, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := store.series["tt1234567"]; exists {
		t.Error("expected auto series row to be dropped after unmarking an episode")
	}
}

func TestUnmarkEpisode_PreservesManualSeriesRow(t *testing.T) {
	store := newMockStore()
	store.episodeTotals["tt1234567"] = 2
	svc := newTestService(store)
	userID := uuid.NewV4()
	ctx := context.Background()

	// User manually marks the whole series AND watches one episode
	_ = svc.MarkSeriesWatched(ctx, userID, "tt1234567", models.UserVideoSourceManual)
	_ = svc.MarkEpisodeWatched(ctx, userID, "tt1234567", 1, 1, models.UserVideoSourceAuto90pct)

	// Unmark that one episode — manual series row must survive
	if err := svc.UnmarkEpisode(ctx, userID, "tt1234567", 1, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := store.series["tt1234567"]
	if got == nil {
		t.Fatal("expected manual series row to be preserved")
	}
	if got.Source != models.UserVideoSourceManual {
		t.Errorf("expected source 'manual', got %s", got.Source)
	}
}

func TestMarkSeriesWatched(t *testing.T) {
	store := newMockStore()
	svc := newTestService(store)
	userID := uuid.NewV4()

	err := svc.MarkSeriesWatched(context.Background(), userID, "tt1234567", models.UserVideoSourceManual)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := store.series["tt1234567"]
	if got == nil {
		t.Fatal("expected series status to be upserted")
	}
	if got.Source != models.UserVideoSourceManual {
		t.Errorf("expected source 'manual', got %s", got.Source)
	}
}

func TestMarkSeriesWatched_DoesNotCreateEpisodeRows(t *testing.T) {
	store := newMockStore()
	svc := newTestService(store)
	userID := uuid.NewV4()

	_ = svc.MarkSeriesWatched(context.Background(), userID, "tt1234567", models.UserVideoSourceManual)
	if len(store.episodes) != 0 {
		t.Errorf("expected no episode rows to be created by MarkSeriesWatched, got %d", len(store.episodes))
	}
}

func TestUnmarkSeries(t *testing.T) {
	store := newMockStore()
	store.series["tt1234567"] = &models.SeriesStatus{VideoID: "tt1234567", Source: models.UserVideoSourceManual}
	store.episodes[episodeKey{videoID: "tt1234567", season: 1, episode: 1}] = &models.EpisodeStatus{
		VideoID: "tt1234567", Season: 1, Episode: 1, Watched: true,
	}
	svc := newTestService(store)

	if err := svc.UnmarkSeries(context.Background(), uuid.NewV4(), "tt1234567"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := store.series["tt1234567"]; exists {
		t.Error("expected series row to be deleted")
	}
	if len(store.episodes) != 1 {
		t.Error("expected episode rows to remain untouched")
	}
}
