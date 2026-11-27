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

// uploadReserveUnpledged: the upload is not pledged in Vault, which VP pay
// for and which keeps it past its directory.
const uploadReserveUnpledged = `NOT EXISTS (SELECT 1 FROM vault.pledge p WHERE p.resource_id = r.resource_id AND p.user_id = r.user_id)`

// uploadReserveCounted is the account's rows its quota may count: all but
// the excepted ID and the uploads pledged in Vault.
const uploadReserveCounted = `r.user_id = ? AND r.resource_id <> ? AND ` + uploadReserveUnpledged

// uploadReserveIdle: the row's token is over, or it never had one, so no
// write can bring the upload's files back.
const uploadReserveIdle = `(r.write_until IS NULL OR r.write_until + interval '5 minutes' <= now())`

// UploadUsed is the account's quota use without exceptID. A row weighs its
// reserved_bytes while its token is alive (write_until plus five minutes)
// or while it is not marked gone; a gone row past its token weighs nothing.
func UploadUsed(ctx context.Context, db pg.DBI, userID uuid.UUID, exceptID string) (int64, error) {
	var used int64
	_, err := db.QueryOneContext(ctx, pg.Scan(&used), `SELECT coalesce(sum(r.reserved_bytes), 0) FROM upload_reserve r
WHERE `+uploadReserveCounted+`
  AND (r.write_until + interval '5 minutes' > now() OR r.gone_at IS NULL)`, userID, exceptID)
	if err != nil {
		return 0, errors.Wrap(err, "failed to sum upload reserves")
	}
	return used, nil
}

// UploadLiveReserve is the reserve of id while its token is alive, 0 after
// that or without a row.
func UploadLiveReserve(ctx context.Context, db pg.DBI, userID uuid.UUID, id string) (int64, error) {
	var reserved int64
	_, err := db.QueryContext(ctx, pg.Scan(&reserved), `SELECT reserved_bytes FROM upload_reserve
WHERE user_id = ? AND resource_id = ? AND write_until + interval '5 minutes' > now()`, userID, id)
	if err != nil {
		return 0, errors.Wrap(err, "failed to read upload reserve")
	}
	return reserved, nil
}

// UploadCheckCandidates are the counted rows worth asking rest-api about:
// not marked gone, and past their token or without one. Random order, so
// that rows a deadline cuts off are not the same ones every time.
func UploadCheckCandidates(ctx context.Context, db pg.DBI, userID uuid.UUID, exceptID string) ([]UploadReserve, error) {
	var rows []UploadReserve
	_, err := db.QueryContext(ctx, &rows, `SELECT r.* FROM upload_reserve r
WHERE `+uploadReserveCounted+`
  AND r.gone_at IS NULL
  AND `+uploadReserveIdle+`
ORDER BY random()`, userID, exceptID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list upload reserves to check")
	}
	return rows, nil
}

// SetUploadReserved writes what the seeder holds for r, unless a token was
// issued for it since r was read.
func SetUploadReserved(ctx context.Context, db pg.DBI, r *UploadReserve, reserved int64) error {
	_, err := db.ExecContext(ctx, `UPDATE upload_reserve SET reserved_bytes = ?
WHERE user_id = ? AND resource_id = ? AND write_until IS NOT DISTINCT FROM ?`, reserved, r.UserID, r.ResourceID, r.WriteUntil)
	return errors.Wrap(err, "failed to update upload reserve")
}

// MarkUploadGone marks r gone, unless a token was issued for it since r was
// read.
func MarkUploadGone(ctx context.Context, db pg.DBI, r *UploadReserve) error {
	_, err := db.ExecContext(ctx, `UPDATE upload_reserve SET gone_at = now()
WHERE user_id = ? AND resource_id = ? AND write_until IS NOT DISTINCT FROM ?`, r.UserID, r.ResourceID, r.WriteUntil)
	return errors.Wrap(err, "failed to mark upload gone")
}

