package common

import (
	"context"
)

// Backend defines the interface for streaming backends
type Backend interface {
	// CheckAvailability checks if content is available in this backend
	CheckAvailability(ctx context.Context, token, hash, path string) (bool, error)

	// ResolveLink generates a direct link for the content
	ResolveLink(ctx context.Context, token, hash, path string) (string, bool, error)
}
