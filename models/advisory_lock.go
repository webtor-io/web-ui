package models

import (
	"context"
	"hash/fnv"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
)

// AdvisoryXactLock takes a lock on key until tx ends. FNV-64a of key folded
// into a bigint; collisions only serialise unrelated work for one
// transaction.
func AdvisoryXactLock(ctx context.Context, tx *pg.Tx, key string) error {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(?)", int64(h.Sum64())); err != nil {
		return errors.Wrap(err, "failed to take advisory lock")
	}
	return nil
}
