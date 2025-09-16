package stremio

import (
	"context"
	"errors"
	"testing"
)

// dedupMockStreamService implements StreamService for testing
type dedupMockStreamService struct {
	name    string
	streams []StreamItem
	err     error
}

func (m *dedupMockStreamService) GetName() string {
	return m.name
}

func (m *dedupMockStreamService) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &StreamsResponse{Streams: m.streams}, nil
}

func TestDedupStreamService_GetName(t *testing.T) {
	inner := &dedupMockStreamService{name: "MockService"}
	dedup := NewDedupStreamService(inner)

	if got := dedup.GetName(); got != "DedupStreamService" {
		t.Errorf("GetName() = %v, want %v", got, "DedupStreamService")
	}
}

func TestDedupStreamService_GetStreams_NoDuplicates(t *testing.T) {
	streams := []StreamItem{
		{Title: "Stream 1", InfoHash: "hash1", FileIdx: 0},
		{Title: "Stream 2", InfoHash: "hash2", FileIdx: 1},
		{Title: "Stream 3", InfoHash: "hash3", FileIdx: 0},
	}

	inner := &dedupMockStreamService{streams: streams}
	dedup := NewDedupStreamService(inner)

	result, err := dedup.GetStreams(context.Background(), "movie", "test")

	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}

	if len(result.Streams) != 3 {
		t.Errorf("Expected 3 streams, got %d", len(result.Streams))
	}

	// Verify order is preserved
	expected := []string{"Stream 1", "Stream 2", "Stream 3"}
	for i, stream := range result.Streams {
		if stream.Title != expected[i] {
			t.Errorf("Stream %d title = %v, want %v", i, stream.Title, expected[i])
		}
	}
}

func TestDedupStreamService_GetStreams_WithDuplicates(t *testing.T) {
	streams := []StreamItem{
		{Title: "Stream 1", InfoHash: "hash1", FileIdx: 0},
		{Title: "Stream 2", InfoHash: "hash2", FileIdx: 1},
		{Title: "Stream 3 (duplicate)", InfoHash: "hash1", FileIdx: 0}, // Duplicate by infohash+fileIdx
		{Title: "Stream 4", InfoHash: "hash3", FileIdx: 0},
		{Title: "Stream 5 (duplicate)", InfoHash: "hash2", FileIdx: 1}, // Another duplicate
		{Title: "Stream 6", InfoHash: "hash1", FileIdx: 1},             // Same infohash, different fileIdx - should keep
	}

	inner := &dedupMockStreamService{streams: streams}
	dedup := NewDedupStreamService(inner)

	result, err := dedup.GetStreams(context.Background(), "movie", "test")

	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}

	if len(result.Streams) != 4 {
		t.Errorf("Expected 4 unique streams, got %d", len(result.Streams))
	}

	// Verify order is preserved and correct streams are kept
	expected := []string{"Stream 1", "Stream 2", "Stream 4", "Stream 6"}
	for i, stream := range result.Streams {
		if stream.Title != expected[i] {
			t.Errorf("Stream %d title = %v, want %v", i, stream.Title, expected[i])
		}
	}

	// Verify the unique combinations
	expectedCombos := []dedupKey{
		{"hash1", 0},
		{"hash2", 1},
		{"hash3", 0},
		{"hash1", 1},
	}

	for i, stream := range result.Streams {
		key := dedupKey{InfoHash: stream.InfoHash, FileIdx: stream.FileIdx}
		if key != expectedCombos[i] {
			t.Errorf("Stream %d key = %v, want %v", i, key, expectedCombos[i])
		}
	}
}

func TestDedupStreamService_GetStreams_EmptyResponse(t *testing.T) {
	inner := &dedupMockStreamService{streams: []StreamItem{}}
	dedup := NewDedupStreamService(inner)

	result, err := dedup.GetStreams(context.Background(), "movie", "test")

	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}

	if len(result.Streams) != 0 {
		t.Errorf("Expected 0 streams, got %d", len(result.Streams))
	}
}

func TestDedupStreamService_GetStreams_NilResponse(t *testing.T) {
	inner := &dedupMockStreamService{streams: nil}
	dedup := NewDedupStreamService(inner)

	result, err := dedup.GetStreams(context.Background(), "movie", "test")

	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}

	if result == nil || len(result.Streams) != 0 {
		t.Errorf("Expected empty result, got %v", result)
	}
}

func TestDedupStreamService_GetStreams_InnerServiceError(t *testing.T) {
	expectedErr := errors.New("inner service error")
	inner := &dedupMockStreamService{err: expectedErr}
	dedup := NewDedupStreamService(inner)

	result, err := dedup.GetStreams(context.Background(), "movie", "test")

	if err != expectedErr {
		t.Errorf("GetStreams() error = %v, want %v", err, expectedErr)
	}

	if result != nil {
		t.Errorf("Expected nil result on error, got %v", result)
	}
}

func TestDedupStreamService_GetStreams_EmptyInfoHashAndFileIdx(t *testing.T) {
	streams := []StreamItem{
		{Title: "Stream 1", InfoHash: "", FileIdx: 0},
		{Title: "Stream 2", InfoHash: "", FileIdx: 0}, // Duplicate empty values
		{Title: "Stream 3", InfoHash: "hash1", FileIdx: 0},
	}

	inner := &dedupMockStreamService{streams: streams}
	dedup := NewDedupStreamService(inner)

	result, err := dedup.GetStreams(context.Background(), "movie", "test")

	if err != nil {
		t.Fatalf("GetStreams() error = %v", err)
	}

	// Should keep only first stream with empty infohash+fileIdx combination
	if len(result.Streams) != 2 {
		t.Errorf("Expected 2 streams, got %d", len(result.Streams))
	}

	expected := []string{"Stream 1", "Stream 3"}
	for i, stream := range result.Streams {
		if stream.Title != expected[i] {
			t.Errorf("Stream %d title = %v, want %v", i, stream.Title, expected[i])
		}
	}
}
