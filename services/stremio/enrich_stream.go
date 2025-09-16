package stremio

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
)

// APIService defines the interface for API operations needed by EnrichStream
type APIService interface {
	GetResource(ctx context.Context, c *api.Claims, infohash string) (*ra.ResourceResponse, error)
	StoreResource(ctx context.Context, c *api.Claims, resource []byte) (*ra.ResourceResponse, error)
	ListResourceContentCached(ctx context.Context, c *api.Claims, infohash string, args *api.ListResourceContentArgs) (*ra.ListResponse, error)
	ExportResourceContent(ctx context.Context, c *api.Claims, infohash string, itemID string, imdbID string) (*ra.ExportResponse, error)
}

// EnrichStream wraps another StreamsService to enrich streams with URLs
type EnrichStream struct {
	wrapped StreamsService
	api     APIService
	claims  *api.Claims
}

// NewEnrichStream creates a new EnrichStream service
func NewEnrichStream(wrapped StreamsService, api APIService, claims *api.Claims) *EnrichStream {
	return &EnrichStream{
		wrapped: wrapped,
		api:     api,
		claims:  claims,
	}
}

// GetName returns the name of the wrapped service with "Enrich" prefix
func (e *EnrichStream) GetName() string {
	return "Enrich" + e.wrapped.GetName()
}

// GetStreams gets streams from the wrapped service and enriches them with URLs
func (e *EnrichStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	// Get streams from wrapped service
	response, err := e.wrapped.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}

	if response == nil || len(response.Streams) == 0 {
		return response, nil
	}

	// Enrich streams in parallel while preserving order
	enrichedStreams := make([]*StreamItem, len(response.Streams))
	var wg sync.WaitGroup

	for i, stream := range response.Streams {
		wg.Add(1)
		go func(index int, s *StreamItem) {
			defer wg.Done()

			// Create context with 10-second timeout
			streamCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			// Create a copy of the stream to avoid potential shared memory issues
			streamCopy := *s
			enriched := e.enrichStream(streamCtx, &streamCopy)
			enrichedStreams[index] = enriched
		}(i, &stream)
	}

	wg.Wait()

	// Filter out empty streams (those that were dropped due to timeout)
	var finalStreams []StreamItem
	for _, stream := range enrichedStreams {
		// Check if stream was dropped (empty InfoHash indicates dropped stream)
		if stream != nil {
			finalStreams = append(finalStreams, *stream)
		}
	}

	return &StreamsResponse{Streams: finalStreams}, nil
}

// enrichStream enriches a single stream with URL if needed
func (e *EnrichStream) enrichStream(ctx context.Context, stream *StreamItem) *StreamItem {
	// Step 1: Check if there is URL in stream
	if stream.Url != "" {
		return stream
	}

	// Check if we have InfoHash
	if stream.InfoHash == "" {
		log.WithField("stream_title", stream.Title).
			Warn("stream has no InfoHash, dropping stream")
		return nil
	}

	// Step 2: Check with webtor API if it has resource with infohash
	resource, err := e.api.GetResource(ctx, e.claims, stream.InfoHash)
	if err != nil {
		log.WithError(err).
			WithField("infohash", stream.InfoHash).
			WithField("stream_title", stream.Title).
			Warn("failed to check resource, dropping stream")
		return nil
	}

	// If resource doesn't exist, store it
	if resource == nil {
		// Step 3: Make magnet URL and store it in API
		magnetURL := e.makeMagnetURL(stream.InfoHash, stream.Sources)

		log.WithField("infohash", stream.InfoHash).
			WithField("magnet_url", magnetURL).
			Debug("storing resource for stream")

		_, err := e.api.StoreResource(ctx, e.claims, []byte(magnetURL))
		if err != nil {
			// Check if the error is due to context deadline
			if ctx.Err() == context.DeadlineExceeded {
				log.WithField("infohash", stream.InfoHash).
					WithField("magnet_url", magnetURL).
					Info("storeResource failed due to timeout, scheduling background retry")

				// Start background goroutine to retry with longer timeout
				go e.backgroundStoreResource(stream.InfoHash, magnetURL)
			} else {
				log.WithError(err).
					WithField("infohash", stream.InfoHash).
					WithField("magnet_url", magnetURL).
					Warn("failed to store resource")
			}

			// Drop stream for current request but allow background retry
			return nil
		}
	}

	// Step 4: Generate URL with webtor's API using FileIdx
	url, err := e.generateStreamURL(ctx, stream.InfoHash, stream.FileIdx)
	if err != nil {
		log.WithError(err).
			WithField("infohash", stream.InfoHash).
			WithField("file_idx", stream.FileIdx).
			Warn("failed to generate stream URL, dropping stream")
		return nil
	}

	// Step 5: Add generated URL to Stream
	stream.Url = url
	return stream
}

// backgroundStoreResource attempts to store a resource in the background with extended timeout
func (e *EnrichStream) backgroundStoreResource(infohash, magnetURL string) {
	// Create a background context with 60-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.WithField("infohash", infohash).
		WithField("magnet_url", magnetURL).
		Debug("starting background resource storage with extended timeout")

	_, err := e.api.StoreResource(ctx, e.claims, []byte(magnetURL))
	if err != nil {
		log.WithError(err).
			WithField("infohash", infohash).
			WithField("magnet_url", magnetURL).
			Warn("background resource storage failed")
	} else {
		log.WithField("infohash", infohash).
			Info("background resource storage completed successfully")
	}
}

// makeMagnetURL creates a magnet URL from InfoHash and Sources
func (e *EnrichStream) makeMagnetURL(infohash string, sources []string) string {
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

// generateStreamURL generates a URL for the stream using the API
func (e *EnrichStream) generateStreamURL(ctx context.Context, infohash string, fileIdx uint8) (string, error) {
	// List resource content to find the file at the given index
	listArgs := &api.ListResourceContentArgs{
		Limit:  100,
		Offset: 0,
	}

	var targetItem *ra.ListItem
	currentIdx := uint8(0)

	// Paginate through results to find the file at the specified index
	for {
		resp, err := e.api.ListResourceContentCached(ctx, e.claims, infohash, listArgs)
		if err != nil {
			return "", err
		}

		for _, item := range resp.Items {
			if item.Type == ra.ListTypeFile {
				if currentIdx == fileIdx {
					targetItem = &item
					break
				}
				currentIdx++
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

	// Export the resource content to get the download URL
	exportResp, err := e.api.ExportResourceContent(ctx, e.claims, infohash, targetItem.ID, "")
	if err != nil {
		return "", err
	}

	if exportResp.ExportItems == nil {
		return "", fmt.Errorf("no export items returned")
	}

	downloadItem, exists := exportResp.ExportItems["download"]
	if !exists {
		return "", fmt.Errorf("download export item not found")
	}

	return downloadItem.URL, nil
}
