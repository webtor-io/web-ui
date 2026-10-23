package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	"github.com/webtor-io/lazymap"
	rum "github.com/webtor-io/web-ui/services/request_url_mapper"
)

// AddonStream handles requests to Stremio addon stream endpoints
type AddonStream struct {
	client           *http.Client
	addonURL         string
	cache            *lazymap.LazyMap[*StreamsResponse]
	userAgent        string
	requestURLMapper *rum.RequestURLMapper
}

// Ensure AddonStream implements StreamsService
var _ StreamsService = (*AddonStream)(nil)

// NewAddonStream creates a new addon stream service instance
func NewAddonStream(cl *http.Client, addonURL string, cache *lazymap.LazyMap[*StreamsResponse], userAgent string, requestURLMapper *rum.RequestURLMapper) *AddonStream {
	return &AddonStream{
		client:           cl,
		addonURL:         addonURL,
		cache:            cache,
		userAgent:        userAgent,
		requestURLMapper: requestURLMapper,
	}
}

// GetName returns the name of this stream service for logging purposes:
// the addon's host only, see addonHost.
func (s *AddonStream) GetName() string {
	return fmt.Sprintf("AddonStream (%v)", addonHost(s.addonURL))
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

	// Apply URL mapping
	if s.requestURLMapper != nil {
		streamURL = s.requestURLMapper.MapURL(streamURL)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return nil, errors.Wrap(hostOnly(err), "failed to create request")
	}

	// Set appropriate headers
	req.Header.Set("Accept", "application/json")
	if s.userAgent != "" {
		req.Header.Set("User-Agent", s.userAgent)
	}

	// Execute the request
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(hostOnly(err), "failed to execute request")
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

// addonHost is an addon URL as logs and errors may carry it: scheme and host.
// A third-party addon keeps its configuration, debrid keys included, in the
// path or query (https://host/<config>/manifest.json), which
// helpers.RedactURL cannot tell from an ordinary path.
func addonHost(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "<redacted>"
	}
	return u.Scheme + "://" + u.Host
}

// hostOnly is err with the URL a *url.Error quotes (the whole of it, both
// when the URL does not parse and when the request fails) cut to addonHost.
// A redirect whose Location does not parse is quoted whole in the inner
// error too: net/http parses Location before CheckRedirect can refuse the
// redirect, and an addon may put its configuration in it.
func hostOnly(err error) error {
	if ue, ok := err.(*url.Error); ok {
		ue.URL = addonHost(ue.URL)
		if ue.Err != nil && strings.HasPrefix(ue.Err.Error(), "failed to parse Location header") {
			ue.Err = errors.New("failed to parse Location header")
		}
	}
	return err
}
