package cache_index

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"

	log "github.com/sirupsen/logrus"
)

const (
	cacheExpireFlag       = "cache-index-expire"
	seederCacheExpireFlag = "cache-index-seeder-expire"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.DurationFlag{
			Name:   cacheExpireFlag,
			Usage:  "cache index expiration time",
			Value:  12 * time.Hour,
			EnvVar: "CACHE_INDEX_EXPIRE",
		},
		// Entries written from the seeder's own events are taken back by its
		// events; this is only how long one is believed when the node that
		// wrote it died without a word. Cleaners turn a disk over in days.
		cli.DurationFlag{
			Name:   seederCacheExpireFlag,
			Usage:  "cache index expiration time for entries reported by the seeder",
			Value:  7 * 24 * time.Hour,
			EnvVar: "CACHE_INDEX_SEEDER_EXPIRE",
		},
	)
}

type CacheIndex struct {
	pg            *cs.PG
	windows       models.CacheWindows
	markCachedMap *lazymap.LazyMap[bool]
	isCachedMap   *lazymap.LazyMap[[]models.CacheIndexResult]
}

func New(c *cli.Context, pg *cs.PG) *CacheIndex {
	return &CacheIndex{
		pg: pg,
		windows: models.CacheWindows{
			Probe:  c.Duration(cacheExpireFlag),
			Seeder: c.Duration(seederCacheExpireFlag),
		},
		markCachedMap: lazymap.New[bool](&lazymap.Config{
			Expire:      time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		isCachedMap: lazymap.New[[]models.CacheIndexResult](&lazymap.Config{
			Expire:      time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}

// MarkAsCached records that the backend, asked just now, has the file
// (resourceID, fileIdx). For Webtor the answer covers the seeder and the Vault
// alike, which is why it is kept apart from what the seeder reports itself.
func (s *CacheIndex) MarkAsCached(ctx context.Context, backendType models.StreamingBackendType, resourceID string, fileIdx int) error {
	return s.mark(ctx, backendType, models.CacheSourceProbe, resourceID, fileIdx)
}

// MarkFromSeeder records the seeder's own report that the file is complete on
// its disk.
func (s *CacheIndex) MarkFromSeeder(ctx context.Context, resourceID string, fileIdx int) error {
	return s.mark(ctx, models.StreamingBackendTypeWebtor, models.CacheSourceSeeder, resourceID, fileIdx)
}

func (s *CacheIndex) mark(ctx context.Context, backendType models.StreamingBackendType, source models.CacheSource, resourceID string, fileIdx int) error {
	key := markKey(backendType, source, resourceID) + fmt.Sprint(fileIdx)
	_, err := s.markCachedMap.Get(key, func() (bool, error) {
		db := s.pg.Get()
		if db == nil {
			return false, errors.New("database connection not available")
		}
		err := models.MarkAsCached(ctx, db, backendType, source, resourceID, fileIdx)
		if err != nil {
			return false, errors.Wrap(err, "failed to mark as cached")
		}
		defer func() {
			isCacheKey := fmt.Sprintf("is:%s:%d", resourceID, fileIdx)
			s.isCachedMap.Drop(isCacheKey)
		}()
		return true, nil
	})
	return err
}

func markKey(backendType models.StreamingBackendType, source models.CacheSource, resourceID string) string {
	return fmt.Sprintf("mark:%s:%s:%s:", backendType, source, resourceID)
}

// Unmark records that the backend, asked just now, does NOT have the file --
// so whatever any source said before is over.
func (s *CacheIndex) Unmark(ctx context.Context, backendType models.StreamingBackendType, resourceID string, fileIdx *int) error {
	return s.unmark(ctx, backendType, models.CacheSourceAny, resourceID, fileIdx)
}

// UnmarkFromSeeder records that the file left the seeder's disk; a nil fileIdx
// means the whole torrent did. It takes back only what the seeder reported:
// the same file may be in the Vault, and the seeder does not know.
func (s *CacheIndex) UnmarkFromSeeder(ctx context.Context, resourceID string, fileIdx *int) error {
	return s.unmark(ctx, models.StreamingBackendTypeWebtor, models.CacheSourceSeeder, resourceID, fileIdx)
}

// Not deduplicated like the marks: removals are rare, and a swallowed one
// leaves a false mark until the expiry.
func (s *CacheIndex) unmark(ctx context.Context, backendType models.StreamingBackendType, source models.CacheSource, resourceID string, fileIdx *int) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("database connection not available")
	}
	if err := models.UnmarkCached(ctx, db, backendType, source, resourceID, fileIdx); err != nil {
		return errors.Wrap(err, "failed to unmark cached")
	}
	// The memo maps too, or a mark arriving within the minute would be taken
	// for the one already written and the entry would stay deleted.
	for _, src := range []models.CacheSource{models.CacheSourceProbe, models.CacheSourceSeeder} {
		if source != models.CacheSourceAny && source != src {
			continue
		}
		if fileIdx != nil {
			s.markCachedMap.Drop(markKey(backendType, src, resourceID) + fmt.Sprint(*fileIdx))
		} else {
			dropPrefix(s.markCachedMap, markKey(backendType, src, resourceID))
		}
	}
	if fileIdx != nil {
		s.isCachedMap.Drop(fmt.Sprintf("is:%s:%d", resourceID, *fileIdx))
	} else {
		dropPrefix(s.isCachedMap, fmt.Sprintf("is:%s:", resourceID))
	}
	return nil
}

// IsCached returns a list of backend types and their last seen times for the
// given resource + file index.
func (s *CacheIndex) IsCached(ctx context.Context, resourceID string, fileIdx int) ([]models.CacheIndexResult, error) {
	key := fmt.Sprintf("is:%s:%d", resourceID, fileIdx)
	return s.isCachedMap.Get(key, func() ([]models.CacheIndexResult, error) {
		db := s.pg.Get()
		if db == nil {
			return nil, errors.New("database connection not available")
		}
		results, err := models.IsCached(ctx, db, resourceID, fileIdx, s.windows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to check if cached")
		}
		return results, nil
	})
}

// RunCleanup removes old cache entries from the database
func (s *CacheIndex) RunCleanup(ctx context.Context) {
	db := s.pg.Get()
	if db == nil {
		log.Warn("database connection not available for cache index cleanup")
		return
	}

	rowsAffected, err := models.DeleteOldCacheEntries(ctx, db, s.windows)
	if err != nil {
		log.WithError(err).Error("failed to delete old cache entries")
		return
	}

	if rowsAffected > 0 {
		log.WithField("rows_affected", rowsAffected).Info("cleaned up old cache index entries")
	}
}

func dropPrefix[T any](m *lazymap.LazyMap[T], prefix string) {
	for _, k := range m.Keys() {
		if strings.HasPrefix(k, prefix) {
			m.Drop(k)
		}
	}
}
