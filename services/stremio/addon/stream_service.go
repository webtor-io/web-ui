package addon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/stremio"
)

// StreamService handles requests to Stremio addon stream endpoints
type StreamService struct {
	client *http.Client
	cache  lazymap.LazyMap[*stremio.StreamsResponse]
}

// NewStreamService creates a new stream service instance
func NewStreamService(cl *http.Client) *StreamService {
	return &StreamService{
		client: cl,
		cache: lazymap.New[*stremio.StreamsResponse](&lazymap.Config{
			Expire:      1 * time.Minute,  // Cache for 1 minute as required
			ErrorExpire: 10 * time.Second, // Cache errors for 10 seconds
		}),
	}
}

// GetStreams fetches streams from a Stremio addon endpoint with caching
func (s *StreamService) GetStreams(ctx context.Context, addonURL, contentType, contentID string) (*stremio.StreamsResponse, error) {
	// Create cache key from URL components
	cacheKey := fmt.Sprintf("%s_%s_%s", addonURL, contentType, contentID)

	return s.cache.Get(cacheKey, func() (*stremio.StreamsResponse, error) {
		return s.fetchStreams(ctx, addonURL, contentType, contentID)
	})
}

// fetchStreams performs the actual HTTP request to the addon endpoint
func (s *StreamService) fetchStreams(ctx context.Context, addonURL, contentType, contentID string) (*stremio.StreamsResponse, error) {
	// Construct the stream endpoint URL
	// Format: {addonURL}/stream/{contentType}/{contentID}.json
	streamURL := fmt.Sprintf("%s/stream/%s/%s.json", addonURL, contentType, contentID)

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	// Set appropriate headers
	req.Header.Set("Accept", "application/json")

	// Execute the request
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute request")
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("addon returned status %d", resp.StatusCode)
	}

	// Parse JSON response
	var streamsResp stremio.StreamsResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamsResp); err != nil {
		return nil, errors.Wrap(err, "failed to decode JSON response")
	}

	return &streamsResp, nil
}
