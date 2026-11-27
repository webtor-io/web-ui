package models

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// UploadPrefix starts every upload ID, so that an ID tells an upload from a
// torrent infohash.
const UploadPrefix = "9c5db5752b7c"

// CreateUpload records a new upload: its resource row, with no files until
// the seeder reports them, and the owner's library row. Rows already there
// are kept.
func CreateUpload(ctx context.Context, db pg.DBI, uID uuid.UUID, id, name string) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes) VALUES (?, ?, 0, 0)
ON CONFLICT DO NOTHING`, id, name); err != nil {
		return errors.Wrap(err, "failed to insert upload resource")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO library (user_id, resource_id, name, created_at) VALUES (?, ?, ?, now())
ON CONFLICT DO NOTHING`, uID, id, name); err != nil {
		return errors.Wrap(err, "failed to insert upload library entry")
	}
	return nil
}

// UploadFrozen says the upload is saved to Vault: it has a vault.resource
// row. It takes pg.DBI to run inside the transaction that issues a token.
func UploadFrozen(ctx context.Context, db pg.DBI, id string) (bool, error) {
	var frozen bool
	if _, err := db.QueryOneContext(ctx, pg.Scan(&frozen), `SELECT EXISTS(SELECT 1 FROM vault.resource WHERE resource_id = ?)`, id); err != nil {
		return false, errors.Wrap(err, "failed to check the upload's vault resource")
	}
	return frozen, nil
}

// AddUploadToLibrary keeps the owner's library row of an upload, records
// its size and file count and clears its gone mark: rest-api has just
// answered for it. torrent_resource.updated_at moves only when the size or
// the count changes. An empty name (no file finished yet) keeps the name
// already there.
func AddUploadToLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID, id, name string, size int64, filesCount int) error {
	return db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO torrent_resource (resource_id, name, size_bytes, file_count) VALUES (?, ?, ?, ?)
ON CONFLICT (resource_id) DO UPDATE SET name = COALESCE(NULLIF(EXCLUDED.name, ''), torrent_resource.name),
  size_bytes = EXCLUDED.size_bytes,
  file_count = EXCLUDED.file_count,
  updated_at = CASE WHEN torrent_resource.size_bytes IS DISTINCT FROM EXCLUDED.size_bytes
                      OR torrent_resource.file_count IS DISTINCT FROM EXCLUDED.file_count
                    THEN now() ELSE torrent_resource.updated_at END`, id, name, size, filesCount); err != nil {
			return errors.Wrap(err, "failed to upsert upload resource")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO library (user_id, resource_id, name, created_at) VALUES (?, ?, ?, now()) ON CONFLICT DO NOTHING`,
			uID, id, name); err != nil {
			return errors.Wrap(err, "failed to insert upload library entry")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE upload_reserve SET gone_at = NULL WHERE user_id = ? AND resource_id = ?`, uID, id); err != nil {
			return errors.Wrap(err, "failed to clear the upload's gone mark")
		}
		return nil
	})
}

// DropUploadTraces deletes media_info of id — movie, series, episode and resource_metadata go by
// cascade — and the ai_enrich.query rows that name it. userData true also deletes the
// watch_history and user_subtitle rows that name it, the way PurgeResourceByID does. One
// transaction.
func DropUploadTraces(ctx context.Context, db *pg.DB, id string, userData bool) error {
	qs := []string{
		`DELETE FROM media_info WHERE resource_id = ?`,
		`DELETE FROM ai_enrich.query WHERE resource_id = ?`,
	}
	if userData {
		qs = append(qs,
			`DELETE FROM watch_history WHERE resource_id = ?`,
			`DELETE FROM user_subtitle WHERE resource_id = ?`)
	}
	return db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		for _, q := range qs {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return errors.Wrapf(err, "failed to execute cleanup query: %s", q)
			}
		}
		return nil
	})
}

// DropStaleMediaInfo deletes the upload's media_info row when it is older than the last change of the
// upload's size or file count (torrent_resource.updated_at, AddUploadToLibrary); movie, series, episode
// and resource_metadata go with it by cascade. kept reports whether a row is there afterwards.
func DropStaleMediaInfo(ctx context.Context, db *pg.DB, id string) (kept bool, err error) {
	if _, err := db.ExecContext(ctx, `DELETE FROM media_info m USING torrent_resource tr
WHERE m.resource_id = ? AND tr.resource_id = m.resource_id AND m.updated_at < tr.updated_at`, id); err != nil {
		return false, errors.Wrap(err, "failed to drop stale media info")
	}
	kept, err = MediaInfoExists(ctx, db, id)
	return kept, errors.Wrap(err, "failed to check media info")
}
