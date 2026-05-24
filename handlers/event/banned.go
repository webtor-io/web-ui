package event

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
)

type resourceBannedMsg struct {
	Infohash string `json:"infohash"`
}

// resourceBanned reacts to abuse-store's `resource.banned` event by
// delegating to models.PurgeResourceByID, which is the single
// source-of-truth for "wipe every trace of an infohash". The same
// helper is also called from the metadata-only backfill when it
// encounters a stoplist rejection — both paths converge on the same
// SQL.
//
// Idempotent: redelivery on an already-purged hash is a no-op
// (DELETE-by-id has nothing to match, refund SELECT returns zero rows).
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

	if err := models.PurgeResourceByID(ctx, db, m.Infohash); err != nil {
		return errors.Wrapf(err, "failed to clean up banned resource infohash=%s", m.Infohash)
	}
	log.WithField("infohash", m.Infohash).Info("resource.banned cleanup committed")
	return nil
}
