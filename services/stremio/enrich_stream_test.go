package stremio

import (
	"context"
	"errors"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
)

// Mock StreamsService
type mockWrappedService struct {
	response *StreamsResponse
	err      error
}

func (m *mockWrappedService) GetName() string {
	return "MockService"
}

func (m *mockWrappedService) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	return m.response, m.err
}

// Mock API
type mockAPI struct {
	getResourceResponse   *ra.ResourceResponse
	getResourceError      error
	storeResourceResponse *ra.ResourceResponse
	storeResourceError    error
	listContentResponse   *ra.ListResponse
	listContentError      error
	exportContentResponse *ra.ExportResponse
	exportContentError    error
}

func (m *mockAPI) GetResource(ctx context.Context, c *api.Claims, infohash string) (*ra.ResourceResponse, error) {
	return m.getResourceResponse, m.getResourceError
}

func (m *mockAPI) StoreResource(ctx context.Context, c *api.Claims, resource []byte) (*ra.ResourceResponse, error) {
	return m.storeResourceResponse, m.storeResourceError
}

func (m *mockAPI) ListResourceContentCached(ctx context.Context, c *api.Claims, infohash string, args *api.ListResourceContentArgs) (*ra.ListResponse, error) {
	return m.listContentResponse, m.listContentError
}

func (m *mockAPI) ExportResourceContent(ctx context.Context, c *api.Claims, infohash string, itemID string, imdbID string) (*ra.ExportResponse, error) {
	return m.exportContentResponse, m.exportContentError
}

func TestEnrichStream_GetName(t *testing.T) {
	wrapped := &mockWrappedService{}
	mockAPI := &mockAPI{}
	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	expected := "EnrichMockService"
	if service.GetName() != expected {
		t.Errorf("Expected name %s, got %s", expected, service.GetName())
	}
}

func TestEnrichStream_GetStreams_EmptyResponse(t *testing.T) {
	wrapped := &mockWrappedService{
		response: &StreamsResponse{Streams: []StreamItem{}},
	}
	mockAPI := &mockAPI{}
	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	ctx := context.Background()
	result, err := service.GetStreams(ctx, "movie", "test")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(result.Streams) != 0 {
		t.Errorf("Expected empty streams, got %d streams", len(result.Streams))
	}
}

func TestEnrichStream_GetStreams_WithExistingURL(t *testing.T) {
	wrapped := &mockWrappedService{
		response: &StreamsResponse{
			Streams: []StreamItem{
				{
					Title:    "Test Stream",
					Url:      "http://existing.url",
					InfoHash: "testhash",
					FileIdx:  0,
				},
			},
		},
	}
	mockAPI := &mockAPI{}
	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	ctx := context.Background()
	result, err := service.GetStreams(ctx, "movie", "test")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(result.Streams) != 1 {
		t.Errorf("Expected 1 stream, got %d streams", len(result.Streams))
	}
	if result.Streams[0].Url != "http://existing.url" {
		t.Errorf("Expected URL to remain unchanged, got %s", result.Streams[0].Url)
	}
}

func TestEnrichStream_GetStreams_NoInfoHash(t *testing.T) {
	wrapped := &mockWrappedService{
		response: &StreamsResponse{
			Streams: []StreamItem{
				{
					Title: "Test Stream",
					Url:   "", // No URL
					// No InfoHash
					FileIdx: 0,
				},
			},
		},
	}
	mockAPI := &mockAPI{}
	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	ctx := context.Background()
	result, err := service.GetStreams(ctx, "movie", "test")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	// Stream should be dropped due to missing InfoHash
	if len(result.Streams) != 0 {
		t.Errorf("Expected no streams (dropped), got %d streams", len(result.Streams))
	}
}

func TestEnrichStream_GetStreams_ResourceExists(t *testing.T) {
	wrapped := &mockWrappedService{
		response: &StreamsResponse{
			Streams: []StreamItem{
				{
					Title:    "Test Stream",
					Url:      "", // No URL
					InfoHash: "testhash",
					FileIdx:  0,
					Sources:  []string{"tracker1", "tracker2"},
				},
			},
		},
	}

	mockAPI := &mockAPI{
		// Resource exists
		getResourceResponse: &ra.ResourceResponse{},
		// Mock list content response
		listContentResponse: &ra.ListResponse{
			Items: []ra.ListItem{
				{
					ID:   "file1",
					Type: ra.ListTypeFile,
				},
			},
			Count: 1,
		},
		// Mock export response
		exportContentResponse: &ra.ExportResponse{
			ExportItems: map[string]ra.ExportItem{
				"download": {
					URL: "http://generated.url",
				},
			},
		},
	}

	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	ctx := context.Background()
	result, err := service.GetStreams(ctx, "movie", "test")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(result.Streams) != 1 {
		t.Errorf("Expected 1 stream, got %d streams", len(result.Streams))
	}
	if result.Streams[0].Url != "http://generated.url" {
		t.Errorf("Expected generated URL, got %s", result.Streams[0].Url)
	}
}

