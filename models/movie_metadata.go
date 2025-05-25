package models

import (
	"context"
	"errors"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"time"
)

type MovieMetadata struct {
	*VideoMetadata

	tableName struct{} `pg:"movie_metadata"`

	MovieMetadataID uuid.UUID `pg:"movie_metadata_id,pk,type:uuid,default:uuid_generate_v4()"`
	CreatedAt       time.Time `pg:"created_at,default:now()"`
	UpdatedAt       time.Time `pg:"updated_at,default:now()"`
}

func LinkMovieToMetadata(
	ctx context.Context,
	db *pg.DB,
	movieID uuid.UUID,
	metadataID uuid.UUID,
) error {
	_, err := db.Model(&Movie{}).
		Set("movie_metadata_id = ?", metadataID).
		Where("movie_id = ?", movieID).
		Context(ctx).
		Update()
	return err
}

func UpsertMovieMetadata(
	ctx context.Context,
	db *pg.DB,
	md *VideoMetadata,
) (uuid.UUID, error) {
	meta := &MovieMetadata{
		VideoMetadata: md,
	}

	_, err := db.Model(meta).
		Context(ctx).
		OnConflict("(video_id) DO UPDATE").
		Set(`
			title = EXCLUDED.title,
			year = EXCLUDED.year,
			plot = EXCLUDED.plot,
			poster_url = EXCLUDED.poster_url,
			rating = EXCLUDED.rating
		`).
		Returning("movie_metadata_id").
		Insert()
	if err != nil {
		return uuid.NewV4(), err
	}

	return meta.MovieMetadataID, nil
}

func GetMovieMetadataByVideoID(
	ctx context.Context,
	db *pg.DB,
	imdbID string,
) (*MovieMetadata, error) {
	var meta MovieMetadata

	err := db.Model(&meta).
		Context(ctx).
		Where("video_id = ?", imdbID).
		Limit(1).
		Select()

	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &meta, nil
}
