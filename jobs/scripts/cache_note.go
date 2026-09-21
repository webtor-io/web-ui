package scripts

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
)

// CacheIndexer is the part of services/cache_index a start reports to.
type CacheIndexer interface {
	MarkAsCached(ctx context.Context, backendType models.StreamingBackendType, resourceID string, fileIdx int) error
	Unmark(ctx context.Context, backendType models.StreamingBackendType, resourceID string, fileIdx *int) error
}

const cacheNoteTimeout = 5 * time.Second

// noteCache tells the cache index what rest-api has just said about the file:
// every stream and download start on the site already asks ("is it complete in
// the seeder or in Vault", Meta.Cache), and until 2026-09 the answer was used
// for the warm-up decision and thrown away -- the index heard only from links
// resolved for Stremio, 13 entries against 10k site starts a week.
//
// Both answers are reported. "Yes" is the mark. "No" removes one: the index is
// otherwise corrected by the seeder's own events, and a node that died with
// its disk sends none -- the next start of that file is what notices.
//
// Only `cached` from Meta.Cache belongs here, never the seeder fast-path of
// warmUp (head and tail pieces on the pod): that is a warm start, not a file
// that can be served whole.
//
// Off the job's path and best-effort: the index is an optimisation, a start
// must not wait on it or fail with it.
func noteCache(ci CacheIndexer, resourceID string, src ra.ListItem, cached bool) {
	if ci == nil || src.Type != ra.ListTypeFile {
		return
	}
	idx := src.Index
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cacheNoteTimeout)
		defer cancel()
		var err error
		if cached {
			err = ci.MarkAsCached(ctx, models.StreamingBackendTypeWebtor, resourceID, idx)
		} else {
			err = ci.Unmark(ctx, models.StreamingBackendTypeWebtor, resourceID, &idx)
		}
		if err != nil {
			log.WithError(err).WithField("resource_id", resourceID).WithField("file_idx", idx).Warn("failed to update cache index")
		}
	}()
}
