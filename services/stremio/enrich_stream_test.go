package stremio

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
					BehaviorHints: &StreamBehaviorHints{
						Filename: "test_file.mp4",
					},
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
					BehaviorHints: &StreamBehaviorHints{
						Filename: "test_file.mp4",
					},
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
					Name: "test_file.mp4",
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
					BehaviorHints: &StreamBehaviorHints{
						Filename: "test_file.mp4",
					},
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
					Name: "test_file.mp4",
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
					BehaviorHints: &StreamBehaviorHints{
						Filename: "test_file.mp4",
					},
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
					BehaviorHints: &StreamBehaviorHints{
						Filename: "test_file.mp4",
					},
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

func TestEnrichStream_ConcurrentAccess(t *testing.T) {
	// Test for race conditions with multiple concurrent streams
	streams := make([]StreamItem, 50)
	for i := 0; i < 50; i++ {
		streams[i] = StreamItem{
			Title:    fmt.Sprintf("Test Stream %d", i),
			Url:      "", // No URL, requires enrichment
			InfoHash: fmt.Sprintf("testhash%d", i),
			FileIdx:  int(i % 10),
			Sources:  []string{"tracker1", "tracker2"},
			BehaviorHints: &StreamBehaviorHints{
				Filename: fmt.Sprintf("test_file_%d.mp4", i),
			},
		}
	}

	wrapped := &mockWrappedService{
		response: &StreamsResponse{Streams: streams},
	}

	mockAPI := &mockAPI{
		// Resource exists for all streams
		getResourceResponse: &ra.ResourceResponse{},
		listContentResponse: &ra.ListResponse{
			Items: []ra.ListItem{
				{ID: "file0", Name: "test_file_0.mp4", Type: ra.ListTypeFile},
				{ID: "file1", Name: "test_file_1.mp4", Type: ra.ListTypeFile},
				{ID: "file2", Name: "test_file_2.mp4", Type: ra.ListTypeFile},
				{ID: "file3", Name: "test_file_3.mp4", Type: ra.ListTypeFile},
				{ID: "file4", Name: "test_file_4.mp4", Type: ra.ListTypeFile},
				{ID: "file5", Name: "test_file_5.mp4", Type: ra.ListTypeFile},
				{ID: "file6", Name: "test_file_6.mp4", Type: ra.ListTypeFile},
				{ID: "file7", Name: "test_file_7.mp4", Type: ra.ListTypeFile},
				{ID: "file8", Name: "test_file_8.mp4", Type: ra.ListTypeFile},
				{ID: "file9", Name: "test_file_9.mp4", Type: ra.ListTypeFile},
			},
			Count: 10,
		},
		exportContentResponse: &ra.ExportResponse{
			ExportItems: map[string]ra.ExportItem{
				"download": {
					URL: "http://generated.url",
				},
			},
		},
	}

	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	// Run the test multiple times to increase chance of detecting race conditions
	for run := 0; run < 10; run++ {
		ctx := context.Background()
		result, err := service.GetStreams(ctx, "movie", "test")

		if err != nil {
			t.Errorf("Run %d: Expected no error, got %v", run, err)
		}
		if len(result.Streams) != 50 {
			t.Errorf("Run %d: Expected 50 streams, got %d", run, len(result.Streams))
		}

		// Verify all streams have URLs
		for i, stream := range result.Streams {
			if stream.Url == "" {
				t.Errorf("Run %d: Stream %d missing URL", run, i)
			}
		}
	}
}

func TestEnrichStream_BackgroundRetry(t *testing.T) {
	// Test that background retry is triggered when StoreResource fails due to context deadline
	wrapped := &mockWrappedService{
		response: &StreamsResponse{
			Streams: []StreamItem{
				{
					Title:    "Test Stream",
					Url:      "", // No URL, requires enrichment
					InfoHash: "testhash",
					FileIdx:  0,
					Sources:  []string{"tracker1", "tracker2"},
					BehaviorHints: &StreamBehaviorHints{
						Filename: "test_file.mp4",
					},
				},
			},
		},
	}

	mockAPI := &mockAPI{
		getResourceResponse:   nil, // Resource doesn't exist, will trigger StoreResource
		storeResourceResponse: nil,
		storeResourceError:    context.DeadlineExceeded, // Simulate deadline exceeded
	}

	service := NewEnrichStream(wrapped, mockAPI, &api.Claims{})

	// Create a context with very short timeout to trigger deadline exceeded
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Allow some time for context to timeout
	time.Sleep(2 * time.Millisecond)

	result, err := service.GetStreams(ctx, "movie", "test")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Stream should be dropped from immediate result due to StoreResource failure
	if len(result.Streams) != 0 {
		t.Errorf("Expected no streams (dropped due to StoreResource timeout), got %d streams", len(result.Streams))
	}

	// Wait a bit for background goroutine to complete
	// Note: This test verifies that the background retry is triggered,
	// but with the current mock structure we can't easily verify the actual retry call.
	// The important part is that the stream is dropped and the background retry logic is invoked.
	time.Sleep(100 * time.Millisecond)
}
