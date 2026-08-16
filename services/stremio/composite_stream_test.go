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

// slowService reports how long it was allowed to run before its context was
// cancelled, which is how the timeout tests below observe the budget.
type slowService struct {
	timeout time.Duration
	gotCtx  chan time.Duration
}

func (s *slowService) GetName() string { return "SlowService" }

func (s *slowService) GetTimeout() time.Duration { return s.timeout }

func (s *slowService) GetStreams(ctx context.Context, _, _ string) (*StreamsResponse, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		s.gotCtx <- 0
		return &StreamsResponse{}, nil
	}
	s.gotCtx <- time.Until(deadline)
	return &StreamsResponse{}, nil
}

// plainService takes the default budget.
type plainService struct {
	gotCtx chan time.Duration
}

func (s *plainService) GetName() string { return "PlainService" }

func (s *plainService) GetStreams(ctx context.Context, _, _ string) (*StreamsResponse, error) {
	deadline, _ := ctx.Deadline()
	s.gotCtx <- time.Until(deadline)
	return &StreamsResponse{}, nil
}

func TestCompositeStreamHonoursServiceTimeout(t *testing.T) {
	slow := &slowService{timeout: 12 * time.Second, gotCtx: make(chan time.Duration, 1)}
	plain := &plainService{gotCtx: make(chan time.Duration, 1)}

	cs := NewCompositeStream([]StreamsService{slow, plain})
	if _, err := cs.GetStreams(context.Background(), "movie", "tt0133093"); err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}

	// Torznab indexers ask for more than the addon default; without the
	// TimeoutedService check they would be cut at 5s and dropped.
	if got := <-slow.gotCtx; got < 10*time.Second {
		t.Errorf("slow service budget = %v, want ~12s", got)
	}
	if got := <-plain.gotCtx; got > 6*time.Second {
		t.Errorf("plain service budget = %v, want the 5s default", got)
	}
}

func TestCompositeStreamTimeoutIsMaxOfChildren(t *testing.T) {
	// Composites nest: the Torznab composite is one service of the outer
	// composite, so the outer must not clamp it back to the default.
	inner := NewCompositeStream([]StreamsService{&slowService{timeout: 12 * time.Second, gotCtx: make(chan time.Duration, 1)}})
	if got := inner.GetTimeout(); got != 12*time.Second {
		t.Errorf("nested composite timeout = %v, want 12s", got)
	}
	empty := NewCompositeStream(nil)
	if got := empty.GetTimeout(); got != 0 {
		t.Errorf("empty composite timeout = %v, want 0 (falls back to the default)", got)
	}
}

// TestCompositeStreamSourceAccounting pins the leaf counting that lets the
// subscription poller tell "found nothing" from "could not ask": failed
// leaves are counted, nested composites report their own totals (zero
// included — an inner composite over no addons must not read as an answered
// source), and dedup passes the numbers through.
func TestCompositeStreamSourceAccounting(t *testing.T) {
	ctx := context.Background()

	t.Run("zero services means zero sources", func(t *testing.T) {
		resp, err := NewCompositeStream(nil).GetStreams(ctx, "movie", "tt1")
		if err != nil {
			t.Fatalf("GetStreams: %v", err)
		}
		if resp.Sources != 0 || resp.SourcesFailed != 0 {
			t.Errorf("sources=%d failed=%d, want 0/0", resp.Sources, resp.SourcesFailed)
		}
	})

	t.Run("failed leaves are counted", func(t *testing.T) {
		composite := NewCompositeStream([]StreamsService{
			&mockStreamService{response: &StreamsResponse{Streams: []StreamItem{{InfoHash: "aa"}}}},
			&mockStreamService{err: errors.New("addon down")},
		})
		resp, err := composite.GetStreams(ctx, "movie", "tt1")
		if err != nil {
			t.Fatalf("GetStreams: %v", err)
		}
		if resp.Sources != 2 || resp.SourcesFailed != 1 {
			t.Errorf("sources=%d failed=%d, want 2/1", resp.Sources, resp.SourcesFailed)
		}
	})

	t.Run("an empty nested composite is not an answered source", func(t *testing.T) {
		outer := NewCompositeStream([]StreamsService{
			NewCompositeStream(nil), // the addon half of an account with no addons
			NewCompositeStream([]StreamsService{
				&mockStreamService{err: errors.New("indexer down")},
			}),
		})
		resp, err := outer.GetStreams(ctx, "movie", "tt1")
		if err != nil {
			t.Fatalf("GetStreams: %v", err)
		}
		if resp.Sources != 1 || resp.SourcesFailed != 1 {
			t.Errorf("sources=%d failed=%d, want 1/1 — every real source failed", resp.Sources, resp.SourcesFailed)
		}
	})

	t.Run("dedup passes the accounting through", func(t *testing.T) {
		inner := NewCompositeStream([]StreamsService{
			&mockStreamService{response: &StreamsResponse{Streams: []StreamItem{{InfoHash: "aa"}, {InfoHash: "aa"}}}},
			&mockStreamService{err: errors.New("down")},
		})
		resp, err := NewDedupStream(inner).GetStreams(ctx, "movie", "tt1")
		if err != nil {
			t.Fatalf("GetStreams: %v", err)
		}
		if resp.Sources != 2 || resp.SourcesFailed != 1 {
			t.Errorf("sources=%d failed=%d, want 2/1 after dedup", resp.Sources, resp.SourcesFailed)
		}
		if len(resp.Streams) != 1 {
			t.Errorf("streams: %d, want 1 after dedup", len(resp.Streams))
		}
	})
}
