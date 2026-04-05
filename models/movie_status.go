package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// Shared source constants for movie_status / series_status / episode_status
// tables. The `source` column records how the row was created, which becomes
// useful as a signal weight for the future recommendation engine (manual
// declaration is a stronger signal than auto_90pct, etc.).
const (
	UserVideoSourceManual          = "manual"
	UserVideoSourceAuto90pct       = "auto_90pct"
	UserVideoSourceAutoAllEpisodes = "auto_all_episodes"
)

type MovieStatus struct {
	tableName struct{} `pg:"movie_status"`

	UserID    uuid.UUID  `pg:"user_id,pk"`
	VideoID   string     `pg:"video_id,pk"`
	Watched   bool       `pg:"watched,use_zero"`
	Rating    *int16     `pg:"rating"`
	Source    string     `pg:"source"`
	WatchedAt *time.Time `pg:"watched_at"`
	CreatedAt time.Time  `pg:"created_at"`
	UpdatedAt time.Time  `pg:"updated_at"`
}

func UpsertMovieStatus(ctx context.Context, db *pg.DB, s *MovieStatus) error {
	_, err := db.Model(s).
		Context(ctx).
		OnConflict("(user_id, video_id) DO UPDATE").
		Set("watched = EXCLUDED.watched").
		Set("rating = COALESCE(EXCLUDED.rating, movie_status.rating)").
		Set("source = EXCLUDED.source").
		Set("watched_at = EXCLUDED.watched_at").
		Insert()
	if err != nil {
		return errors.Wrap(err, "failed to upsert movie status")
	}
	return nil
}

func GetMovieStatus(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string) (*MovieStatus, error) {
	var s MovieStatus
	err := db.Model(&s).
		Context(ctx).
		Where("user_id = ? AND video_id = ?", userID, videoID).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get movie status")
	}
	return &s, nil
}

func DeleteMovieStatus(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string) error {
	_, err := db.Model((*MovieStatus)(nil)).
		Context(ctx).
		Where("user_id = ? AND video_id = ?", userID, videoID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete movie status")
	}
	return nil
}

// FilterWatchedMovieIDs takes a candidate list of IMDB ids and returns the
// subset that the given user has marked as watched. Used by the
// /library/watched/ids endpoint that drives the discover-page watched marker
// — the client sends the ids of visible items, the server returns which of
// them are already watched. Returns an empty slice for empty/no-match input.
func FilterWatchedMovieIDs(ctx context.Context, db *pg.DB, userID uuid.UUID, videoIDs []string) ([]string, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
	var list []*MovieStatus
	err := db.Model(&list).
		Context(ctx).
		Column("video_id").
		Where("user_id = ?", userID).
		Where("video_id IN (?)", pg.In(videoIDs)).
		Where("watched = true").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to filter watched movie ids")
	}
	out := make([]string, len(list))
	for i, m := range list {
		out[i] = m.VideoID
	}
	return out, nil
}

// GetMovieStatusMap returns a map of video_id -> *MovieStatus for the
// given user, filtered to the requested video IDs. Used for bulk prefetch in
// library listings to avoid N+1 queries.
func GetMovieStatusMap(ctx context.Context, db *pg.DB, userID uuid.UUID, videoIDs []string) (map[string]*MovieStatus, error) {
	result := make(map[string]*MovieStatus, len(videoIDs))
	if len(videoIDs) == 0 {
		return result, nil
	}
	var list []*MovieStatus
	err := db.Model(&list).
		Context(ctx).
		Where("user_id = ?", userID).
		Where("video_id IN (?)", pg.In(videoIDs)).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list movie status")
	}
	for _, s := range list {
		result[s.VideoID] = s
	}
	return result, nil
}
