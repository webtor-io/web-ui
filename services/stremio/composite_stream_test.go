package stremio

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockStreamService implements StreamsService for testing
type mockStreamService struct {
	response *StreamsResponse
	err      error
	delay    time.Duration
}

func (m *mockStreamService) GetName() string {
	return "mockStreamService"
}

func (m *mockStreamService) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.response, m.err
}

func TestNewCompositeStreamService(t *testing.T) {
	services := []StreamsService{
		&mockStreamService{},
	}

	composite := NewCompositeStream(services)

	if composite == nil {
		t.Fatal("expected composite service to be created")
	}

	if len(composite.services) != 1 {
		t.Errorf("expected 1 service, got %d", len(composite.services))
	}
}

func TestNewCompositeStreamService_NilLogger(t *testing.T) {
	services := []StreamsService{
		&mockStreamService{},
	}

	composite := NewCompositeStream(services)

	if composite == nil {
		t.Fatal("expected composite service to be created")
	}
}

func TestCompositeStreamService_GetStreams_EmptyServices(t *testing.T) {
	composite := NewCompositeStream([]StreamsService{})

	result, err := composite.GetStreams(context.Background(), "movie", "123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be non-nil")
	}

	if len(result.Streams) != 0 {
		t.Errorf("expected empty streams, got %d", len(result.Streams))
	}
}

func TestCompositeStreamService_GetStreams_Success(t *testing.T) {
	services := []StreamsService{
		&mockStreamService{
			response: &StreamsResponse{
				Streams: []StreamItem{
					{Title: "Stream 1", InfoHash: "hash1"},
				},
			},
		},
		&mockStreamService{
			response: &StreamsResponse{
				Streams: []StreamItem{
					{Title: "Stream 2", InfoHash: "hash2"},
					{Title: "Stream 3", InfoHash: "hash3"},
				},
			},
		},
	}

	composite := NewCompositeStream(services)

	result, err := composite.GetStreams(context.Background(), "movie", "123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be non-nil")
	}

	// Should have 3 streams total (1 + 2)
	if len(result.Streams) != 3 {
		t.Errorf("expected 3 streams, got %d", len(result.Streams))
	}

	// Check order is preserved (first service streams come first)
	if result.Streams[0].Title != "Stream 1" {
		t.Errorf("expected first stream to be 'Stream 1', got '%s'", result.Streams[0].Title)
	}
	if result.Streams[1].Title != "Stream 2" {
		t.Errorf("expected second stream to be 'Stream 2', got '%s'", result.Streams[1].Title)
	}
	if result.Streams[2].Title != "Stream 3" {
		t.Errorf("expected third stream to be 'Stream 3', got '%s'", result.Streams[2].Title)
	}
}

func TestCompositeStreamService_GetStreams_WithErrors(t *testing.T) {
	services := []StreamsService{
		&mockStreamService{
			response: &StreamsResponse{
				Streams: []StreamItem{
					{Title: "Stream 1", InfoHash: "hash1"},
				},
			},
		},
		&mockStreamService{
			err: errors.New("service error"),
		},
		&mockStreamService{
			response: &StreamsResponse{
				Streams: []StreamItem{
					{Title: "Stream 3", InfoHash: "hash3"},
				},
			},
		},
	}

	composite := NewCompositeStream(services)

	result, err := composite.GetStreams(context.Background(), "movie", "123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 streams (error service dropped)
	if len(result.Streams) != 2 {
		t.Errorf("expected 2 streams, got %d", len(result.Streams))
	}
}

func TestCompositeStreamService_GetStreams_WithTimeout(t *testing.T) {
	services := []StreamsService{
		&mockStreamService{
			response: &StreamsResponse{
				Streams: []StreamItem{
					{Title: "Stream 1", InfoHash: "hash1"},
				},
			},
		},
		&mockStreamService{
			delay: 200 * time.Millisecond, // Will timeout
		},
	}

	composite := NewCompositeStream(services)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := composite.GetStreams(ctx, "movie", "123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 stream (timeout service dropped)
	if len(result.Streams) != 1 {
		t.Errorf("expected 1 stream, got %d", len(result.Streams))
	}
}

func TestCompositeStreamService_GetStreams_OrderPreservation(t *testing.T) {
	// Create services with different response times to test order preservation
	services := []StreamsService{
		&mockStreamService{
			response: &StreamsResponse{
				Streams: []StreamItem{
					{Title: "First Service", InfoHash: "hash1"},
				},
			},
			delay: 50 * time.Millisecond, // Slower
		},
		&mockStreamService{
			response: &StreamsResponse{
				Streams: []StreamItem{
					{Title: "Second Service", InfoHash: "hash2"},
				},
			},
			delay: 10 * time.Millisecond, // Faster
		},
		&mockStreamService{
			response: &StreamsResponse{
				Streams: []StreamItem{
					{Title: "Third Service", InfoHash: "hash3"},
				},
			},
			delay: 30 * time.Millisecond, // Medium
		},
	}

	composite := NewCompositeStream(services)

	result, err := composite.GetStreams(context.Background(), "movie", "123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Streams) != 3 {
		t.Fatalf("expected 3 streams, got %d", len(result.Streams))
	}

	// Even though second service responds fastest, first service's streams should come first
	if result.Streams[0].Title != "First Service" {
		t.Errorf("expected first stream to be 'First Service', got '%s'", result.Streams[0].Title)
	}
	if result.Streams[1].Title != "Second Service" {
		t.Errorf("expected second stream to be 'Second Service', got '%s'", result.Streams[1].Title)
	}
	if result.Streams[2].Title != "Third Service" {
		t.Errorf("expected third stream to be 'Third Service', got '%s'", result.Streams[2].Title)
	}
}

func TestConvertManifestURLToBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL with manifest.json suffix",
			input:    "https://example.com/addon/manifest.json",
			expected: "https://example.com/addon",
		},
		{
			name:     "URL without manifest.json suffix",
			input:    "https://example.com/addon",
			expected: "https://example.com/addon",
		},
		{
			name:     "URL with manifest.json in middle",
			input:    "https://example.com/manifest.json/addon",
			expected: "https://example.com/manifest.json/addon",
		},
		{
			name:     "Complex URL with manifest.json suffix",
			input:    "https://api.example.com/v1/stremio/addon/manifest.json",
			expected: "https://api.example.com/v1/stremio/addon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertManifestURLToBaseURL(tt.input)
			if result != tt.expected {
				t.Errorf("convertManifestURLToBaseURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
