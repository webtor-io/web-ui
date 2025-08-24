package webdav

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

type Library interface {
	GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error)
}

type AllLibrary struct{}

func (s *AllLibrary) GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error) {
	return models.GetLibraryTorrentsList(ctx, db, uID, models.SortTypeName)
}

var _ Library = (*AllLibrary)(nil)

type MovieLibrary struct{}

func (s *MovieLibrary) GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error) {
	return models.GetLibraryMovieTorrentList(ctx, db, uID, models.SortTypeName)
}

var _ Library = (*MovieLibrary)(nil)

type SeriesLibrary struct{}

func (s *SeriesLibrary) GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error) {
	return models.GetLibrarySeriesTorrentList(ctx, db, uID, models.SortTypeName)
}

var _ Library = (*SeriesLibrary)(nil)
