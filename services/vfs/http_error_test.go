package vfs

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/api"
)

// An error that carries its own HTTP status keeps it through every protocol
// layer, wrapped or not: rest-api's refusal of a private torrent reaches a
// WebDAV or S3 client as rest-api's status, not as a 500.
func TestHTTPErrorKeepsCarriedStatus(t *testing.T) {
	if got := StatusCode(errors.Wrap(&api.PrivateError{Status: 422}, "x")); got != 422 {
		t.Errorf("a carried 422: %d", got)
	}
	if got := StatusCode(errors.New("x")); got != 500 {
		t.Errorf("a plain error: %d, want 500", got)
	}
}
