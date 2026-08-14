package stremio

import (
	"context"
	"time"
)

// StreamsService defines the contract for stream services
type StreamsService interface {
	// GetName returns the name of the stream service for logging purposes
	GetName() string
	// GetStreams fetches streams from an addon endpoint
	GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error)
}

// TimeoutedService is an optional StreamsService extension for sources that
// need a budget other than the CompositeStream default. Torznab indexers do:
// a Jackett endpoint fans out to every tracker it has configured before it
// answers, which regularly outlasts the 5s a Stremio addon gets.
type TimeoutedService interface {
	// GetTimeout returns the per-request budget for this service. A
	// non-positive value means "use the default".
	GetTimeout() time.Duration
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
