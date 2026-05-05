package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// MovieWatchlist is a single "I want to watch this" entry, IMDB-keyed.
// Mirrors movie_status (see migration 44 / models/movie_status.go) but tracks
// intent instead of completion. There is no FK on movie_metadata: a user can
// bookmark an item before we've ever cached its metadata locally; the handler
// populates movie_metadata lazily on insert.
type MovieWatchlist struct {
	tableName struct{} `pg:"movie_watchlist"`

	UserID    uuid.UUID `pg:"user_id,pk"`
	VideoID   string    `pg:"video_id,pk"`
	Source    string    `pg:"source"`
	CreatedAt time.Time `pg:"created_at"`
}

// WatchlistItem is the read shape returned to the client. It joins the
// watchlist row with movie_metadata / series_metadata so the list can render
// without a second round-trip and stays compatible with the Cinemeta-shaped
// item the discover Preact app already understands (see ItemGrid.jsx — it
// reads id/name/poster/year/imdbRating/type).
type WatchlistItem struct {
	VideoID   string   `json:"video_id"   pg:"video_id"`
	Type      string   `json:"type"       pg:"type"`
	Title     string   `json:"title"      pg:"title"`
	Year      *int16   `json:"year"       pg:"year"`
	PosterURL string   `json:"poster_url" pg:"poster_url"`
	Rating    *float64 `json:"rating"     pg:"rating"`
	Source    string   `json:"source"     pg:"source"`
	CreatedAt int64    `json:"created_at" pg:"created_at"`
}

// AddToMovieWatchlist inserts (or no-ops on conflict) a movie watchlist row.
// Returns true on a fresh insert, false on conflict — the handler uses this
// to decide whether to count against the free-tier quota.
func AddToMovieWatchlist(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string, source string) (bool, error) {
	w := &MovieWatchlist{UserID: userID, VideoID: videoID, Source: source}
	res, err := db.Model(w).
		Context(ctx).
		OnConflict("(user_id, video_id) DO NOTHING").
		Insert()
	if err != nil {
		return false, errors.Wrap(err, "failed to add to movie watchlist")
	}
	return res.RowsAffected() > 0, nil
}

// RemoveFromMovieWatchlist deletes a single (user, video) pair. Idempotent —
// a missing row is not an error from the API's perspective.
func RemoveFromMovieWatchlist(ctx context.Context, db *pg.DB, userID uuid.UUID, videoID string) error {
	_, err := db.Model((*MovieWatchlist)(nil)).
		Context(ctx).
		Where("user_id = ? AND video_id = ?", userID, videoID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to remove from movie watchlist")
	}
	return nil
}

// CountMovieWatchlist returns how many movie entries the user has, used to
// enforce the free-tier soft cap on add.
func CountMovieWatchlist(ctx context.Context, db *pg.DB, userID uuid.UUID) (int, error) {
	n, err := db.Model((*MovieWatchlist)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Count()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count movie watchlist")
	}
	return n, nil
}

// ListMovieWatchlistVideoIDs returns just the IMDB ids in the user's
// watchlist, ordered newest-first. Used by the bulk-fetch path that hydrates
// the bookmark badges on every visible card without paying for metadata.
func ListMovieWatchlistVideoIDs(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]string, error) {
	var rows []*MovieWatchlist
	err := db.Model(&rows).
		Context(ctx).
		Column("video_id").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list movie watchlist ids")
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.VideoID
	}
	return out, nil
}

// FilterMovieWatchlistVideoIDs returns the subset of videoIDs that are
// already in the user's movie watchlist. Cheap PK-only lookup used by the
// AI recommender to skip already-bookmarked titles before showing them.
func FilterMovieWatchlistVideoIDs(ctx context.Context, db *pg.DB, userID uuid.UUID, videoIDs []string) ([]string, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
	var rows []*MovieWatchlist
	err := db.Model(&rows).
		Context(ctx).
		Column("video_id").
		Where("user_id = ?", userID).
		Where("video_id IN (?)", pg.In(videoIDs)).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to filter movie watchlist ids")
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.VideoID
	}
	return out, nil
}

// ListMovieWatchlistItems returns the user's watchlist with metadata joined,
// ready for the grid. Items whose metadata isn't (yet) in our cache are
// skipped — the client can't render a useful card without at least a title.
// In practice this is rare because the handler triggers enrichment on add.
func ListMovieWatchlistItems(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]WatchlistItem, error) {
	var out []WatchlistItem
	_, err := db.QueryContext(ctx, &out, `
		SELECT
			mw.video_id,
			'movie'::text  AS type,
			mmd.title,
			mmd.year,
			mmd.poster_url,
			mmd.rating,
			mw.source,
			extract(epoch FROM mw.created_at)::bigint AS created_at
		FROM movie_watchlist mw
		JOIN movie_metadata mmd ON mmd.video_id = mw.video_id
		WHERE mw.user_id = ?
		ORDER BY mw.created_at DESC
	`, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list movie watchlist items")
	}
	return out, nil
}
