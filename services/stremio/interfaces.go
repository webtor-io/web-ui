package stremio

import (
	"context"
)

// StreamService defines the contract for stream services
type StreamService interface {
	// GetName returns the name of the stream service for logging purposes
	GetName() string
	// GetStreams fetches streams from an addon endpoint
	GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error)
}
