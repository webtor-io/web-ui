package backends

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
)

// Webtor implements Backend interface for Webtor
type Webtor struct {
	api      *api.Api
	storeMap *lazymap.LazyMap[*ra.ResourceResponse]
	bgMap    *lazymap.LazyMap[*ra.ResourceResponse]
}

// NewWebtor creates a new Webtor backend
func NewWebtor(apiService *api.Api) *Webtor {
	return &Webtor{
		api: apiService,
		storeMap: lazymap.New[*ra.ResourceResponse](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		bgMap: lazymap.New[*ra.ResourceResponse](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}

// EnsureResource registers the bare (hash-only) magnet with rest-api when
// it isn't already aware of the resource. Idempotent — concurrent calls
// for the same hash coalesce into a single StoreResource. If the inline
// call hits the request deadline, a background goroutine retries with a
// longer budget so the resource still ends up registered for next time.
//
// Stremio addons (Torrentio etc.) typically only ship the infohash, so a
// hash-only magnet is what we have. rest-api uses DHT (and any trackers
// the magnet picks up) to fetch torrent metadata.
func (s *Webtor) EnsureResource(ctx context.Context, apiClaims *api.Claims, hash string) error {
	res, err := s.api.GetResourceCached(ctx, apiClaims, hash)
	if err != nil {
		return errors.Wrap(err, "failed to get resource from API")
	}
	if res != nil {
		return nil
	}
	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)
	_, err = s.storeMap.Get(hash, func() (*ra.ResourceResponse, error) {
		return s.api.StoreResource(ctx, apiClaims, []byte(magnet))
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			go s.backgroundStore(apiClaims, hash, magnet)
			return nil
		}
		return errors.Wrap(err, "failed to store resource")
	}
	return nil
}

func (s *Webtor) backgroundStore(apiClaims *api.Claims, hash, magnet string) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, _ = s.bgMap.Get(hash, func() (*ra.ResourceResponse, error) {
		return s.api.StoreResource(bgCtx, apiClaims, []byte(magnet))
	})
}

// getExportItem asks rest-api for the export descriptor of the file at the
// given fileIdx. rest-api accepts a numeric content_id as a file-index
// shorthand into the torrent's natural file order, so we skip the /list
// round-trip we'd otherwise need to translate idx → SHA1.
func (s *Webtor) getExportItem(ctx context.Context, apiClaims *api.Claims, hash string, fileIdx int, exportType string) (*ra.ExportItem, error) {
	exportResp, err := s.api.ExportResourceContent(ctx, apiClaims, hash, strconv.Itoa(fileIdx), "")
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

// ResolveLink generates a webtor streaming link with cached status.
// Ensures the resource is known to rest-api first so the export call
// can succeed even on a freshly-seen torrent.
func (s *Webtor) ResolveLink(ctx context.Context, apiClaims *api.Claims, hash string, fileIdx int) (string, bool, error) {
	log.WithFields(log.Fields{
		"hash":     hash,
		"file_idx": fileIdx,
	}).Debug("resolving webtor link")

	if err := s.EnsureResource(ctx, apiClaims, hash); err != nil {
		return "", false, errors.Wrap(err, "failed to ensure resource")
	}

	item, err := s.getExportItem(ctx, apiClaims, hash, fileIdx, "download")
	if err != nil {
		return "", false, errors.Wrap(err, "failed to get download export item")
	}
	cached := false
	if item.Meta != nil {
		cached = item.Meta.Cache
	}

	log.WithFields(log.Fields{
		"hash":     hash,
		"file_idx": fileIdx,
		"url":      item.URL,
		"cached":   cached,
	}).Info("generated webtor link")

	return item.URL, cached, nil
}
