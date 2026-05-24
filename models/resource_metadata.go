package models

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
)

// ResourceMetadata is the per-torrent classification + parsed-name
// cache. Sits orthogonal to movie_metadata / series_metadata: those
// exist only when enrichment matched a known work; this row exists
// for every torrent we've seen and carries the parse_torrent_name
// output plus the derived adult/sport flags.
//
// Metadata is the full ptn.TorrentInfo as JSONB — kept open-shaped
// so we can iterate later (showing resolution/codec on cards,
// building filters) without further migrations. Hot-path booleans
// (IsAdult / IsSport) are denormalised at write-time so poster
// blur decisions and library filters don't have to walk the JSON.
type ResourceMetadata struct {
	tableName struct{} `pg:"resource_metadata"`

	ResourceID string                 `pg:"resource_id,pk"`
	IsAdult    bool                   `pg:"is_adult,use_zero,notnull"`
	IsSport    bool                   `pg:"is_sport,use_zero,notnull"`
	Metadata   map[string]interface{} `pg:"metadata,type:jsonb"`
	CreatedAt  time.Time              `pg:"created_at,notnull"`
	UpdatedAt  time.Time              `pg:"updated_at,notnull"`
}

// GetResourceMetadataByResourceID returns the row for a resource, or
// nil when the resource hasn't been classified yet. Callers treat nil
// as "unknown — default to non-adult/non-sport behaviour"; the
// classifier service backfills lazily on first encounter.
func GetResourceMetadataByResourceID(ctx context.Context, db *pg.DB, resourceID string) (*ResourceMetadata, error) {
	rm := new(ResourceMetadata)
	err := db.Model(rm).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return rm, nil
}

// GetResourceIDsWithoutResourceMetadata returns hashes of media_info
// rows that don't yet have a paired resource_metadata. Used by the
// `enrich run --metadata-only` backfill — older resources, enriched
// before classification existed, need their adult/sport flags
// populated without rerunning the heavy mapper/AI pipeline.
//
// Joins from media_info rather than torrent_resource so non-library
// (stream-only) resources are also covered — their shared previews
// matter just as much for adult-safety as library cards.
func GetResourceIDsWithoutResourceMetadata(ctx context.Context, db *pg.DB) ([]string, error) {
	var rows []struct {
		ResourceID string `pg:"resource_id"`
	}
	_, err := db.QueryContext(ctx, &rows, `
		SELECT mi.resource_id
		FROM media_info mi
		LEFT JOIN resource_metadata rm ON rm.resource_id = mi.resource_id
		WHERE rm.resource_id IS NULL
	`)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ResourceID
	}
	return out, nil
}

// GetAllResourceIDsFromMediaInfo returns every hash known to media_info.
// Used by `enrich run --metadata-only --force` to re-classify the entire
// catalog after a parse_torrent_name change (new adult-studio prefix,
// updated sport regex, etc.) — UpsertResourceMetadata then replaces
// existing rows in place.
func GetAllResourceIDsFromMediaInfo(ctx context.Context, db *pg.DB) ([]string, error) {
	var rows []struct {
		ResourceID string `pg:"resource_id"`
	}
	_, err := db.QueryContext(ctx, &rows, `SELECT resource_id FROM media_info`)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ResourceID
	}
	return out, nil
}

// UpsertResourceMetadata writes (or replaces) the row keyed by
// resource_id. Re-running classification is idempotent — same parse
// + same flags = same row, the trigger bumps updated_at.
func UpsertResourceMetadata(ctx context.Context, db *pg.DB, rm *ResourceMetadata) error {
	if rm.CreatedAt.IsZero() {
		rm.CreatedAt = time.Now()
	}
	if rm.UpdatedAt.IsZero() {
		rm.UpdatedAt = rm.CreatedAt
	}
	_, err := db.Model(rm).
		Context(ctx).
		OnConflict("(resource_id) DO UPDATE").
		Set("is_adult = EXCLUDED.is_adult").
		Set("is_sport = EXCLUDED.is_sport").
		Set("metadata = EXCLUDED.metadata").
		Set("updated_at = now()").
		Insert()
	return err
}
