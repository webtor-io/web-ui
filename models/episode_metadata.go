package models

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

type EpisodeMetadata struct {
	tableName struct{} `pg:"episode_metadata"`

	EpisodeMetadataID uuid.UUID  `pg:"episode_metadata_id,pk,type:uuid,default:uuid_generate_v4()"`
	VideoID           string     `pg:"video_id,notnull"`
	Season            int16      `pg:"season,notnull"`
	Episode           int16      `pg:"episode,notnull"`
	Title             *string    `pg:"title"`
	Plot              *string    `pg:"plot"`
	StillURL          *string    `pg:"still_url"`
	AirDate           *time.Time `pg:"air_date"`
	Rating            *float64   `pg:"rating"`
	CreatedAt         time.Time  `pg:"created_at,default:now()"`
	UpdatedAt         time.Time  `pg:"updated_at,default:now()"`
}

func UpsertEpisodeMetadata(
	ctx context.Context,
	db *pg.DB,
	md *EpisodeMetadata,
) (uuid.UUID, error) {
	_, err := db.Model(md).
		Context(ctx).
		OnConflict("(video_id, season, episode) DO UPDATE").
		Set(`
			title = EXCLUDED.title,
			plot = EXCLUDED.plot,
			still_url = EXCLUDED.still_url,
			air_date = EXCLUDED.air_date,
			rating = EXCLUDED.rating
		`).
		Returning("episode_metadata_id").
		Insert()
	if err != nil {
		return uuid.Nil, err
	}
	return md.EpisodeMetadataID, nil
}

func LinkEpisodeToMetadata(
	ctx context.Context,
	db *pg.DB,
	episodeID uuid.UUID,
	metadataID uuid.UUID,
) error {
	_, err := db.Model(&Episode{}).
		Set("episode_metadata_id = ?", metadataID).
		Where("episode_id = ?", episodeID).
		Context(ctx).
		Update()
	return err
}

func GetEpisodeMetadata(
	ctx context.Context,
	db *pg.DB,
	videoID string,
	season int16,
	episode int16,
) (*EpisodeMetadata, error) {
	var meta EpisodeMetadata

	err := db.Model(&meta).
		Context(ctx).
		Where("video_id = ?", videoID).
		Where("season = ?", season).
		Where("episode = ?", episode).
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
