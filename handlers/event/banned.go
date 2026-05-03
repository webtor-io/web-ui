package event

import (
	"context"
	"encoding/json"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

type resourceBannedMsg struct {
	Infohash string `json:"infohash"`
}

// resourceBanned reacts to abuse-store's `resource.banned` event by purging
// every trace of an infohash from the web-ui's tables. Heavy lifting is done
// by FK cascades — `media_info → movie/series → episode` is `ON DELETE
// CASCADE`, so a single delete on `media_info` clears the whole media
// structure. Tables that point to `user(user_id)` instead of the resource
// (library, watch_history, cache_index, user_subtitle) need explicit deletes.
//
// The vault block additionally refunds frozen VP via tx_log entries before
// dropping pledges (pledge → resource is `ON DELETE RESTRICT`).
//
// All work runs in a single transaction so a partial failure leaves the row
// set unchanged and the message gets re-delivered. The whole flow is
// idempotent: on a redelivery, no funded pledges remain so refund inserts
// produce zero rows, and DELETE-by-resource_id is naturally a no-op when the
// rows are gone.
func (h *Handler) resourceBanned(data []byte) error {
	var m resourceBannedMsg
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, "failed to unmarshal resource.banned payload")
	}
	if m.Infohash == "" {
		log.Warn("resource.banned with empty infohash, skipping")
		return nil
	}

	ctx := context.Background()
	db := h.pg.Get()
	if db == nil {
		return errors.New("database connection is not available")
	}

	err := db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Refund: one tx_log entry per funded pledge — captured *before* the
		// pledges are dropped, so the user keeps an audit row showing the
		// VP returned for this resource. OpTypeAbuseRefund distinguishes
		// these from user-initiated claims.
		refundRes, err := tx.ExecContext(ctx, `
			INSERT INTO vault.tx_log (user_id, resource_id, balance, op_type)
			SELECT user_id, resource_id, amount, ?
			FROM vault.pledge
			WHERE resource_id = ? AND funded = TRUE
		`, vaultModels.OpTypeAbuseRefund, m.Infohash)
		if err != nil {
			return errors.Wrap(err, "failed to insert refund tx_log entries")
		}

		// vault.pledge.resource_id → vault.resource ON DELETE RESTRICT,
		// so pledges must go before the resource row.
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault.pledge WHERE resource_id = ?`, m.Infohash); err != nil {
			return errors.Wrap(err, "failed to delete vault.pledge rows")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault.resource WHERE resource_id = ?`, m.Infohash); err != nil {
			return errors.Wrap(err, "failed to delete vault.resource row")
		}

		// media_info cascades to movie, series (→ episode), episode.
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_info WHERE resource_id = ?`, m.Infohash); err != nil {
			return errors.Wrap(err, "failed to delete media_info row")
		}

		// Per-user / cache rows that have no FK on the resource.
		for _, q := range []string{
			`DELETE FROM library WHERE resource_id = ?`,
			`DELETE FROM watch_history WHERE resource_id = ?`,
			`DELETE FROM cache_index WHERE resource_id = ?`,
			`DELETE FROM torrent_resource WHERE resource_id = ?`,
			`DELETE FROM user_subtitle WHERE resource_id = ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, m.Infohash); err != nil {
				return errors.Wrapf(err, "failed to execute cleanup query: %s", q)
			}
		}

		log.WithFields(log.Fields{
			"infohash": m.Infohash,
			"refunds":  refundRes.RowsAffected(),
		}).Info("resource.banned cleanup committed")
		return nil
	})

	if err != nil {
		return errors.Wrapf(err, "failed to clean up banned resource infohash=%s", m.Infohash)
	}
	return nil
}
