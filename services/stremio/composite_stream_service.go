package stremio

import (
	"context"
	"sync"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// CompositeStreamService aggregates multiple stream services and makes parallel requests
type CompositeStreamService struct {
	services []StreamService
}

// Ensure CompositeStreamService implements StreamService
var _ StreamService = (*CompositeStreamService)(nil)

// NewCompositeStreamService creates a new composite stream service with the given list of services
func NewCompositeStreamService(services []StreamService) *CompositeStreamService {
	return &CompositeStreamService{
		services: services,
	}
}

// GetName returns the name of this composite stream service for logging purposes
func (c *CompositeStreamService) GetName() string {
	return "CompositeStreamService"
}

// GetStreams performs parallel requests to all inner StreamServices and merges results
func (c *CompositeStreamService) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
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
		go func(index int, svc StreamService) {
			defer wg.Done()

			resp, err := svc.GetStreams(ctx, contentType, contentID)
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
					Warn("StreamService request timed out, dropping results")
			} else {
				log.WithError(res.err).
					WithField("service_name", serviceName).
					Warn("StreamService request failed, dropping results")
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
