package torbox

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// RequestDownloadLink carries the user's TorBox token in the query; a
// transport error quotes the URL, and the link resolver logs it.
func TestTransportErrorHidesTheToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(http.DefaultClient, "https://api.torbox.app", "SECRETTOKEN").RequestDownloadLink(ctx, 1, 2)
	if err == nil || strings.Contains(err.Error(), "SECRETTOKEN") || !strings.Contains(err.Error(), "token=<redacted>") {
		t.Errorf("error = %v", err)
	}
}
