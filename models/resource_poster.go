package models

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
)

// ResourcePoster is the poster a user saw in Discover when they started a
// torrent. See migration 77.
type ResourcePoster struct {
	tableName struct{} `pg:"resource_poster"`

	ResourceID string `pg:"resource_id,pk"`
	PosterURL  string `pg:"poster_url"`
}

// GetResourcePoster returns the stored poster for a resource, or nil.
func GetResourcePoster(ctx context.Context, db *pg.DB, resourceID string) (*ResourcePoster, error) {
	p := &ResourcePoster{}
	err := db.Model(p).Context(ctx).Where("resource_id = ?", resourceID).Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get resource poster")
	}
	return p, nil
}

// SaveResourcePoster stores a poster unless the resource already has one.
func SaveResourcePoster(ctx context.Context, db *pg.DB, resourceID, posterURL string) error {
	_, err := db.Model(&ResourcePoster{ResourceID: resourceID, PosterURL: posterURL}).
		Context(ctx).
		OnConflict("(resource_id) DO NOTHING").
		Insert()
	return errors.Wrap(err, "failed to save resource poster")
}
