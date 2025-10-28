package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	ci "github.com/webtor-io/web-ui/services/cache_index"
)

// PrefetchCacheStream wraps PreferredStream and populates cache index
// by checking external addons (like Torrentio) for cached content
type PrefetchCacheStream struct {
	inner        StreamsService
	client       *http.Client
	pg           *cs.PG
	u            *auth.User
	cacheIndex   *ci.CacheIndex
	addonBaseURL string
	userAgent    string
	addonCache   lazymap.LazyMap[*StreamsResponse]
	api          *api.Api
	claims       *api.Claims
}

// Ensure PrefetchCacheStream implements StreamsService
var _ StreamsService = (*PrefetchCacheStream)(nil)

// NewPrefetchCacheStream creates a new populate cache stream service
func NewPrefetchCacheStream(inner StreamsService, client *http.Client, pg *cs.PG, u *auth.User, cacheIndex *ci.CacheIndex, addonBaseURL string, userAgent string, api *api.Api, claims *api.Claims) *PrefetchCacheStream {
	return &PrefetchCacheStream{
		inner:        inner,
		client:       client,
		pg:           pg,
		u:            u,
		cacheIndex:   cacheIndex,
		addonBaseURL: addonBaseURL,
		userAgent:    userAgent,
		api:          api,
		claims:       claims,
		addonCache: lazymap.New[*StreamsResponse](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}

// GetName returns the name of this stream service for logging purposes
func (s *PrefetchCacheStream) GetName() string {
	return "PopulateCache" + s.inner.GetName()
}

// GetStreams fetches streams and populates cache index from external addons
func (s *PrefetchCacheStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	streams, err := s.inner.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	addonUrls, err := models.GetUserStremioAddonUrls(ctx, db, s.u.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user stremio addon urls")
	}
	found := false
	for _, addonURL := range addonUrls {
		if strings.Contains(addonURL.Url, s.addonBaseURL) {
			found = true
			break
		}
	}
	if !found {
		log.WithField("addon_base_url", s.addonBaseURL).Debug("no addon base URL found in user addon urls, skipping cache population")
		return streams, nil
	}
	err = s.prefetchCacheIndex(ctx, contentType, contentID, streams)
	if err != nil {
		// Log error but continue - cache population is not critical
		log.WithError(err).
			WithField("content_type", contentType).
			WithField("content_id", contentID).
			Warn("failed to populate cache index from external addons")
	}

	return streams, nil
}

// prefetchCacheIndex checks external addons for cached content and updates cache index
func (s *PrefetchCacheStream) prefetchCacheIndex(ctx context.Context, contentType, contentID string, streams *StreamsResponse) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("database not initialized")
	}

	// Get user's enabled streaming backends
	backends, err := models.GetUserStreamingBackends(ctx, db, s.u.ID)
	if err != nil {
		return errors.Wrap(err, "failed to get user streaming backends")
	}

	// Filter to only enabled backends that are not webtor
	var enabledBackends []*models.StreamingBackend
	for _, backend := range backends {
		if backend.Enabled {
			enabledBackends = append(enabledBackends, backend)
		}
	}

	if len(enabledBackends) == 0 {
		log.Debug("no enabled non-webtor backends found, skipping cache population")
		return nil
	}

	// For each enabled backend, fetch cache status from addon
	for _, backend := range enabledBackends {
		err := s.checkBackendCache(ctx, backend, contentType, contentID, streams)
		if err != nil {
			// Log but continue with other backends
			log.WithError(err).
				WithField("backend_type", backend.Type).
				WithField("content_type", contentType).
				WithField("content_id", contentID).
				Warn("failed to check backend cache")
		}
	}

	return nil
}

