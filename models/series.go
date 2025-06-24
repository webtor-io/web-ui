package models

import (
	"context"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"time"

	"github.com/satori/go.uuid"
)

type Series struct {
	*VideoContent
	tableName struct{} `pg:"series"`

	SeriesID         uuid.UUID  `pg:"series_id,pk,type:uuid,default:uuid_generate_v4()"`
	SeriesMetadataID *uuid.UUID `pg:"series_metadata_id"`
	CreatedAt        time.Time  `pg:"created_at,default:now()"`
	UpdatedAt        time.Time  `pg:"updated_at,default:now()"`

	Episodes       []*Episode      `pg:"rel:has-many,fk:series_id"`
	MediaInfo      *MediaInfo      `pg:"rel:has-one,fk:resource_id"`
	SeriesMetadata *SeriesMetadata `pg:"rel:has-one,fk:series_metadata_id"`
	LibraryItems   []*Library      `pg:"rel:has-many,fk:library_id,join_fk:resource_id"`
}

func (s *Series) GetMetadata() *VideoMetadata {
	if s.SeriesMetadata == nil {
		return nil
	}
	return s.SeriesMetadata.VideoMetadata
}

func (s *Series) GetContent() *VideoContent {
	return s.VideoContent
}

func (s *Series) GetContentType() ContentType {
	return ContentTypeSeries
}

func (s *Series) GetID() uuid.UUID {
	return s.SeriesID
}

func (s *Series) GetPath() *string {
	return nil
}

func (s *Series) GetEpisode(season int, episode int) *Episode {
	for _, e := range s.Episodes {
		var se int16
		if e.Season != nil {
			se = *e.Season
		}
		var ep int16
		if e.Episode != nil {
			ep = *e.Episode
		}
		if int(se) == season && int(ep) == episode {
			return e
		}
	}
	return nil
}

func (s *Series) GetIntYear() int {
	if s.Year == nil {
		return 0
	}
	return int(*s.Year)
}

func ReplaceSeriesForResource(ctx context.Context, db *pg.DB, resourceID string, seriesList []*Series) error {
	tx, err := db.BeginContext(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Close()
	}()

	// Delete series (episodes will cascade)
	_, err = tx.Model((*Series)(nil)).
		Where("resource_id = ?", resourceID).
		Context(ctx).
		Delete()
	if err != nil {
		return err
	}

	for _, series := range seriesList {
		// Insert series
		_, err := tx.Model(series).
			Context(ctx).
			Insert()
		if err != nil {
			return err
		}

		// Insert episodes
		_, err = tx.Model(&series.Episodes).
			Context(ctx).
			Insert()
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func GetSeriesByResourceID(ctx context.Context, db *pg.DB, resourceID string) ([]*Series, error) {
	var series []*Series

	err := db.Model(&series).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Select()

	if err != nil {
		return nil, err
	}

	return series, nil
}

func GetSeriesByID(ctx context.Context, db *pg.DB, uID uuid.UUID, seriesID string) (*Series, error) {
	var s Series

	query := db.Model(&s).
		Context(ctx).
		Join("join library as l").
		JoinOn("series.resource_id = l.resource_id").
		Where("series.series_id = ?", seriesID).
		Where("l.user_id = ?", uID).
		Relation("Episodes").
		Limit(1)

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch series")
	}

	return &s, nil
}

func GetSeriesByVideoID(ctx context.Context, db *pg.DB, uID uuid.UUID, videoID string) ([]*Series, error) {
	var list []*Series

	query := db.Model(&list).
		Context(ctx).
		Join("left join series_metadata as smd").
		JoinOn("series.series_metadata_id = smd.series_metadata_id").
		Join("join library as l").
		JoinOn("series.resource_id = l.resource_id").
		Where("l.user_id = ?", uID).
		Where("smd.video_id = ?", videoID).
		Relation("SeriesMetadata").
		Relation("Episodes")

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch series list")
	}

	return list, nil
}

func GetSeriesWithEpisodes(ctx context.Context, db *pg.DB, sID uuid.UUID) (*Series, error) {
	var s Series
	err := db.Model(&s).
		Context(ctx).
		Where("series.series_id = ?", sID).
		Relation("Episodes").
		Select()
	if err != nil {
		return nil, err
	}
	return &s, nil
}
