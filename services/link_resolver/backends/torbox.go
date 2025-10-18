package backends

import (
	"context"
	"fmt"

	"github.com/webtor-io/web-ui/services/link_resolver/common"
)

// Torbox implements Backend interface for Torbox
type Torbox struct{}

// Compile-time check to ensure Torbox implements Backend interface
var _ common.Backend = (*Torbox)(nil)

// NewTorbox creates a new Torbox backend
func NewTorbox() *Torbox {
	return &Torbox{}
}

// CheckAvailability checks if content is available in Torbox
func (t *Torbox) CheckAvailability(ctx context.Context, token, hash, path string) (bool, error) {
	// Torbox not yet implemented
	return false, fmt.Errorf("torbox not yet implemented")
}

// ResolveLink generates a direct link using Torbox
func (t *Torbox) ResolveLink(ctx context.Context, token, hash, path string) (string, bool, error) {
	// Torbox not yet implemented
	return "", false, fmt.Errorf("torbox not yet implemented")
}