// MarkUploadEvicted marks id gone once the cleaner removed its directory:
// the rows a quota check would ask about, of any account (only the owner
// ever holds one).
//
// ponytail: it scans upload_reserve, whose key leads with user_id; an index
// on resource_id when the table outgrows a scan.
func MarkUploadEvicted(ctx context.Context, db pg.DBI, id string) error {
	_, err := db.ExecContext(ctx, `UPDATE upload_reserve r SET gone_at = now()
WHERE r.resource_id = ? AND r.gone_at IS NULL AND `+uploadReserveUnpledged+` AND `+uploadReserveIdle, id)
	return errors.Wrap(err, "failed to mark an evicted upload gone")
}

// MarkOwnUploadGone marks the account's upload id gone once rest-api answered
// its page that it does not have it: the inference a quota check makes, on
// the rows it would ask about.
func MarkOwnUploadGone(ctx context.Context, db pg.DBI, userID uuid.UUID, id string) error {
	_, err := db.ExecContext(ctx, `UPDATE upload_reserve r SET gone_at = now()
WHERE r.user_id = ? AND r.resource_id = ? AND r.gone_at IS NULL AND `+uploadReserveUnpledged+` AND `+uploadReserveIdle, userID, id)
	return errors.Wrap(err, "failed to mark upload gone")
}

// MarkUploadDeleted marks the account's upload id gone once its owner deleted
// it. tombstoned: the seeder wrote the upload's tombstone, the upload can
// never grow back, and the row weighs nothing from now on; otherwise it
// weighs until its token ends.
func MarkUploadDeleted(ctx context.Context, db pg.DBI, userID uuid.UUID, id string, tombstoned bool) error {
	_, err := db.ExecContext(ctx, `UPDATE upload_reserve SET gone_at = now(),
  write_until = CASE WHEN ? THEN NULL ELSE write_until END
WHERE user_id = ? AND resource_id = ?`, tombstoned, userID, id)
	return errors.Wrap(err, "failed to mark upload deleted")
}

// HasUploadReserve says the account was ever given a write token for id.
func HasUploadReserve(ctx context.Context, db pg.DBI, userID uuid.UUID, id string) (bool, error) {
	var ok bool
	_, err := db.QueryOneContext(ctx, pg.Scan(&ok), `SELECT EXISTS (SELECT 1 FROM upload_reserve WHERE user_id = ? AND resource_id = ?)`, userID, id)
	if err != nil {
		return false, errors.Wrap(err, "failed to read upload reserve")
	}
	return ok, nil
}

// UploadReserveIDs are the uploads the account was ever given a write token
// for.
func UploadReserveIDs(ctx context.Context, db pg.DBI, userID uuid.UUID) ([]string, error) {
	var ids []string
	err := db.ModelContext(ctx, (*UploadReserve)(nil)).
		Column("resource_id").
		Where("user_id = ?", userID).
		Select(&ids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the account's uploads")
	}
	return ids, nil
}

// UploadGoneIDs are the account's uploads marked gone: the cleaner removed
// them, a quota check or their page found rest-api without them, or their
// owner deleted them.
func UploadGoneIDs(ctx context.Context, db pg.DBI, userID uuid.UUID) ([]string, error) {
	var ids []string
	err := db.ModelContext(ctx, (*UploadReserve)(nil)).
		Column("resource_id").
		Where("user_id = ? AND gone_at IS NOT NULL", userID).
		Select(&ids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the account's gone uploads")
	}
	return ids, nil
}

// UpsertUploadReserve records a token for id with limit lim until exp. A live
// reserve is never lowered and a token end never moved back.
func UpsertUploadReserve(ctx context.Context, db pg.DBI, userID uuid.UUID, id string, lim int64, exp time.Time) error {
	_, err := db.ExecContext(ctx, `INSERT INTO upload_reserve (user_id, resource_id, reserved_bytes, write_until, gone_at)
VALUES (?, ?, ?, ?, NULL)
ON CONFLICT (user_id, resource_id) DO UPDATE SET
  reserved_bytes = GREATEST(EXCLUDED.reserved_bytes,
                            CASE WHEN upload_reserve.write_until + interval '5 minutes' > now()
                                 THEN upload_reserve.reserved_bytes ELSE 0 END),
  write_until    = GREATEST(upload_reserve.write_until, EXCLUDED.write_until),
  gone_at        = NULL`, userID, id, lim, exp)
	return errors.Wrap(err, "failed to write upload reserve")
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
