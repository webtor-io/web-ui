package models

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
)

// opTypeAbuseRefund mirrors models/vault.OpTypeAbuseRefund — duplicated
// here (instead of imported) because models/vault imports the models
// package, and pulling it back in would close an import cycle. Keep
// in sync with vault.tx_log if the enum ever shifts.
const opTypeAbuseRefund int16 = 4

// PurgeResourceByID wipes every web-ui trace of a banned/stoplist
// infohash in a single transaction. FK cascades handle the heavy
// lifting: `media_info → movie/series → episode → resource_metadata`
// is `ON DELETE CASCADE`, so a single `DELETE FROM media_info` clears
// the whole media stack. Tables that point to `user(user_id)` instead
// of the resource (library, watch_history, cache_index, user_subtitle,
// torrent_resource, ai_enrich.query) need explicit deletes.
//
// Vault is handled first: any funded pledges are converted to refund
// tx_log entries so the user keeps an audit trail of the VP they got
// back, then the pledges themselves are dropped (FK ON DELETE
// RESTRICT). The vault.resource row goes next.
//
// Two consumers today:
//   - handlers/event/banned.go on the NATS `resource.banned` broadcast
//     from abuse-store (live ban path)
//   - services/enrich on stoplist rejections seen during the
//     metadata-only backfill (cleanup of pre-existing leakage that
//     pre-dates abuse-store wiring)
//
// Idempotent: re-running on a hash whose rows are already gone is a
// no-op. Caller logs success / failure — this function only returns
// the error.
func PurgeResourceByID(ctx context.Context, db *pg.DB, infohash string) error {
	if infohash == "" {
		return errors.New("infohash is empty")
	}
	return db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Refund first — captured before pledges drop so the audit row
		// shows the VP returned. Distinct OpType keeps these separable
		// from user-initiated claims.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vault.tx_log (user_id, resource_id, balance, op_type)
			SELECT user_id, resource_id, amount, ?
			FROM vault.pledge
			WHERE resource_id = ? AND funded = TRUE
		`, opTypeAbuseRefund, infohash); err != nil {
			return errors.Wrap(err, "failed to insert refund tx_log entries")
		}

		// vault.pledge.resource_id → vault.resource ON DELETE RESTRICT,
		// so pledges must go before the resource row.
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault.pledge WHERE resource_id = ?`, infohash); err != nil {
			return errors.Wrap(err, "failed to delete vault.pledge rows")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault.resource WHERE resource_id = ?`, infohash); err != nil {
			return errors.Wrap(err, "failed to delete vault.resource row")
		}

		// media_info cascades to movie, series (→ episode), episode,
		// resource_metadata.
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_info WHERE resource_id = ?`, infohash); err != nil {
			return errors.Wrap(err, "failed to delete media_info row")
		}

		// Per-user / cache rows that have no FK on the resource.
		// ai_enrich.query: diagnostic resource_id column (non-unique,
		// PK is (parsed_title, parsed_year, content_type)) records the
		// first torrent that triggered the memoized Claude lookup —
		// best-effort cleanup of the diagnostic trace.
		for _, q := range []string{
			`DELETE FROM library WHERE resource_id = ?`,
			`DELETE FROM watch_history WHERE resource_id = ?`,
			`DELETE FROM cache_index WHERE resource_id = ?`,
			`DELETE FROM torrent_resource WHERE resource_id = ?`,
			`DELETE FROM user_subtitle WHERE resource_id = ?`,
			`DELETE FROM ai_enrich.query WHERE resource_id = ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, infohash); err != nil {
				return errors.Wrapf(err, "failed to execute cleanup query: %s", q)
			}
		}
		return nil
	})
}
