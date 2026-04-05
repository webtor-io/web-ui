package user_video_status

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// Service manages per-user "watched / want to watch / rated" state tied to
// IMDB video_id (not to torrent resource_id), across three tables:
// movie_status, series_status, episode_status.
//
// Cross-torrent propagation is automatic because the key is video_id: a user
// who watched S01E01 on torrent A will see it marked when opening torrent B
// of the same series (different dub/quality), provided enrichment resolved
// both torrents to the same video_id.
//
// Watch position / resume lives in watch_history and is intentionally
// torrent-scoped; this service does not touch it.
type Service struct {
	store userVideoStatusStore
}

func New(db *pg.DB) *Service {
	return &Service{
		store: &pgUserVideoStatusStore{db: db},
	}
}

// MarkMovieWatched marks a movie as watched for a user. `source` should be
// one of models.UserVideoSourceManual or models.UserVideoSourceAuto90pct.
// Also propagates watched=true into watch_history for every matching file
// across all of the user's torrents of this movie, so the continue-watching
// ribbon picks up the declared intent.
func (s *Service) MarkMovieWatched(ctx context.Context, userID uuid.UUID, videoID string, source string) error {
	if videoID == "" {
		return errors.New("videoID is required")
	}
	now := time.Now()
	if err := s.store.UpsertMovieStatus(ctx, &models.MovieStatus{
		UserID:    userID,
		VideoID:   videoID,
		Watched:   true,
		Source:    source,
		WatchedAt: &now,
	}); err != nil {
		return err
	}
	return s.store.SetWatchHistoryWatchedForMovie(ctx, userID, videoID, true)
}

// UnmarkMovie removes the user's status row for the given movie and clears
// watch_history.watched for matching files so the ribbon surfaces the movie
// again if the user has non-trivial playback progress on it.
func (s *Service) UnmarkMovie(ctx context.Context, userID uuid.UUID, videoID string) error {
	if err := s.store.DeleteMovieStatus(ctx, userID, videoID); err != nil {
		return err
	}
	return s.store.SetWatchHistoryWatchedForMovie(ctx, userID, videoID, false)
}

// MarkEpisodeWatched marks a specific episode as watched and then, if the
// count of watched episodes equals the total known from episode_metadata
// (and that total is > 0), auto-marks the parent series with
// source='auto_all_episodes'. This is the "all episodes watched" rule —
// deliberately correct for ongoing series, where the series-level row will
// fall out of sync naturally once new episodes are enriched.
func (s *Service) MarkEpisodeWatched(ctx context.Context, userID uuid.UUID, videoID string, season, episode int16, source string) error {
	if videoID == "" {
		return errors.New("videoID is required")
	}
	now := time.Now()
	err := s.store.UpsertEpisodeStatus(ctx, &models.EpisodeStatus{
		UserID:    userID,
		VideoID:   videoID,
		Season:    season,
		Episode:   episode,
		Watched:   true,
		Source:    source,
		WatchedAt: &now,
	})
	if err != nil {
		return errors.Wrap(err, "failed to upsert episode status")
	}
	if err := s.store.SetWatchHistoryWatchedForEpisode(ctx, userID, videoID, season, episode, true); err != nil {
		return err
	}
	return s.checkAndMarkSeriesComplete(ctx, userID, videoID, now)
}

// UnmarkEpisode removes an episode status row. If an auto-marked series-level
// row (source='auto_all_episodes') exists for the same series, it is removed
// too, since the completion condition no longer holds. A manual series-level
// declaration is preserved — it represents explicit user intent. The matching
// watch_history rows are also reset so the ribbon can surface the episode
// again once the user scrubs below 90% or otherwise re-engages.
func (s *Service) UnmarkEpisode(ctx context.Context, userID uuid.UUID, videoID string, season, episode int16) error {
	err := s.store.DeleteEpisodeStatus(ctx, userID, videoID, season, episode)
	if err != nil {
		return errors.Wrap(err, "failed to delete episode status")
	}
	if err := s.store.SetWatchHistoryWatchedForEpisode(ctx, userID, videoID, season, episode, false); err != nil {
		return err
	}
	return s.store.DeleteSeriesStatusBySource(ctx, userID, videoID, models.UserVideoSourceAutoAllEpisodes)
}

// MarkSeriesWatched declares a whole series as watched. Per-episode rows are
// intentionally NOT created as a side effect — series-level and episode-level
// rows are independent so that unmarking the series later does not destroy
// actual per-episode history. Templates are expected to display "all episodes
// watched" implicitly whenever a series-level row exists.
func (s *Service) MarkSeriesWatched(ctx context.Context, userID uuid.UUID, videoID string, source string) error {
	if videoID == "" {
		return errors.New("videoID is required")
	}
	now := time.Now()
	return s.store.UpsertSeriesStatus(ctx, &models.SeriesStatus{
		UserID:    userID,
		VideoID:   videoID,
		Watched:   true,
		Source:    source,
		WatchedAt: &now,
	})
}

// UnmarkSeries removes only the series-level row; per-episode rows remain.
func (s *Service) UnmarkSeries(ctx context.Context, userID uuid.UUID, videoID string) error {
	return s.store.DeleteSeriesStatus(ctx, userID, videoID)
}

// FilterWatchedIDs returns the subset of the given IMDB ids that are marked
// watched for the user, merging hits from movie_status and series_status.
// Single pass through both tables; order of the returned slice is unspecified.
// Used by the discover watched-marker endpoint.
func (s *Service) FilterWatchedIDs(ctx context.Context, userID uuid.UUID, videoIDs []string) ([]string, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
	movies, err := s.store.FilterWatchedMovieIDs(ctx, userID, videoIDs)
	if err != nil {
		return nil, err
	}
	series, err := s.store.FilterWatchedSeriesIDs(ctx, userID, videoIDs)
	if err != nil {
		return nil, err
	}
	if len(movies) == 0 && len(series) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(movies)+len(series))
	out = append(out, movies...)
	out = append(out, series...)
	return out, nil
}

// checkAndMarkSeriesComplete inspects the per-episode watched count for a
// series and inserts an auto_all_episodes series-level row if the user has
// watched every known episode. Called internally after MarkEpisodeWatched.
func (s *Service) checkAndMarkSeriesComplete(ctx context.Context, userID uuid.UUID, videoID string, watchedAt time.Time) error {
	total, err := s.store.CountEpisodeMetadataByVideoID(ctx, videoID)
	if err != nil {
		return errors.Wrap(err, "failed to count episode metadata")
	}
	if total == 0 {
		return nil
	}
	done, err := s.store.CountWatchedEpisodes(ctx, userID, videoID)
	if err != nil {
		return errors.Wrap(err, "failed to count watched episodes")
	}
	if done < total {
		return nil
	}
	return s.store.UpsertSeriesStatus(ctx, &models.SeriesStatus{
		UserID:    userID,
		VideoID:   videoID,
		Watched:   true,
		Source:    models.UserVideoSourceAutoAllEpisodes,
		WatchedAt: &watchedAt,
	})
}
