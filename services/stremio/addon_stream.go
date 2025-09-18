package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
)

// AddonStream handles requests to Stremio addon stream endpoints
type AddonStream struct {
	client    *http.Client
	addonURL  string
	cache     lazymap.LazyMap[*StreamsResponse]
	userAgent string
}

// Ensure AddonStream implements StreamsService
var _ StreamsService = (*AddonStream)(nil)

// NewAddonStream creates a new addon stream service instance
func NewAddonStream(cl *http.Client, addonURL string, cache lazymap.LazyMap[*StreamsResponse], userAgent string) *AddonStream {
	return &AddonStream{
		client:    cl,
		addonURL:  addonURL,
		cache:     cache,
		userAgent: userAgent,
	}
}

// GetName returns the name of this stream service for logging purposes
func (s *AddonStream) GetName() string {
	return fmt.Sprintf("AddonStream (%v)", s.addonURL)
}

// GetStreams fetches streams from a Stremio addon endpoint with caching
func (s *AddonStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	// Create cache key from URL components
	cacheKey := fmt.Sprintf("%s_%s_%s", s.addonURL, contentType, contentID)

	return s.cache.Get(cacheKey, func() (*StreamsResponse, error) {
		return s.fetchStreams(ctx, s.addonURL, contentType, contentID)
	})
}

// fetchStreams performs the actual HTTP request to the addon endpoint
func (s *AddonStream) fetchStreams(ctx context.Context, addonURL, contentType, contentID string) (*StreamsResponse, error) {
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
	if s.userAgent != "" {
		req.Header.Set("User-Agent", s.userAgent)
	}

	// Execute the request
	resp, err := s.client.Do(req)
	if err != nil {
		log.WithError(err).
			WithField("service_name", s.GetName()).
			WithField("request_url", streamURL).
			Warn("failed to execute addon stream request")
		return nil, errors.Wrap(err, "failed to execute request")
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		log.WithField("service_name", s.GetName()).
			WithField("request_url", streamURL).
			WithField("status_code", resp.StatusCode).
			Warn("addon returned non-200 status code")
		return nil, errors.Errorf("addon returned status %d", resp.StatusCode)
	}

	// Parse JSON response
	var streamsResp StreamsResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamsResp); err != nil {
		return nil, errors.Wrap(err, "failed to decode JSON response")
	}

	return &streamsResp, nil
}
