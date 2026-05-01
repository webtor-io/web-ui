package common

import (
	"context"
)

// Backend defines the interface for streaming backends.
//
// fileIdx is the file's index in the torrent's natural file order (the
// same convention Stremio addons use). Backends look up the file in
// their own torrent metadata at the matching index — RealDebrid and
// Torbox return Files[] in bencode-decode order, which matches.
type Backend interface {
	// ResolveLink generates a direct link for the content
	ResolveLink(ctx context.Context, token, hash string, fileIdx int) (string, bool, error)
	// Validate validates backend
	Validate(ctx context.Context, token string) error
}
