package models

import (
	"context"
	"github.com/go-pg/pg/v10"
	"time"

	"github.com/satori/go.uuid"
)

type Series struct {
	*VideoContent
	tableName struct{} `pg:"series"`

	SeriesID         uuid.UUID  `pg:"series_id,pk,type:uuid,default:uuid_generate_v4()"`
	ResourceID       string     `pg:"resource_id"`
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
		if series == nil {
			continue
		}

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
