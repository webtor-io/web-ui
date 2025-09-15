package addon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/stremio"
)

func TestNewStreamService(t *testing.T) {
	service := NewStreamService(&http.Client{})
	if service == nil {
		t.Fatal("NewStreamService returned nil")
	}
}

func TestStreamService_GetStreams_Success(t *testing.T) {
	// Create mock response
	mockResponse := &stremio.StreamsResponse{
		Streams: []stremio.StreamItem{
			{
				Name:     "Torrentio\n4k HDR",
				Title:    "Test Movie 2024 4K HDR",
				InfoHash: "7355891df49ef0720e5cc5bc3517d21357b0a8d6",
				FileIdx:  0,
				BehaviorHints: &stremio.StreamBehaviorHints{
					BingeGroup: "torrentio|4k|WEB-DL|hevc|HDR",
					Filename:   "Test.Movie.2024.4K.HDR.mkv",
				},
				Sources: []string{
					"tracker:udp://tracker.opentrackr.org:1337/announce",
					"dht:7355891df49ef0720e5cc5bc3517d21357b0a8d6",
				},
			},
			{
				Name:     "Torrentio\n1080p",
				Title:    "Test Movie 2024 1080p",
				InfoHash: "aad47fa09a7597f4ed2a4ca8857c3e554694deee",
				FileIdx:  4,
				BehaviorHints: &stremio.StreamBehaviorHints{
					BingeGroup: "torrentio|1080p|h264",
					Filename:   "Test.Movie.2024.1080p.mkv",
				},
			},
		},
	}

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream/movie/tt12001534.json" {
			t.Errorf("Expected path /stream/movie/tt12001534.json, got %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Expected Accept application/json, got %s", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Test the service
	service := NewStreamService(&http.Client{})
	ctx := context.Background()

	response, err := service.GetStreams(ctx, server.URL, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("GetStreams failed: %v", err)
	}

	// Verify response
	if len(response.Streams) != 2 {
		t.Errorf("Expected 2 streams, got %d", len(response.Streams))
	}

	firstStream := response.Streams[0]
	if firstStream.Name != "Torrentio\n4k HDR" {
		t.Errorf("Expected first stream name 'Torrentio\n4k HDR', got %s", firstStream.Name)
	}
	if firstStream.InfoHash != "7355891df49ef0720e5cc5bc3517d21357b0a8d6" {
		t.Errorf("Expected first stream InfoHash '7355891df49ef0720e5cc5bc3517d21357b0a8d6', got %s", firstStream.InfoHash)
	}
	if firstStream.BehaviorHints == nil {
		t.Error("Expected BehaviorHints to be present")
	} else {
		if firstStream.BehaviorHints.BingeGroup != "torrentio|4k|WEB-DL|hevc|HDR" {
			t.Errorf("Expected BingeGroup 'torrentio|4k|WEB-DL|hevc|HDR', got %s", firstStream.BehaviorHints.BingeGroup)
		}
		if firstStream.BehaviorHints.Filename != "Test.Movie.2024.4K.HDR.mkv" {
			t.Errorf("Expected Filename 'Test.Movie.2024.4K.HDR.mkv', got %s", firstStream.BehaviorHints.Filename)
		}
	}
	if len(firstStream.Sources) != 2 {
		t.Errorf("Expected 2 sources, got %d", len(firstStream.Sources))
	}
}

func TestStreamService_GetStreams_HTTPError(t *testing.T) {
	// Create mock server that returns 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	service := NewStreamService(&http.Client{})
	ctx := context.Background()

	_, err := service.GetStreams(ctx, server.URL, "movie", "tt12001534")
	if err == nil {
		t.Error("Expected error for HTTP 500, got nil")
	}
}

func TestStreamService_GetStreams_InvalidJSON(t *testing.T) {
	// Create mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	service := NewStreamService(&http.Client{})
	ctx := context.Background()

	_, err := service.GetStreams(ctx, server.URL, "movie", "tt12001534")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestStreamService_GetStreams_Caching(t *testing.T) {
	requestCount := 0

	// Create mock server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		mockResponse := &stremio.StreamsResponse{
			Streams: []stremio.StreamItem{
				{
					Title:    "Test Movie 2024",
					InfoHash: "test-hash",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	service := NewStreamService(&http.Client{})
	ctx := context.Background()

	// First request
	_, err := service.GetStreams(ctx, server.URL, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Second request (should be cached)
	_, err = service.GetStreams(ctx, server.URL, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}

	// Should only have made one HTTP request due to caching
	if requestCount != 1 {
		t.Errorf("Expected 1 HTTP request due to caching, got %d", requestCount)
	}

	// Different content should not be cached
	_, err = service.GetStreams(ctx, server.URL, "movie", "tt99999999")
	if err != nil {
		t.Fatalf("Different content request failed: %v", err)
	}

	// Should now have made two HTTP requests
	if requestCount != 2 {
		t.Errorf("Expected 2 HTTP requests for different content, got %d", requestCount)
	}
}

func TestStreamService_GetStreams_ContextTimeout(t *testing.T) {
	// Create mock server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		mockResponse := &stremio.StreamsResponse{
			Streams: []stremio.StreamItem{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	service := NewStreamService(&http.Client{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := service.GetStreams(ctx, server.URL, "movie", "tt12001534")
	if err == nil {
		t.Error("Expected context timeout error, got nil")
	}
}

func TestStreamService_CacheExpiration(t *testing.T) {
	requestCount := 0

	// Create mock server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		mockResponse := &stremio.StreamsResponse{
			Streams: []stremio.StreamItem{
				{
					Title:    "Test Movie 2024",
					InfoHash: "test-hash",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Create service with shorter cache time for testing
	service := &StreamService{
		client: &http.Client{},
		cache: lazymap.New[*stremio.StreamsResponse](&lazymap.Config{
			Expire:      100 * time.Millisecond, // Short cache for testing
			ErrorExpire: 10 * time.Second,
		}),
	}

	ctx := context.Background()

	// First request
	_, err := service.GetStreams(ctx, server.URL, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Second request (cache should have expired)
	_, err = service.GetStreams(ctx, server.URL, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}

	// Should have made two HTTP requests due to cache expiration
	if requestCount != 2 {
		t.Errorf("Expected 2 HTTP requests due to cache expiration, got %d", requestCount)
	}
}
