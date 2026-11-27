package event

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/uploads"
)

const (
	// resourceBannedRetry: a Nak without a delay is a hot loop, and the
	// handler fails for as long as uploads are off or the proxy or the
	// storage is down.
	resourceBannedRetry = 30 * time.Second
	// resourceBannedTimeout bounds a banned upload's delete and erasure: a
	// hung proxy must not hold every ban behind it.
	resourceBannedTimeout = time.Minute
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
// A user's upload goes from the seeder's disk and from the picture buckets
// first (bannedUpload); until both are done the rows stay and the event is
// retried.
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

	if uploads.Owned(m.Infohash) {
		if err := h.bannedUpload(m.Infohash); err != nil {
			return err
		}
	}

	if err := models.PurgeResourceByID(ctx, db, m.Infohash); err != nil {
		return errors.Wrapf(err, "failed to clean up banned resource infohash=%s", m.Infohash)
	}
	log.WithField("infohash", m.Infohash).Info("resource.banned cleanup committed")
	return nil
}

// bannedUpload deletes a banned upload's files on the seeder, then its
// pictures — before PurgeResourceByID, while the thumbnail rows still name the
// objects. A repeat finds the files gone (410, no error) and erases again.
func (h *Handler) bannedUpload(id string) error {
	if h.up == nil {
		log.WithField("infohash", id).Warn("resource.banned for an upload while uploads are off: its files stay on the seeder")
		return errors.New("uploads are off: the upload's files are not deleted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), resourceBannedTimeout)
	defer cancel()
	if _, err := h.up.Delete(ctx, id); err != nil {
		return errors.Wrapf(err, "failed to delete banned upload infohash=%s", id)
	}
	if err := h.thumb.Erase(ctx, id); err != nil {
		return errors.Wrapf(err, "failed to erase the pictures of banned upload infohash=%s", id)
	}
	return nil
}
