package models

import (
	"context"
	"fmt"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type EpisodeStatus struct {
	tableName struct{} `pg:"episode_status"`

	UserID    uuid.UUID       `pg:"user_id,pk"`
	VideoID   string          `pg:"video_id,pk"`
	Season    int16           `pg:"season,pk"`
	Episode   int16           `pg:"episode,pk"`
	Watched   bool            `pg:"watched,use_zero"`
	Rating    *int16          `pg:"rating"`
	Source    UserVideoSource `pg:"source"`
	WatchedAt *time.Time      `pg:"watched_at"`
	CreatedAt time.Time       `pg:"created_at"`
	UpdatedAt time.Time       `pg:"updated_at"`
}

// EpisodeKey is a compact map key for (season, episode) lookups in templates.
type EpisodeKey struct {
	Season  int16
	Episode int16
}

func (k EpisodeKey) String() string {
	return fmt.Sprintf("s%de%d", k.Season, k.Episode)
}

func UpsertEpisodeStatus(ctx context.Context, db *pg.DB, s *EpisodeStatus) error {
	_, err := db.Model(s).
		Context(ctx).
		OnConflict("(user_id, video_id, season, episode) DO UPDATE").
		Set("watched = EXCLUDED.watched").
		Set("rating = COALESCE(EXCLUDED.rating, episode_status.rating)").
		Set("source = EXCLUDED.source").
		Set("watched_at = EXCLUDED.watched_at").
		Insert()
	if err != nil {
		return errors.Wrap(err, "failed to upsert episode status")
	}
	return nil
}

func GetEpisodeStatus(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, season, episode int16) (*EpisodeStatus, error) {
	var s EpisodeStatus
	err := db.Model(&s).
		Context(ctx).
		Where("user_id = ? AND video_id = ? AND season = ? AND episode = ?", userID, videoID, season, episode).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get episode status")
	}
	return &s, nil
}

func DeleteEpisodeStatus(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, season, episode int16) error {
	_, err := db.Model((*EpisodeStatus)(nil)).
		Context(ctx).
		Where("user_id = ? AND video_id = ? AND season = ? AND episode = ?", userID, videoID, season, episode).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete episode status")
	}
	return nil
}

// CountWatchedEpisodes returns the number of episodes marked as watched for a
// given user and series video_id. Used by the service to drive the
// all-episodes-watched auto-mark rule.
func CountWatchedEpisodes(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string) (int, error) {
	n, err := db.Model((*EpisodeStatus)(nil)).
		Context(ctx).
		Where("user_id = ? AND video_id = ? AND watched = true", userID, videoID).
		Count()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count watched episodes")
	}
	return n, nil
}

// GetEpisodeStatusMapForSeries returns a map keyed on (season, episode)
// for a single series. Used on the resource page to render per-episode state.
func GetEpisodeStatusMapForSeries(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string) (map[EpisodeKey]*EpisodeStatus, error) {
	var list []*EpisodeStatus
	err := db.Model(&list).
		Context(ctx).
		Where("user_id = ? AND video_id = ?", userID, videoID).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list episode status for series")
	}
	result := make(map[EpisodeKey]*EpisodeStatus, len(list))
	for _, s := range list {
		result[EpisodeKey{Season: s.Season, Episode: s.Episode}] = s
	}
	return result, nil
}
