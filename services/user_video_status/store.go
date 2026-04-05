package user_video_status

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// userVideoStatusStore abstracts DB operations for the service so that unit
// tests can substitute a mock without touching the database. Production code
// uses pgUserVideoStatusStore which delegates to package-level functions in
// the models package.
type userVideoStatusStore interface {
	// Movies
	UpsertMovieStatus(ctx context.Context, s *models.MovieStatus) error
	DeleteMovieStatus(ctx context.Context, userID uuid.UUID, videoID string) error

	// Series
	UpsertSeriesStatus(ctx context.Context, s *models.SeriesStatus) error
	DeleteSeriesStatus(ctx context.Context, userID uuid.UUID, videoID string) error
	DeleteSeriesStatusBySource(ctx context.Context, userID uuid.UUID, videoID string, source string) error

	// Episodes
	UpsertEpisodeStatus(ctx context.Context, s *models.EpisodeStatus) error
	DeleteEpisodeStatus(ctx context.Context, userID uuid.UUID, videoID string, season, episode int16) error
	CountWatchedEpisodes(ctx context.Context, userID uuid.UUID, videoID string) (int, error)

	// Episode metadata (totals for completion check)
	CountEpisodeMetadataByVideoID(ctx context.Context, videoID string) (int, error)

	// watch_history sync: when a user manually marks/unmarks an IMDB-level
	// item, the corresponding per-file watch_history.watched flag is flipped
	// too. This keeps the continue-watching ribbon consistent with the
	// user's declared intent (a file marked watched shouldn't still show as
	// in-progress just because position < 90%).
	SetWatchHistoryWatchedForMovie(ctx context.Context, userID uuid.UUID, videoID string, watched bool) error
	SetWatchHistoryWatchedForEpisode(ctx context.Context, userID uuid.UUID, videoID string, season, episode int16, watched bool) error

	// Filtering — used by the /library/watched/ids JSON endpoint that
	// drives the discover page marker. Client sends a candidate list of
	// IMDB ids (the currently visible catalog / search page), server
	// returns the subset that this user has actually marked watched.
	FilterWatchedMovieIDs(ctx context.Context, userID uuid.UUID, videoIDs []string) ([]string, error)
	FilterWatchedSeriesIDs(ctx context.Context, userID uuid.UUID, videoIDs []string) ([]string, error)
}

type pgUserVideoStatusStore struct {
	db *pg.DB
}

func (s *pgUserVideoStatusStore) UpsertMovieStatus(ctx context.Context, m *models.MovieStatus) error {
	return models.UpsertMovieStatus(ctx, s.db, m)
}

func (s *pgUserVideoStatusStore) DeleteMovieStatus(ctx context.Context, userID uuid.UUID, videoID string) error {
	return models.DeleteMovieStatus(ctx, s.db, userID, videoID)
}

func (s *pgUserVideoStatusStore) UpsertSeriesStatus(ctx context.Context, m *models.SeriesStatus) error {
	return models.UpsertSeriesStatus(ctx, s.db, m)
}

func (s *pgUserVideoStatusStore) DeleteSeriesStatus(ctx context.Context, userID uuid.UUID, videoID string) error {
	return models.DeleteSeriesStatus(ctx, s.db, userID, videoID)
}

func (s *pgUserVideoStatusStore) DeleteSeriesStatusBySource(ctx context.Context, userID uuid.UUID, videoID string, source string) error {
	return models.DeleteSeriesStatusBySource(ctx, s.db, userID, videoID, source)
}

func (s *pgUserVideoStatusStore) UpsertEpisodeStatus(ctx context.Context, m *models.EpisodeStatus) error {
	return models.UpsertEpisodeStatus(ctx, s.db, m)
}

func (s *pgUserVideoStatusStore) DeleteEpisodeStatus(ctx context.Context, userID uuid.UUID, videoID string, season, episode int16) error {
	return models.DeleteEpisodeStatus(ctx, s.db, userID, videoID, season, episode)
}

func (s *pgUserVideoStatusStore) CountWatchedEpisodes(ctx context.Context, userID uuid.UUID, videoID string) (int, error) {
	return models.CountWatchedEpisodes(ctx, s.db, userID, videoID)
}

func (s *pgUserVideoStatusStore) CountEpisodeMetadataByVideoID(ctx context.Context, videoID string) (int, error) {
	return models.CountEpisodeMetadataByVideoID(ctx, s.db, videoID)
}

func (s *pgUserVideoStatusStore) SetWatchHistoryWatchedForMovie(ctx context.Context, userID uuid.UUID, videoID string, watched bool) error {
	return models.SetWatchedForMovie(ctx, s.db, userID, videoID, watched)
}

func (s *pgUserVideoStatusStore) SetWatchHistoryWatchedForEpisode(ctx context.Context, userID uuid.UUID, videoID string, season, episode int16, watched bool) error {
	return models.SetWatchedForEpisode(ctx, s.db, userID, videoID, season, episode, watched)
}

func (s *pgUserVideoStatusStore) FilterWatchedMovieIDs(ctx context.Context, userID uuid.UUID, videoIDs []string) ([]string, error) {
	return models.FilterWatchedMovieIDs(ctx, s.db, userID, videoIDs)
}

func (s *pgUserVideoStatusStore) FilterWatchedSeriesIDs(ctx context.Context, userID uuid.UUID, videoIDs []string) ([]string, error) {
	return models.FilterWatchedSeriesIDs(ctx, s.db, userID, videoIDs)
}
