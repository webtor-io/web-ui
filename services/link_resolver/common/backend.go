package common

import (
	"context"
)

// Backend defines the interface for streaming backends
type Backend interface {
	// ResolveLink generates a direct link for the content
	ResolveLink(ctx context.Context, token, hash, path string) (string, bool, error)
}
