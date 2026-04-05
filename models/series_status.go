package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type SeriesStatus struct {
	tableName struct{} `pg:"series_status"`

	UserID    uuid.UUID  `pg:"user_id,pk"`
	VideoID   string     `pg:"video_id,pk"`
	Watched   bool       `pg:"watched,use_zero"`
	Rating    *int16     `pg:"rating"`
	Source    string     `pg:"source"`
	WatchedAt *time.Time `pg:"watched_at"`
	CreatedAt time.Time  `pg:"created_at"`
	UpdatedAt time.Time  `pg:"updated_at"`
}

func UpsertSeriesStatus(ctx context.Context, db *pg.DB, s *SeriesStatus) error {
	_, err := db.Model(s).
		Context(ctx).
		OnConflict("(user_id, video_id) DO UPDATE").
		Set("watched = EXCLUDED.watched").
		Set("rating = COALESCE(EXCLUDED.rating, series_status.rating)").
		Set("source = EXCLUDED.source").
		Set("watched_at = EXCLUDED.watched_at").
		Insert()
	if err != nil {
		return errors.Wrap(err, "failed to upsert series status")
	}
	return nil
}

func GetSeriesStatus(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string) (*SeriesStatus, error) {
	var s SeriesStatus
	err := db.Model(&s).
		Context(ctx).
		Where("user_id = ? AND video_id = ?", userID, videoID).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get series status")
	}
	return &s, nil
}

func DeleteSeriesStatus(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string) error {
	_, err := db.Model((*SeriesStatus)(nil)).
		Context(ctx).
		Where("user_id = ? AND video_id = ?", userID, videoID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete series status")
	}
	return nil
}

// DeleteSeriesStatusBySource deletes a series-level row only if its source
// matches. Used by the service to drop an auto_all_episodes row when an episode
// is unmarked, without clobbering a manual declaration.
func DeleteSeriesStatusBySource(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, source string) error {
	_, err := db.Model((*SeriesStatus)(nil)).
		Context(ctx).
		Where("user_id = ? AND video_id = ? AND source = ?", userID, videoID, source).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete series status by source")
	}
	return nil
}

// FilterWatchedSeriesIDs is the series counterpart to FilterWatchedMovieIDs.
// A series is "watched" only when the user has a series-level row (manual
// declaration or auto_all_episodes derivation). Partial per-episode progress
// is not surfaced here — discover's marker means "the title is done".
func FilterWatchedSeriesIDs(ctx context.Context, db *pg.DB, userID uuid.UUID, videoIDs []string) ([]string, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
	var list []*SeriesStatus
	err := db.Model(&list).
		Context(ctx).
		Column("video_id").
		Where("user_id = ?", userID).
		Where("video_id IN (?)", pg.In(videoIDs)).
		Where("watched = true").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to filter watched series ids")
	}
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.VideoID
	}
	return out, nil
}

// GetSeriesStatusMap returns a map of video_id -> *SeriesStatus for
// bulk prefetch in library listings.
func GetSeriesStatusMap(ctx context.Context, db *pg.DB, userID uuid.UUID, videoIDs []string) (map[string]*SeriesStatus, error) {
	result := make(map[string]*SeriesStatus, len(videoIDs))
	if len(videoIDs) == 0 {
		return result, nil
	}
	var list []*SeriesStatus
	err := db.Model(&list).
		Context(ctx).
		Where("user_id = ?", userID).
		Where("video_id IN (?)", pg.In(videoIDs)).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list series status")
	}
	for _, s := range list {
		result[s.VideoID] = s
	}
	return result, nil
}
