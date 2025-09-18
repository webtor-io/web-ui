package stremio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/webtor-io/lazymap"
)

func TestHTTPStreamService_SharedCache(t *testing.T) {
	requestCount := 0

	// Create mock server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		mockResponse := &StreamsResponse{
			Streams: []StreamItem{
				{
					Title:    "Test Movie 2024",
					InfoHash: "shared-cache-test-hash",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Create shared cache
	sharedCache := lazymap.New[*StreamsResponse](&lazymap.Config{
		Expire:      1 * time.Minute,
		ErrorExpire: 10 * time.Second,
	})

	// Create two different AddonStream instances with the same shared cache
	service1 := NewAddonStream(&http.Client{}, server.URL, sharedCache, "")
	service2 := NewAddonStream(&http.Client{}, server.URL, sharedCache, "")

	ctx := context.Background()

	// First request through service1
	response1, err := service1.GetStreams(ctx, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	if len(response1.Streams) != 1 {
		t.Errorf("Expected 1 stream in first response, got %d", len(response1.Streams))
	}
	if requestCount != 1 {
		t.Errorf("Expected 1 HTTP request after first call, got %d", requestCount)
	}

	// Second request through service2 - should use cached result
	response2, err := service2.GetStreams(ctx, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}
	if len(response2.Streams) != 1 {
		t.Errorf("Expected 1 stream in second response, got %d", len(response2.Streams))
	}
	// This should still be 1 because the cache was shared
	if requestCount != 1 {
		t.Errorf("Expected 1 HTTP request after second call (cached), got %d", requestCount)
	}

	// Verify both responses are identical (from cache)
	if response1.Streams[0].InfoHash != response2.Streams[0].InfoHash {
		t.Errorf("Responses should be identical from cache, got different InfoHashes: %s vs %s",
			response1.Streams[0].InfoHash, response2.Streams[0].InfoHash)
	}
}

func TestHTTPStreamService_NonSharedCache(t *testing.T) {
	requestCount := 0

	// Create mock server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		mockResponse := &StreamsResponse{
			Streams: []StreamItem{
				{
					Title:    "Test Movie 2024",
					InfoHash: "non-shared-cache-test-hash",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Create separate caches for each service
	cache1 := lazymap.New[*StreamsResponse](&lazymap.Config{
		Expire:      1 * time.Minute,
		ErrorExpire: 10 * time.Second,
	})
	cache2 := lazymap.New[*StreamsResponse](&lazymap.Config{
		Expire:      1 * time.Minute,
		ErrorExpire: 10 * time.Second,
	})

	// Create two different AddonStream instances with separate caches
	service1 := NewAddonStream(&http.Client{}, server.URL, cache1, "")
	service2 := NewAddonStream(&http.Client{}, server.URL, cache2, "")

	ctx := context.Background()

	// First request through service1
	_, err := service1.GetStreams(ctx, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("Expected 1 HTTP request after first call, got %d", requestCount)
	}

	// Second request through service2 - should NOT use cached result (different cache)
	_, err = service2.GetStreams(ctx, "movie", "tt12001534")
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}
	// This should be 2 because caches are separate
	if requestCount != 2 {
		t.Errorf("Expected 2 HTTP requests with separate caches, got %d", requestCount)
	}
}