// checkBackendCache checks a specific backend for cached content via addon
func (s *PrefetchCacheStream) checkBackendCache(ctx context.Context, backend *models.StreamingBackend, contentType, contentID string, innerStreams *StreamsResponse) error {
	// Map backend type to addon provider parameter
	provider := s.getAddonProvider(backend.Type)
	if provider == "" {
		log.WithField("backend_type", backend.Type).Debug("no addon provider mapping found")
		return nil
	}

	// Construct addon URL with access token
	// Format: {baseURL}/{provider}={token}/stream/{contentType}/{contentID}.json
	addonURL := fmt.Sprintf("%s/%s=%s", s.addonBaseURL, provider, backend.AccessToken)

	// Fetch streams from addon with caching
	streams, err := s.fetchAddonStreams(ctx, addonURL, contentType, contentID)
	if err != nil {
		return errors.Wrap(err, "failed to fetch addon streams")
	}

	// Parse streams and update cache index
	err = s.parseAndMarkCached(ctx, streams, backend.Type, innerStreams)
	if err != nil {
		return errors.Wrap(err, "failed to parse and mark cached")
	}

	return nil
}

// getAddonProvider maps backend type to addon provider parameter
func (s *PrefetchCacheStream) getAddonProvider(backendType models.StreamingBackendType) string {
	switch backendType {
	case models.StreamingBackendTypeRealDebrid:
		return "realdebrid"
	case models.StreamingBackendTypeTorbox:
		return "torbox"
	default:
		return ""
	}
}

// fetchAddonStreams fetches streams from an addon endpoint with caching
func (s *PrefetchCacheStream) fetchAddonStreams(
	ctx context.Context,
	addonURL,
	contentType,
	contentID string,
) (*StreamsResponse, error) {
	// Create cache key
	cacheKey := fmt.Sprintf("%s%s%s", addonURL, contentType, contentID)

	return s.addonCache.Get(cacheKey, func() (*StreamsResponse, error) {
		return s.doFetchAddonStreams(ctx, addonURL, contentType, contentID)
	})
}

// doFetchAddonStreams performs the actual HTTP request to the addon endpoint
func (s *PrefetchCacheStream) doFetchAddonStreams(
	ctx context.Context,
	addonURL,
	contentType,
	contentID string,
) (*StreamsResponse, error) {
	// Construct the stream endpoint URL
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
		return nil, errors.Wrap(err, "failed to execute request")
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status code: %d", resp.StatusCode)

	}

	// Parse JSON response
	var streamsResp StreamsResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamsResp); err != nil {
		return nil, errors.Wrap(err, "failed to decode JSON response")
	}

	return &streamsResp, nil
}

// extractInfoFromURL extracts InfoHash and fileIdx from stream URL
// URL format: https://torrentio.strem.fun/resolve/{provider}/{token}/{infoHash}/null/{fileIdx}/{filename}
// Returns: infoHash, fileIdx, error
func (s *PrefetchCacheStream) extractInfoFromURL(streamURL string) (string, int, error) {
	if streamURL == "" {
		return "", 0, errors.New("empty URL")
	}

	// Split URL by "/"
	parts := strings.Split(streamURL, "/")

	// We need at least: scheme, empty, domain, resolve, provider, token, infoHash, null, fileIdx
	// Minimum parts: 9
	if len(parts) < 9 {
		return "", 0, errors.New("invalid URL format: not enough parts")
	}

	// Find "resolve" part to anchor our parsing
	resolveIdx := -1
	for i, part := range parts {
		if part == "resolve" {
			resolveIdx = i
			break
		}
	}

	if resolveIdx == -1 || resolveIdx+5 >= len(parts) {
		return "", 0, errors.New("invalid URL format: 'resolve' not found or insufficient parts")
	}

	// InfoHash is 3 positions after "resolve": resolve/{provider}/{token}/{infoHash}
	infoHash := parts[resolveIdx+3]

	// Validate InfoHash (should be 40 hex characters)
	if len(infoHash) != 40 {
		return "", 0, errors.New("invalid InfoHash length")
	}

	// FileIdx is at position resolveIdx+5
	// Format: /resolve/{provider}/{token}/{infoHash}/null/{fileIdx}/{filename...}
	fileIdxStr := parts[resolveIdx+5]
	var fileIdx int
	if fileIdxStr == "undefined" {
		fileIdx = 0
	} else {
		var err error
		fileIdx, err = strconv.Atoi(fileIdxStr)
		if err != nil {
			return "", 0, errors.Wrap(err, "failed to parse fileIdx")
		}
	}

	return infoHash, fileIdx, nil
}

