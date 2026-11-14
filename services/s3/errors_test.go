package s3

import (
	"net/http"
	"testing"

	"github.com/webtor-io/web-ui/services/vfs"
)

// S3 has no 422: a refused upload (a private torrent without a tracker) is the
// client's request that cannot be served, which S3 calls InvalidRequest.
func TestErrorFromVFS422(t *testing.T) {
	e := errorFromVFS(vfs.NewHTTPError(http.StatusUnprocessableEntity, nil), false)
	if e.Status != http.StatusBadRequest || e.Code != ErrCodeInvalidRequest || e.Message != "Invalid Request" {
		t.Errorf("got %d %s %q, want 400 InvalidRequest", e.Status, e.Code, e.Message)
	}
}
