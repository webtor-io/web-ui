package event

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/pkg/errors"
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