// makeMagnetURL creates a magnet URL from InfoHash and Sources
func (s *PrefetchCacheStream) makeMagnetURL(infohash string) string {
	magnetURL := fmt.Sprintf("magnet:?xt=urn:btih:%s", infohash)
	return magnetURL
}

// getFilePathFromInfoHash converts fileIdx to file path by querying the API
func (s *PrefetchCacheStream) getFilePathFromInfoHash(ctx context.Context, infoHash string, fileIdx int) (string, error) {
	resource, err := s.api.GetResourceCached(ctx, s.claims, infoHash)
	if err != nil {
		return "", errors.Wrap(err, "failed to get resource from API")
	}
	if resource == nil {
		return "", errors.New("resource not found")
	}
	listArgs := &api.ListResourceContentArgs{
		Limit:  100,
		Offset: 0,
	}

	var targetItem *ra.ListItem
	var idx int

	// Paginate through results to find the file at the specified index
	for {
		resp, err := s.api.ListResourceContentCached(ctx, s.claims, infoHash, listArgs)
		if err != nil {
			return "", err
		}

		for _, item := range resp.Items {
			if item.Type == ra.ListTypeFile {
				if idx == fileIdx {
					targetItem = &item
					break
				}
				idx++
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
		return "", fmt.Errorf("file at index %d not found", fileIdx)
	}

	return targetItem.PathStr, nil
}

// parseAndMarkCached parses streams and marks cached items in cache index
func (s *PrefetchCacheStream) parseAndMarkCached(ctx context.Context, streams *StreamsResponse, backendType models.StreamingBackendType, innerStreams *StreamsResponse) error {
	if streams == nil || len(streams.Streams) == 0 {
		return nil
	}

	wg := sync.WaitGroup{}
	sCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, stream := range streams.Streams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.markAsCached(sCtx, stream, backendType, innerStreams)
			if err != nil {
				log.WithError(err).
					WithField("backend_type", backendType).
					WithField("stream_name", stream.Name).
					Error("failed to mark stream as cached")
				return
			}
		}()
	}
	wg.Wait()

	return nil
}

func (s *PrefetchCacheStream) markAsCached(ctx context.Context, stream StreamItem, backendType models.StreamingBackendType, streams *StreamsResponse) error {
	// Get cache marker for this backend type
	cacheMarker := s.getCacheMarker(backendType)

	if !strings.Contains(stream.Name, cacheMarker) {
		return nil
	}

	// Extract InfoHash and fileIdx from URL
	infoHash, fileIdx, err := s.extractInfoFromURL(stream.Url)
	if err != nil {
		return err
	}

	found := false
	for _, st := range streams.Streams {
		if st.InfoHash == infoHash {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("stream with InfoHash %s not found in inner streams", infoHash)
	}
	// Get file path from API using fileIdx
	path, err := s.getFilePathFromInfoHash(ctx, infoHash, fileIdx)
	if err != nil {
		return err
	}

	// Mark as cached in cache index
	err = s.cacheIndex.MarkAsCached(ctx, backendType, path, infoHash)
	if err != nil {
		return err
	}
	return nil
}

// getCacheMarker returns the cache marker string for a backend type
func (s *PrefetchCacheStream) getCacheMarker(backendType models.StreamingBackendType) string {
	switch backendType {
	case models.StreamingBackendTypeRealDebrid:
		return "[RD+]"
	case models.StreamingBackendTypeTorbox:
		return "[TB+]"
	default:
		return ""
	}
}
