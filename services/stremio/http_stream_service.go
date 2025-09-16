package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/lazymap"
)

// HTTPStreamService handles requests to Stremio addon stream endpoints
type HTTPStreamService struct {
	client   *http.Client
	addonURL string
	cache    lazymap.LazyMap[*StreamsResponse]
}

// Ensure HTTPStreamService implements StreamService
var _ StreamService = (*HTTPStreamService)(nil)

// NewHTTPStreamService creates a new HTTP stream service instance
func NewHTTPStreamService(cl *http.Client, addonURL string) *HTTPStreamService {
	return &HTTPStreamService{
		client:   cl,
		addonURL: addonURL,
		cache: lazymap.New[*StreamsResponse](&lazymap.Config{
			Expire:      1 * time.Minute,  // Cache for 1 minute as required
			ErrorExpire: 10 * time.Second, // Cache errors for 10 seconds
		}),
	}
}

// GetName returns the name of this stream service for logging purposes
func (s *HTTPStreamService) GetName() string {
	return fmt.Sprintf("HTTPStreamService (%v)", s.addonURL)
}

// GetStreams fetches streams from a Stremio addon endpoint with caching
func (s *HTTPStreamService) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	// Create cache key from URL components
	cacheKey := fmt.Sprintf("%s_%s_%s", s.addonURL, contentType, contentID)

	return s.cache.Get(cacheKey, func() (*StreamsResponse, error) {
		return s.fetchStreams(ctx, s.addonURL, contentType, contentID)
	})
}

// fetchStreams performs the actual HTTP request to the addon endpoint
func (s *HTTPStreamService) fetchStreams(ctx context.Context, addonURL, contentType, contentID string) (*StreamsResponse, error) {
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
	var streamsResp StreamsResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamsResp); err != nil {
		return nil, errors.Wrap(err, "failed to decode JSON response")
	}

	return &streamsResp, nil
}
