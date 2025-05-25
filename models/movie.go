package models

import (
	"context"
	"github.com/go-pg/pg/v10"
	"time"

	"github.com/satori/go.uuid"
)

type Movie struct {
	*VideoContent

	tableName struct{} `pg:"movie"`

	MovieID         uuid.UUID  `pg:"movie_id,pk,type:uuid,default:uuid_generate_v4()"`
	ResourceID      string     `pg:"resource_id"`
	MovieMetadataID *uuid.UUID `pg:"movie_metadata_id"`
	Path            *string    `pg:"path"`
	CreatedAt       time.Time  `pg:"created_at,default:now()"`
	UpdatedAt       time.Time  `pg:"updated_at,default:now()"`

	MediaInfo     *MediaInfo     `pg:"rel:has-one,fk:resource_id"`
	MovieMetadata *MovieMetadata `pg:"rel:has-one,fk:movie_metadata_id"`
	LibraryItems  []*Library     `pg:"rel:has-many,fk:library_id,join_fk:resource_id"`
}

func (s *Movie) GetMetadata() *VideoMetadata {
	if s.MovieMetadata == nil {
		return nil
	}
	return s.MovieMetadata.VideoMetadata
}

func (s *Movie) GetContent() *VideoContent {
	return s.VideoContent
}

func (s *Movie) GetContentType() ContentType {
	return ContentTypeMovie
}

func (s *Movie) GetIntYear() int {
	if s.Year == nil {
		return 0
	}
	return int(*s.Year)
}

func ReplaceMoviesForResource(ctx context.Context, db *pg.DB, resourceID string, movies []*Movie) error {
	tx, err := db.BeginContext(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Close()
	}()

	// Delete existing movies for the resource
	_, err = tx.Model((*Movie)(nil)).
		Where("resource_id = ?", resourceID).
		Context(ctx).
		Delete()
	if err != nil {
		return err
	}

	// Insert new movies if any
	if len(movies) > 0 {
		_, err = tx.Model(&movies).
			Context(ctx).
			Insert()
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func GetMoviesByResourceID(ctx context.Context, db *pg.DB, resourceID string) ([]*Movie, error) {
	var movies []*Movie

	err := db.Model(&movies).
		Where("resource_id = ?", resourceID).
		Context(ctx).
		Select()

	if err != nil {
		return nil, err
	}

	return movies, nil
}
