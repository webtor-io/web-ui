package models

import (
	"context"
	"fmt"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// CacheSource is where an index entry came from -- see migration 73 for why
// the two are kept apart.
//
// Stored as a smallint; this is the dictionary. Numbers are never reused or
// renumbered -- they are in the table. They start at 1: go-pg leaves a zero
// value out of an insert, and a source of 0 would quietly be written as the
// column default.
type CacheSource int16

const (
	// CacheSourceAny is not stored: it is "whatever the source" in a removal.
	CacheSourceAny CacheSource = 0
	// CacheSourceProbe: a backend was asked at play time and said "cached".
	CacheSourceProbe CacheSource = 1
	// CacheSourceSeeder: the seeder reported the file complete on its disk.
	CacheSourceSeeder CacheSource = 2
)

func (s CacheSource) String() string {
	switch s {
	case CacheSourceAny:
		return "any"
	case CacheSourceProbe:
		return "probe"
	case CacheSourceSeeder:
		return "seeder"
	}
	return fmt.Sprintf("source(%d)", int16(s))
}

// CacheWindows is how long an entry of each source is believed without being
// confirmed again.
type CacheWindows struct {
	Probe  time.Duration
	Seeder time.Duration
}

// fresh is the "still believed" condition; stale is its negation, for the
// cleanup. Written once so the reads and the cleanup cannot drift apart.
func (w CacheWindows) fresh(q *pg.Query) *pg.Query {
	now := time.Now()
	return q.WhereGroup(func(q *pg.Query) (*pg.Query, error) {
		return q.
			WhereOr("(source = ? AND last_seen_at >= ?)", CacheSourceSeeder, now.Add(-w.Seeder)).
			WhereOr("(source <> ? AND last_seen_at >= ?)", CacheSourceSeeder, now.Add(-w.Probe)), nil
	})
}

func (w CacheWindows) stale(q *pg.Query) *pg.Query {
	now := time.Now()
	return q.WhereGroup(func(q *pg.Query) (*pg.Query, error) {
		return q.
			WhereOr("(source = ? AND last_seen_at < ?)", CacheSourceSeeder, now.Add(-w.Seeder)).
			WhereOr("(source <> ? AND last_seen_at < ?)", CacheSourceSeeder, now.Add(-w.Probe)), nil
	})
}

type CacheIndex struct {
	tableName   struct{}             `pg:"cache_index"`
	ID          uuid.UUID            `pg:"cache_index_id,pk,type:uuid,default:uuid_generate_v4()"`
	BackendType StreamingBackendType `pg:"backend_type,notnull"`
	Source      CacheSource          `pg:"source,notnull"`
	ResourceID  string               `pg:"resource_id,notnull"`
	FileIdx     int                  `pg:"file_idx,notnull,use_zero"`
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
func MarkAsCached(ctx context.Context, db *pg.DB, backendType StreamingBackendType, source CacheSource, resourceID string, fileIdx int) error {
	now := time.Now()
	cache := &CacheIndex{
		BackendType: backendType,
		Source:      source,
		ResourceID:  resourceID,
		FileIdx:     fileIdx,
		LastSeenAt:  now,
	}

	_, err := db.Model(cache).
		Context(ctx).
		Column("backend_type", "source", "resource_id", "file_idx", "last_seen_at").
		OnConflict("(resource_id, file_idx, backend_type, source) DO UPDATE").
		Set("last_seen_at = EXCLUDED.last_seen_at").
		Insert()

	return err
}

// IsCached returns the backends that have the file, each once, with the most
// recent sighting from any source. Only entries still believed are counted.
func IsCached(ctx context.Context, db *pg.DB, resourceID string, fileIdx int, w CacheWindows) ([]CacheIndexResult, error) {
	var results []CacheIndexResult

	err := w.fresh(db.Model((*CacheIndex)(nil)).
		Context(ctx).
		Column("backend_type").
		ColumnExpr("max(last_seen_at) AS last_seen_at").
		Where("resource_id = ?", resourceID).
		Where("file_idx = ?", fileIdx)).
		Group("backend_type").
		Select(&results)

	if err != nil {
		return nil, err
	}

	return results, nil
}

// DeleteOldCacheEntries removes entries past their source's window.
func DeleteOldCacheEntries(ctx context.Context, db *pg.DB, w CacheWindows) (int, error) {
	res, err := w.stale(db.Model((*CacheIndex)(nil)).Context(ctx)).Delete()

	if err != nil {
		return 0, err
	}

	return res.RowsAffected(), nil
}

// CachedFile is one file the index knows of.
type CachedFile struct {
	ResourceID string
	FileIdx    int
}

// GetCachedFiles is IsCached for a whole list of resources at once -- what a
// page of streams needs (Discover: 50-100 releases of one title). One indexed
// read instead of one per release.
func GetCachedFiles(ctx context.Context, db *pg.DB, resourceIDs []string, backendType StreamingBackendType, w CacheWindows) ([]CachedFile, error) {
	var out []CachedFile
	if len(resourceIDs) == 0 {
		return out, nil
	}
	err := w.fresh(db.Model((*CacheIndex)(nil)).
		Context(ctx).
		ColumnExpr("DISTINCT resource_id, file_idx").
		Where("resource_id IN (?)", pg.In(resourceIDs)).
		Where("backend_type = ?", backendType)).
		Select(&out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UnmarkCached drops index entries of one backend for a resource: one file
// when fileIdx is set, every file of the resource when it is nil (the whole
// torrent left the cache at once). CacheSourceAny means every source -- the
// backend itself was asked and said no; a named one removes only what that
// source wrote, so the seeder losing a file cannot erase what is known of the
// Vault.
func UnmarkCached(ctx context.Context, db *pg.DB, backendType StreamingBackendType, source CacheSource, resourceID string, fileIdx *int) error {
	q := db.Model((*CacheIndex)(nil)).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Where("backend_type = ?", backendType)
	_, err := unmarkScope(q, source, fileIdx).Delete()
	return err
}

func unmarkScope(q *pg.Query, source CacheSource, fileIdx *int) *pg.Query {
	if source != CacheSourceAny {
		q = q.Where("source = ?", source)
	}
	if fileIdx != nil {
		q = q.Where("file_idx = ?", *fileIdx)
	}
	return q
}
