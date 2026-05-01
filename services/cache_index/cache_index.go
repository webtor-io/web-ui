package cache_index

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"

	log "github.com/sirupsen/logrus"
)

const (
	cacheExpireFlag = "cache-index-expire"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.DurationFlag{
			Name:   cacheExpireFlag,
			Usage:  "cache index expiration time",
			Value:  12 * time.Hour,
			EnvVar: "CACHE_INDEX_EXPIRE",
		},
	)
}

type CacheIndex struct {
	pg            *cs.PG
	cacheExpire   time.Duration
	markCachedMap *lazymap.LazyMap[bool]
	isCachedMap   *lazymap.LazyMap[[]models.CacheIndexResult]
}

func New(c *cli.Context, pg *cs.PG) *CacheIndex {
	return &CacheIndex{
		pg:          pg,
		cacheExpire: c.Duration(cacheExpireFlag),
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

// MarkAsCached records that the file (resourceID, fileIdx) is currently
// streamable on the given backend.
func (s *CacheIndex) MarkAsCached(ctx context.Context, backendType models.StreamingBackendType, resourceID string, fileIdx int) error {
	key := fmt.Sprintf("mark:%s:%s:%d", backendType, resourceID, fileIdx)
	_, err := s.markCachedMap.Get(key, func() (bool, error) {
		db := s.pg.Get()
		if db == nil {
			return false, errors.New("database connection not available")
		}
		err := models.MarkAsCached(ctx, db, backendType, resourceID, fileIdx)
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

// IsCached returns a list of backend types and their last seen times for the
// given resource + file index.
func (s *CacheIndex) IsCached(ctx context.Context, resourceID string, fileIdx int) ([]models.CacheIndexResult, error) {
	key := fmt.Sprintf("is:%s:%d", resourceID, fileIdx)
	return s.isCachedMap.Get(key, func() ([]models.CacheIndexResult, error) {
		db := s.pg.Get()
		if db == nil {
			return nil, errors.New("database connection not available")
		}
		results, err := models.IsCached(ctx, db, resourceID, fileIdx, s.cacheExpire)
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

	rowsAffected, err := models.DeleteOldCacheEntries(ctx, db, s.cacheExpire)
	if err != nil {
		log.WithError(err).Error("failed to delete old cache entries")
		return
	}

	if rowsAffected > 0 {
		log.WithField("rows_affected", rowsAffected).Info("cleaned up old cache index entries")
	}
}
