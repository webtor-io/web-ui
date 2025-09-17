package stremio

import (
	"context"
)

// StreamsService defines the contract for stream services
type StreamsService interface {
	// GetName returns the name of the stream service for logging purposes
	GetName() string
	// GetStreams fetches streams from an addon endpoint
	GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error)
}

type MetaService interface {
	GetMeta(ctx context.Context, contentType, contentID string) (*MetaResponse, error)
}

type CatalogService interface {
	GetCatalog(ctx context.Context, contentType string) (*MetasResponse, error)
}

type ManifestService interface {
	GetManifest(ctx context.Context) (*ManifestResponse, error)
}
