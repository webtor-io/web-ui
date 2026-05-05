package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// SeriesWatchlist mirrors MovieWatchlist for series. See movie_watchlist.go
// for design notes.
type SeriesWatchlist struct {
	tableName struct{} `pg:"series_watchlist"`

	UserID    uuid.UUID `pg:"user_id,pk"`
	VideoID   string    `pg:"video_id,pk"`
	Source    string    `pg:"source"`
	CreatedAt time.Time `pg:"created_at"`
}

func AddToSeriesWatchlist(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, source string) (bool, error) {
	w := &SeriesWatchlist{UserID: userID, VideoID: videoID, Source: source}
	res, err := db.Model(w).
		Context(ctx).
		OnConflict("(user_id, video_id) DO NOTHING").
		Insert()
	if err != nil {
		return false, errors.Wrap(err, "failed to add to series watchlist")
	}
	return res.RowsAffected() > 0, nil
}

func RemoveFromSeriesWatchlist(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string) error {
	_, err := db.Model((*SeriesWatchlist)(nil)).
		Context(ctx).
		Where("user_id = ? AND video_id = ?", userID, videoID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to remove from series watchlist")
	}
	return nil
}

func CountSeriesWatchlist(ctx context.Context, db *pg.DB, userID uuid.UUID) (int, error) {
	n, err := db.Model((*SeriesWatchlist)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Count()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count series watchlist")
	}
	return n, nil
}

func ListSeriesWatchlistVideoIDs(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]string, error) {
	var rows []*SeriesWatchlist
	err := db.Model(&rows).
		Context(ctx).
		Column("video_id").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list series watchlist ids")
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.VideoID
	}
	return out, nil
}

// FilterSeriesWatchlistVideoIDs mirrors FilterMovieWatchlistVideoIDs for the
// series table.
func FilterSeriesWatchlistVideoIDs(ctx context.Context, db *pg.DB, userID uuid.UUID, videoIDs []string) ([]string, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
	var rows []*SeriesWatchlist
	err := db.Model(&rows).
		Context(ctx).
		Column("video_id").
		Where("user_id = ?", userID).
		Where("video_id IN (?)", pg.In(videoIDs)).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to filter series watchlist ids")
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.VideoID
	}
	return out, nil
}

func ListSeriesWatchlistItems(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]WatchlistItem, error) {
	var out []WatchlistItem
	_, err := db.QueryContext(ctx, &out, `
		SELECT
			sw.video_id,
			'series'::text AS type,
			smd.title,
			smd.year,
			smd.poster_url,
			smd.rating,
			sw.source,
			extract(epoch FROM sw.created_at)::bigint AS created_at
		FROM series_watchlist sw
		JOIN series_metadata smd ON smd.video_id = sw.video_id
		WHERE sw.user_id = ?
		ORDER BY sw.created_at DESC
	`, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list series watchlist items")
	}
	return out, nil
}
