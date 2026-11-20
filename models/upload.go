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
