package models

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// ThumbnailSourceKind labels the provenance of a thumbnail row.
// Stored as smallint in the DB; constants are exported so callers
// never deal with the raw int. Order is intentionally stable —
// don't renumber.
type ThumbnailSourceKind int16

const (
	ThumbnailSourceImageFile   ThumbnailSourceKind = 1
	ThumbnailSourceFFmpegFrame ThumbnailSourceKind = 2
)

type Thumbnail struct {
	tableName struct{} `pg:"thumbnail"`

	ThumbnailID uuid.UUID           `pg:"thumbnail_id,pk"`
	ResourceID  string              `pg:"resource_id,notnull"`
	Path        string              `pg:"path,notnull"`
	OffsetSec   int                 `pg:"offset_sec,use_zero,notnull"`
	SourceKind  ThumbnailSourceKind `pg:"source_kind,notnull"`
	// Hash is the SHA-1 of the binary content (lowercase hex). Doubles as
	// the S3 object key — `<hash>.<format>` lets a single object back many
	// thumbnail rows when the same poster.jpg ships across torrents.
	Hash      string    `pg:"hash,notnull"`
	Format    string    `pg:"format,notnull"`
	Size      int64     `pg:"size,notnull"`
	Width     int       `pg:"width"`
	Height    int       `pg:"height"`
	CreatedAt time.Time `pg:"created_at,notnull"`
	UpdatedAt time.Time `pg:"updated_at,notnull"`
}

// GetThumbnailByResourceID returns the most recent thumbnail row for a
// resource. The OG-image handler picks "most recent" because the enrich
// pipeline may produce multiple over time (image_file first, then
// regenerate via ffmpeg_frame if the image-file pull failed). Newest
// wins so a successful retry replaces an earlier mistake.
func GetThumbnailByResourceID(ctx context.Context, db *pg.DB, resourceID string) (*Thumbnail, error) {
	t := new(Thumbnail)
	err := db.Model(t).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		OrderExpr("created_at DESC").
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// UpsertThumbnail writes (or replaces) the thumbnail keyed by
// (resource_id, path, offset_sec). Replace-on-conflict lets a retry
// overwrite a stale row without a separate delete — the common case is
// re-running enrichment against the same file at the same offset.
func UpsertThumbnail(ctx context.Context, db *pg.DB, t *Thumbnail) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = t.CreatedAt
	}
	_, err := db.Model(t).
		Context(ctx).
		OnConflict("(resource_id, path, offset_sec) DO UPDATE").
		Set("source_kind = EXCLUDED.source_kind").
		Set("hash = EXCLUDED.hash").
		Set("format = EXCLUDED.format").
		Set("size = EXCLUDED.size").
		Set("width = EXCLUDED.width").
		Set("height = EXCLUDED.height").
		Set("updated_at = now()").
		Returning("*").
		Insert()
	return err
}