func TestEnrichStream_GetStreams_ResourceDoesNotExist(t *testing.T) {
	wrapped := &mockWrappedService{
		response: &StreamsResponse{
			Streams: []StreamItem{
				{
					Title:    "Test Stream",
					Url:      "", // No URL
					InfoHash: "testhash",
					FileIdx:  0,
					Sources:  []string{"tracker1", "tracker2"},
				},
			},
		},
	}

	mockAPI := &mockAPI{
		// Resource does not exist
		getResourceResponse: nil,
		// Store resource succeeds
		storeResourceResponse: &ra.ResourceResponse{},
		// Mock list content response
		listContentResponse: &ra.ListResponse{
			Items: []ra.ListItem{
				{
					ID:   "file1",
					Type: ra.ListTypeFile,
				},
			},
			Count: 1,
		},
		// Mock export response
		exportContentResponse: &ra.ExportResponse{
			ExportItems: map[string]ra.ExportItem{
				"download": {
					URL: "http://generated.url",
				},
			},
		},
	}

	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	ctx := context.Background()
	result, err := service.GetStreams(ctx, "movie", "test")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(result.Streams) != 1 {
		t.Errorf("Expected 1 stream, got %d streams", len(result.Streams))
	}
	if result.Streams[0].Url != "http://generated.url" {
		t.Errorf("Expected generated URL, got %s", result.Streams[0].Url)
	}
}

func TestEnrichStream_GetStreams_APIError(t *testing.T) {
	wrapped := &mockWrappedService{
		response: &StreamsResponse{
			Streams: []StreamItem{
				{
					Title:    "Test Stream",
					Url:      "", // No URL
					InfoHash: "testhash",
					FileIdx:  0,
				},
			},
		},
	}

	mockAPI := &mockAPI{
		getResourceError: errors.New("API error"),
	}

	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	ctx := context.Background()
	result, err := service.GetStreams(ctx, "movie", "test")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	// Stream should be dropped due to API error
	if len(result.Streams) != 0 {
		t.Errorf("Expected no streams (dropped due to API error), got %d streams", len(result.Streams))
	}
}

func TestEnrichStream_MakeMagnetURL(t *testing.T) {
	wrapped := &mockWrappedService{}
	mockAPI := &mockAPI{}
	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	infohash := "testhash123"
	sources := []string{"tracker1", "tracker2"}

	result := service.makeMagnetURL(infohash, sources)

	expected := "magnet:?xt=urn:btih:testhash123&tr=tracker1&tr=tracker2"
	if result != expected {
		t.Errorf("Expected magnet URL %s, got %s", expected, result)
	}
}

func TestEnrichStream_MakeMagnetURL_EmptySources(t *testing.T) {
	wrapped := &mockWrappedService{}
	mockAPI := &mockAPI{}
	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	infohash := "testhash123"
	sources := []string{}

	result := service.makeMagnetURL(infohash, sources)

	expected := "magnet:?xt=urn:btih:testhash123"
	if result != expected {
		t.Errorf("Expected magnet URL %s, got %s", expected, result)
	}
}

func TestEnrichStream_Timeout(t *testing.T) {
	// This test would require more complex mocking to simulate timeouts
	// For now, we'll just test that the service handles contexts properly
	wrapped := &mockWrappedService{
		response: &StreamsResponse{
			Streams: []StreamItem{
				{
					Title:    "Test Stream",
					Url:      "", // No URL
					InfoHash: "testhash",
					FileIdx:  0,
				},
			},
		},
	}

	// Create a context that's already cancelled to simulate timeout
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	mockAPI := &mockAPI{
		getResourceError: context.Canceled,
	}

	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	result, err := service.GetStreams(ctx, "movie", "test")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	// Stream should be dropped due to timeout/cancellation
	if len(result.Streams) != 0 {
		t.Errorf("Expected no streams (dropped due to timeout), got %d streams", len(result.Streams))
	}
}
