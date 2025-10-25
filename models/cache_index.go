package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

type CacheIndex struct {
	tableName   struct{}             `pg:"cache_index"`
	ID          uuid.UUID            `pg:"cache_index_id,pk,type:uuid,default:uuid_generate_v4()"`
	BackendType StreamingBackendType `pg:"backend_type,notnull"`
	ResourceID  string               `pg:"resource_id,notnull"`
	Path        string               `pg:"path,notnull"`
	LastSeenAt  time.Time            `pg:"last_seen_at,default:now()"`
	CreatedAt   time.Time            `pg:"created_at,default:now()"`
	UpdatedAt   time.Time            `pg:"updated_at,default:now()"`
}

// CacheIndexResult represents a cache entry with backend type and last seen time
type CacheIndexResult struct {
	BackendType StreamingBackendType
	LastSeenAt  time.Time
}

// MarkAsCached updates the last_seen_at for a cache entry, or creates it if it doesn't exist
func MarkAsCached(ctx context.Context, db *pg.DB, backendType StreamingBackendType, resourceID, path string) error {
	now := time.Now()
	cache := &CacheIndex{
		BackendType: backendType,
		ResourceID:  resourceID,
		Path:        path,
		LastSeenAt:  now,
	}

	_, err := db.Model(cache).
		Context(ctx).
		Column("backend_type", "resource_id", "path", "last_seen_at").
		OnConflict("(resource_id, path, backend_type) DO UPDATE").
		Set("last_seen_at = EXCLUDED.last_seen_at").
		Insert()

	return err
}

// IsCached returns a list of backend types and their last seen times for a given resource and path
// Only returns entries that were seen within the expiration window
func IsCached(ctx context.Context, db *pg.DB, resourceID, path string, expiration time.Duration) ([]CacheIndexResult, error) {
	var results []CacheIndexResult
	cutoffTime := time.Now().Add(-expiration)

	err := db.Model((*CacheIndex)(nil)).
		Context(ctx).
		Column("backend_type", "last_seen_at").
		Where("resource_id = ?", resourceID).
		Where("path = ?", path).
		Where("last_seen_at >= ?", cutoffTime).
		Select(&results)

	if err != nil {
		return nil, err
	}

	return results, nil
}

// DeleteOldCacheEntries removes cache entries older than the specified expiration
func DeleteOldCacheEntries(ctx context.Context, db *pg.DB, expiration time.Duration) (int, error) {
	cutoffTime := time.Now().Add(-expiration)

	res, err := db.Model((*CacheIndex)(nil)).
		Context(ctx).
		Where("last_seen_at < ?", cutoffTime).
		Delete()

	if err != nil {
		return 0, err
	}

	return res.RowsAffected(), nil
}
