package stremio

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
)

// PrefetchResourceStream wraps an APIService to ensure resources exist before returning them
type PrefetchResourceStream struct {
	inner                 StreamsService
	api                   *api.Api
	storeResourceMap      lazymap.LazyMap[*ra.ResourceResponse]
	backgroundResourceMap lazymap.LazyMap[*ra.ResourceResponse]
	cla                   *api.Claims
}

var _ StreamsService = (*PrefetchResourceStream)(nil)

// NewPrefetchResourceStream creates a new PrefetchResourceStream wrapper
func NewPrefetchResourceStream(inner StreamsService, api *api.Api, cla *api.Claims) *PrefetchResourceStream {
	return &PrefetchResourceStream{
		inner: inner,
		backgroundResourceMap: lazymap.New[*ra.ResourceResponse](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		api: api,
		cla: cla,
		storeResourceMap: lazymap.New[*ra.ResourceResponse](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}

// GetName returns the name of this stream service for logging purposes
func (s *PrefetchResourceStream) GetName() string {
	return "PopulateResource" + s.inner.GetName()
}

// GetStreams fetches streams and populates resources from api
func (s *PrefetchResourceStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	// Get streams from inner service
	response, err := s.inner.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}

	if response == nil || len(response.Streams) == 0 {
		return response, nil
	}

	prefetchedStreams := make([]*StreamItem, len(response.Streams))
	pCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	wg := sync.WaitGroup{}
	for i, stream := range response.Streams {
		wg.Add(1)
		go func(index int, si *StreamItem) {
			defer wg.Done()
			streamCopy := *si
			err = s.prefetchResource(pCtx, streamCopy)
			if err != nil {
				log.WithError(err).
					WithField("infohash", stream.InfoHash).
					Warn("failed to populate resource")
			} else {
				prefetchedStreams[index] = &streamCopy
			}
		}(i, &stream)
	}
	wg.Wait()

	// Filter out empty streams (those that were dropped due to timeout)
	var finalStreams []StreamItem
	for _, stream := range prefetchedStreams {
		// Check if stream was dropped (empty InfoHash indicates dropped stream)
		if stream != nil {
			finalStreams = append(finalStreams, *stream)
		}
	}

	return &StreamsResponse{Streams: finalStreams}, nil
}

// backgroundStoreResource attempts to store a resource in the background with extended timeout
func (s *PrefetchResourceStream) backgroundStoreResource(c *api.Claims, infoHash, magnetURL string) error {
	// Create a background context with 5-minutes timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, err := s.backgroundResourceMap.Get(infoHash, func() (*ra.ResourceResponse, error) {
		return s.api.StoreResource(ctx, c, []byte(magnetURL))
	})
	return err
}

// makeMagnetURL creates a magnet URL from InfoHash and Sources
func (s *PrefetchResourceStream) makeMagnetURL(infohash string, sources []string) string {
	magnetURL := fmt.Sprintf("magnet:?xt=urn:btih:%s", infohash)

	// Add trackers from sources
	for _, source := range sources {
		if strings.TrimSpace(source) != "" {
			// URL encode the tracker
			encoded := url.QueryEscape(source)
			magnetURL += "&tr=" + encoded
		}
	}

	return magnetURL
}

func (s *PrefetchResourceStream) prefetchResource(ctx context.Context, stream StreamItem) error {
	resource, err := s.api.GetResourceCached(ctx, s.cla, stream.InfoHash)
	if err != nil {
		return errors.Wrap(err, "failed to get resource from API")
	}
	if resource != nil {
		return nil
	}
	// Make magnet URL and store it in API using lazymap
	magnetURL := s.makeMagnetURL(stream.InfoHash, stream.Sources)

	_, err = s.storeResourceMap.Get(stream.InfoHash, func() (*ra.ResourceResponse, error) {
		return s.api.StoreResource(ctx, s.cla, []byte(magnetURL))
	})
	if err != nil {
		// Check if the error is due to context deadline
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// Start background goroutine to retry with longer timeout
			go func() {
				log.WithField("infohash", stream.InfoHash).
					Info("started resource store in background")
				err := s.backgroundStoreResource(s.cla, stream.InfoHash, magnetURL)
				if err != nil {
					log.WithError(err).
						WithField("infohash", stream.InfoHash).
						Warn("failed to store resource in background")
					return
				}
				log.WithField("infohash", stream.InfoHash).
					Info("resource stored in background")
			}()
			return nil
		}
		return errors.Wrap(err, "failed to store resource")
	}
	return nil
}
