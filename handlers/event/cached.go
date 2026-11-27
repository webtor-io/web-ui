package event

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/uploads"
)

// The seeder and its disk cleaner say when a file enters or leaves the
// seeder's cache, and the cache index follows. Until 2026-09 the index was fed
// only by what somebody played through a resolved link and forgot entries by
// age; it knew nothing of a file that finished downloading DURING a stream or
// a download (the start saw "not cached" and nobody looked again), and nothing
// of traffic that never passes web-ui at all (embeds, the API).
//
// The expiry stays as the backstop, not the mechanism: a node that dies takes
// its disk with it and says nothing.
//
// Subjects (JetStream stream `common`, `resource.*`):
//
//	resource.cached    {"resource_id": "<infohash>", "file_idx": 3}
//	resource.uncached  {"resource_id": "<infohash>", "file_idx": 3}
//	resource.uncached  {"resource_id": "<infohash>"}   -- the whole torrent
type resourceCacheMsg struct {
	ResourceID string `json:"resource_id"`
	FileIdx    *int   `json:"file_idx"`
}

// cacheIndexer is what the handlers need from services/cache_index.
type cacheIndexer interface {
	MarkFromSeeder(ctx context.Context, resourceID string, fileIdx int) error
	// UnmarkFromSeeder, not Unmark: the sender knows its own disk and nothing
	// of the Vault, so its "gone" may only take back its own "here".
	UnmarkFromSeeder(ctx context.Context, resourceID string, fileIdx *int) error
}

var infohashRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

const cacheEventTimeout = 10 * time.Second

// parseResourceCacheMsg returns ok=false for a message that can never be
// applied. Such a message is acknowledged, not retried: redelivery does not
// repair a malformed payload, it only blocks the consumer behind it.
func parseResourceCacheMsg(msg []byte) (m resourceCacheMsg, ok bool) {
	if err := json.Unmarshal(msg, &m); err != nil {
		return m, false
	}
	if !infohashRe.MatchString(m.ResourceID) {
		return m, false
	}
	if m.FileIdx != nil && *m.FileIdx < 0 {
		return m, false
	}
	return m, true
}

func resourceCached(ci cacheIndexer, msg []byte) error {
	m, ok := parseResourceCacheMsg(msg)
	// "Cached" is always about one file; without an index there is no row to
	// write.
	if !ok || m.FileIdx == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheEventTimeout)
	defer cancel()
	return errors.Wrap(ci.MarkFromSeeder(ctx, m.ResourceID, *m.FileIdx), "failed to mark as cached")
}

func resourceUncached(ci cacheIndexer, msg []byte) error {
	m, ok := parseResourceCacheMsg(msg)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheEventTimeout)
	defer cancel()
	return errors.Wrap(ci.UnmarkFromSeeder(ctx, m.ResourceID, m.FileIdx), "failed to unmark cached")
}

// uncached: the cache index first, as before; then what web-ui derived from an
// upload whose directory went.
func (h *Handler) uncached(ci cacheIndexer, msg []byte) error {
	if err := resourceUncached(ci, msg); err != nil {
		return err
	}
	h.uploadEvicted(msg)
	return nil
}

// uploadEvicted: the cleaner dropped a whole directory. If it was an upload's,
// its row in My files is marked gone now rather than at its owner's next
// token (models.MarkUploadEvicted), and, if it was unsaved, what web-ui
// derived from its files goes with it (uploads.Forget) — the watch history
// and the user subtitle bindings too: the cleaner removed the directory
// itself, so this "gone" is certain, unlike one inferred from a rest-api
// answer. Best effort: the event is acknowledged whatever happens here, so
// that cache events never wait for S3; a quota check and the owner's page
// write the mark and erase the enrichment rows and the pictures this missed,
// "Delete upload" — the rest.
func (h *Handler) uploadEvicted(msg []byte) {
	m, ok := parseResourceCacheMsg(msg)
	// Owned first: most evictions are torrents', and those ask nothing.
	if !ok || m.FileIdx != nil || !uploads.Owned(m.ResourceID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheEventTimeout)
	defer cancel()
	db := h.pg.Get()
	if db != nil {
		if err := models.MarkUploadEvicted(ctx, db, m.ResourceID); err != nil {
			log.WithError(err).WithField("infohash", m.ResourceID).Warn("failed to mark an evicted upload gone")
		}
	}
	if err := uploads.Forget(ctx, db, h.thumb, m.ResourceID, true); err != nil {
		log.WithError(err).WithField("infohash", m.ResourceID).Warn("failed to forget an evicted upload")
	}
}
