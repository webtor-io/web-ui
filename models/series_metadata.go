package models

import (
	"context"
	"errors"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"time"
)

type SeriesMetadata struct {
	*VideoMetadata

	tableName struct{} `pg:"series_metadata"`

	SeriesMetadataID uuid.UUID `pg:"series_metadata_id,pk,type:uuid,default:uuid_generate_v4()"`
	CreatedAt        time.Time `pg:"created_at,default:now()"`
	UpdatedAt        time.Time `pg:"updated_at,default:now()"`
}

func LinkSeriesToMetadata(
	ctx context.Context,
	db *pg.DB,
	seriesID uuid.UUID,
	metadataID uuid.UUID,
) error {
	_, err := db.Model(&Series{}).
		Set("series_metadata_id = ?", metadataID).
		Where("series_id = ?", seriesID).
		Context(ctx).
		Update()
	return err
}

func UpsertSeriesMetadata(
	ctx context.Context,
	db *pg.DB,
	md *VideoMetadata,
) (uuid.UUID, error) {
	meta := &SeriesMetadata{
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
		Returning("series_metadata_id").
		Insert()
	if err != nil {
		return uuid.NewV4(), err
	}

	return meta.SeriesMetadataID, nil
}

func GetSeriesMetadataByVideoID(
	ctx context.Context,
	db *pg.DB,
	videoID string,
) (*SeriesMetadata, error) {
	var meta SeriesMetadata

	err := db.Model(&meta).
		Context(ctx).
		Where("video_id = ?", videoID).
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
