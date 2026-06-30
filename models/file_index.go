package models

import (
	"context"
	"fmt"

	"github.com/go-pg/pg/v10"
)

// FileIndexEntry maps a torrent file path to its natural file index and size,
// as reported by rest-api's ListItem (Index/Size).
type FileIndexEntry struct {
	Path     string
	FileIdx  int
	FileSize int64
}

// HasNullFileIdx reports whether any movie or episode row for the resource is
// missing its file_idx. Cheap gate so the self-heal in enrich skips the
// rest-api round trip once a resource is fully populated (the steady state
// after the one-time backfill).
func HasNullFileIdx(ctx context.Context, db *pg.DB, resourceID string) (bool, error) {
	cnt, err := db.Model((*Movie)(nil)).
		Context(ctx).
		Where("resource_id = ? AND file_idx IS NULL", resourceID).
		Count()
	if err != nil {
		return false, err
	}
	if cnt > 0 {
		return true, nil
	}
	cnt, err = db.Model((*Episode)(nil)).
		Context(ctx).
		Where("resource_id = ? AND file_idx IS NULL", resourceID).
		Count()
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// fillFileIdxQuery updates file_idx/file_size by matching path, only where
// still NULL (idempotent, races safely with a concurrent full enrich). %s is
// the hardcoded table name (movie|episode) — the only non-parameterizable
// token; never user data.
const fillFileIdxQuery = `
update %s t
set file_idx = v.idx, file_size = v.sz
from unnest(?::text[], ?::int[], ?::bigint[]) as v(path, idx, sz)
where t.resource_id = ? and t.path = v.path and t.file_idx is null`

// FillFileIndex sets file_idx/file_size on the resource's movie and episode
// rows in one statement per table. A resource is either movie- or
// series-typed, so one of the two updates is a no-op.
func FillFileIndex(ctx context.Context, db *pg.DB, resourceID string, entries []FileIndexEntry) error {
	if len(entries) == 0 {
		return nil
	}
	paths := make([]string, len(entries))
	idxs := make([]int, len(entries))
	sizes := make([]int64, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
		idxs[i] = e.FileIdx
		sizes[i] = e.FileSize
	}
	for _, table := range []string{"movie", "episode"} {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(fillFileIdxQuery, table),
			pg.Array(paths), pg.Array(idxs), pg.Array(sizes), resourceID); err != nil {
			return err
		}
	}
	return nil
}
