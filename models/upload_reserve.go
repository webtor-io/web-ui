package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// UploadReserve is the space one upload ID holds against its owner's
// upload quota: ReservedBytes, while the write token (WriteUntil) is alive
// or the upload is not known to be gone (GoneAt), unless the upload is
// pledged in Vault. Rows are removed only with the account.
type UploadReserve struct {
	tableName     struct{}   `pg:"upload_reserve"`
	UserID        uuid.UUID  `pg:"user_id,pk,type:uuid"`
	ResourceID    string     `pg:"resource_id,pk"`
	ReservedBytes int64      `pg:"reserved_bytes,use_zero"`
	WriteUntil    *time.Time `pg:"write_until"`
	GoneAt        *time.Time `pg:"gone_at"`
}

// ListAllUploadReserves returns every reserve row of the account, for the
// data export.
func ListAllUploadReserves(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]*UploadReserve, error) {
	var list []*UploadReserve
	err := db.Model(&list).
		Context(ctx).
		Where("user_id = ?", userID).
		OrderExpr("resource_id ASC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list upload reserves")
	}
	return list, nil
}
