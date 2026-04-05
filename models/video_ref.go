package models

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
)

// VideoRefKind tags a VideoRef as either a movie or a series episode.
type VideoRefKind string

const (
	VideoRefKindMovie   VideoRefKind = "movie"
	VideoRefKindEpisode VideoRefKind = "episode"
)

// VideoRef identifies the IMDB-level video (movie or series episode) that
// corresponds to a specific file inside a torrent. Season and Episode are
// only populated when Kind == VideoRefKindEpisode.
type VideoRef struct {
	Kind    VideoRefKind
	VideoID string
	Season  int16
	Episode int16
}

// ResolveVideoFromResourcePath finds the IMDB video_id (and season/episode
// for series) for the given (resource_id, path). It tries movies first,
// then episodes. Returns (nil, nil) if enrichment has not yet resolved a
// video_id for this file — callers treat that as "skip, nothing to record
// against the IMDB profile".
func ResolveVideoFromResourcePath(ctx context.Context, db *pg.DB, resourceID string, path string) (*VideoRef, error) {
	// Try movie first. A single-file torrent has a movie row whose path may
	// be NULL (movie IS the whole resource); multi-file packs set path.
	var movie struct {
		VideoID string `pg:"video_id"`
	}
	_, err := db.QueryOneContext(ctx, &movie, `
		SELECT mm.video_id
		FROM movie m
		JOIN movie_metadata mm ON mm.movie_metadata_id = m.movie_metadata_id
		WHERE m.resource_id = ?
			AND mm.video_id IS NOT NULL
			AND (m.path = ? OR m.path IS NULL)
		ORDER BY (m.path = ?) DESC
		LIMIT 1
	`, resourceID, path, path)
	if err == nil && movie.VideoID != "" {
		return &VideoRef{Kind: VideoRefKindMovie, VideoID: movie.VideoID}, nil
	}
	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to resolve movie video_id")
	}

	// Try episode.
	var ep struct {
		VideoID string `pg:"video_id"`
		Season  int16  `pg:"season"`
		Episode int16  `pg:"episode"`
	}
	_, err = db.QueryOneContext(ctx, &ep, `
		SELECT sm.video_id, e.season, e.episode
		FROM episode e
		JOIN series s ON s.series_id = e.series_id
		JOIN series_metadata sm ON sm.series_metadata_id = s.series_metadata_id
		WHERE e.resource_id = ?
			AND e.path = ?
			AND sm.video_id IS NOT NULL
			AND e.season IS NOT NULL
			AND e.episode IS NOT NULL
		LIMIT 1
	`, resourceID, path)
	if err == nil && ep.VideoID != "" {
		return &VideoRef{
			Kind:    VideoRefKindEpisode,
			VideoID: ep.VideoID,
			Season:  ep.Season,
			Episode: ep.Episode,
		}, nil
	}
	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to resolve episode video_id")
	}

	return nil, nil
}
