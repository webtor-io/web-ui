package backends

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
)

// Webtor implements Backend interface for Webtor
type Webtor struct {
	api               *api.Api
	availabilityCache lazymap.LazyMap[bool]
}

// NewWebtor creates a new Webtor backend
func NewWebtor(apiService *api.Api) *Webtor {
	return &Webtor{
		api: apiService,
		availabilityCache: lazymap.New[bool](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 30 * time.Second,
		}),
	}
}

func (s *Webtor) getExportItem(ctx context.Context, apiClaims *api.Claims, hash, path, exportType string) (*ra.ExportItem, error) {
	// List resource content to find the file at the given path
	listArgs := &api.ListResourceContentArgs{
		Limit:  100,
		Offset: 0,
	}

	var targetItem *ra.ListItem

	// Paginate through results to find the file at the specified path
	for {
		resp, err := s.api.ListResourceContentCached(ctx, apiClaims, hash, listArgs)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list resource content")
		}

		for _, item := range resp.Items {
			if item.Type == ra.ListTypeFile && item.PathStr == path {
				targetItem = &item
				break
			}
		}

		if targetItem != nil {
			break
		}

		// Check if we've reached the end
		if (resp.Count - int(listArgs.Offset)) == len(resp.Items) {
			break
		}

		listArgs.Offset += listArgs.Limit
	}

	if targetItem == nil {
		return nil, fmt.Errorf("file not found at path: %s", path)
	}

	// Export the resource content to get metadata
	exportResp, err := s.api.ExportResourceContent(ctx, apiClaims, hash, targetItem.ID, "")
	if err != nil {
		return nil, errors.Wrap(err, "failed to export resource content")
	}

	if exportResp.ExportItems == nil {
		return nil, fmt.Errorf("no export items returned")
	}

	item, exists := exportResp.ExportItems[exportType]

	if !exists {
		return nil, fmt.Errorf("%s export item not found", exportType)
	}
	return &item, nil
}

// checkAvailabilityCached is a cached variant that uses LazyMap to cache availability check results
func (s *Webtor) checkAvailabilityCached(ctx context.Context, apiClaims *api.Claims, hash, path string) (bool, error) {
	cacheKey := fmt.Sprintf("%s:%s", hash, path)

	log.WithFields(log.Fields{
		"hash":      hash,
		"path":      path,
		"cache_key": cacheKey,
	}).Debug("checking webtor availability with cache")

	return s.availabilityCache.Get(cacheKey, func() (bool, error) {
		log.WithFields(log.Fields{
			"hash": hash,
			"path": path,
		}).Debug("cache miss, performing actual availability check")

		item, err := s.getExportItem(ctx, apiClaims, hash, path, "download")
		if err != nil {
			return false, errors.Wrap(err, "failed to get download export item")
		}
		// Extract cached state from meta
		cached := false
		if item.Meta != nil {
			cached = item.Meta.Cache
		}

		log.WithFields(log.Fields{
			"hash":   hash,
			"path":   path,
			"cached": cached,
		}).Debug("availability check completed, caching result")

		return cached, nil
	})
}

// CheckAvailability checks if content is available in Webtor
func (s *Webtor) CheckAvailability(ctx context.Context, apiClaims *api.Claims, hash, path string) (bool, error) {
	return s.checkAvailabilityCached(ctx, apiClaims, hash, path)
}

// ResolveLink generates a webtor streaming link with cached status
func (s *Webtor) ResolveLink(ctx context.Context, apiClaims *api.Claims, hash, path string) (string, bool, error) {
	log.WithFields(log.Fields{
		"hash": hash,
		"path": path,
	}).Debug("resolving webtor link")

	item, err := s.getExportItem(ctx, apiClaims, hash, path, "download")
	if err != nil {
		return "", false, errors.Wrap(err, "failed to get download export item")
	}
	// Extract cached state from meta
	cached := false
	if item.Meta != nil {
		cached = item.Meta.Cache
	}

	log.WithFields(log.Fields{
		"hash":   hash,
		"path":   path,
		"url":    item.URL,
		"cached": cached,
	}).Info("generated webtor link")

	return item.URL, cached, nil
}
