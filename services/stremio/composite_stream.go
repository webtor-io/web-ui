package stremio

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	rum "github.com/webtor-io/web-ui/services/request_url_mapper"
)

// CompositeStream aggregates multiple stream services and makes parallel requests
type CompositeStream struct {
	services []StreamsService
}

// Ensure CompositeStream implements StreamsService
var _ StreamsService = (*CompositeStream)(nil)

// NewCompositeStream creates a new composite stream service with the given list of services
func NewCompositeStream(services []StreamsService) *CompositeStream {
	return &CompositeStream{
		services: services,
	}
}

// GetName returns the name of this composite stream service for logging purposes
func (c *CompositeStream) GetName() string {
	return "CompositeStream"
}

// GetStreams performs parallel requests to all inner StreamServices and merges results
func (c *CompositeStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	if len(c.services) == 0 {
		return &StreamsResponse{Streams: []StreamItem{}}, nil
	}

	// Channel to collect results with their original index to maintain order
	type result struct {
		index    int
		response *StreamsResponse
		err      error
	}

	results := make(chan result, len(c.services))
	var wg sync.WaitGroup

	// Launch goroutines for parallel requests
	for i, service := range c.services {
		wg.Add(1)
		go func(index int, svc StreamsService) {
			defer wg.Done()

			// Create context with 5-second timeout for each inner stream
			streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			resp, err := svc.GetStreams(streamCtx, contentType, contentID)
			results <- result{
				index:    index,
				response: resp,
				err:      err,
			}
		}(i, service)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results maintaining order
	orderedResults := make([]*StreamsResponse, len(c.services))

	for res := range results {
		if res.err != nil {
			// Get service name for better logging
			serviceName := "unknown"
			if res.index < len(c.services) {
				serviceName = c.services[res.index].GetName()
			}

			// Log error and continue with other services using global logrus
			if errors.Is(res.err, context.DeadlineExceeded) {
				log.WithError(res.err).
					WithField("service_name", serviceName).
					Warn("StreamsService request timed out, dropping results")
			} else {
				log.WithError(res.err).
					WithField("service_name", serviceName).
					Warn("StreamsService request failed, dropping results")
			}
			continue
		}

		orderedResults[res.index] = res.response
	}

	// Merge all successful responses maintaining order
	var allStreams []StreamItem
	for _, response := range orderedResults {
		if response != nil {
			allStreams = append(allStreams, response.Streams...)
		}
	}

	return &StreamsResponse{Streams: allStreams}, nil
}

// convertManifestURLToBaseURL converts a manifest URL (ending with manifest.json) to base URL
// Example: https://example.com/addon/manifest.json -> https://example.com/addon
func convertManifestURLToBaseURL(manifestURL string) string {
	if strings.HasSuffix(manifestURL, "/manifest.json") {
		return strings.TrimSuffix(manifestURL, "/manifest.json")
	}
	return manifestURL
}

// NewAddonCompositeStreamsByUserID creates a CompositeStream by fetching all addon URLs for a user
func NewAddonCompositeStreamsByUserID(ctx context.Context, db *pg.DB, client *http.Client, userID uuid.UUID, cache *lazymap.LazyMap[*StreamsResponse], userAgent string, requestURLMapper *rum.RequestURLMapper) (*CompositeStream, error) {
	// Get all addon URLs for the user
	addonUrls, err := models.GetUserStremioAddonUrls(ctx, db, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user addon URLs")
	}

	// Create AddonStream instances for each addon URL using the provided cache
	services := make([]StreamsService, 0, len(addonUrls))
	for _, addonUrl := range addonUrls {
		// Convert manifest URL to base URL
		baseURL := convertManifestURLToBaseURL(addonUrl.Url)

		// Create addon stream service with provided cache
		addonService := NewAddonStream(client, baseURL, cache, userAgent, requestURLMapper)
		services = append(services, addonService)
	}

	// Create and return composite service
	return NewCompositeStream(services), nil
}
