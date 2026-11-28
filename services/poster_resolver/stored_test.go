package poster_resolver

import (
	"context"
	"strings"
	"testing"
)

func TestValidateStoredPosterURL(t *testing.T) {
	ctx := context.Background()
	bad := []string{
		"",
		"ftp://example.com/a.jpg",
		"file:///etc/passwd",
		"http://127.0.0.1:8096/resource/",
		"http://localhost/a.jpg",
		"http://[::1]/a.jpg",
		"http://10.0.0.5/a.jpg",
		"http://192.168.1.1/a.jpg",
		"http://169.254.169.254/latest/meta-data",
		"http://100.64.0.1/a.jpg",
		"http://0.0.0.0/a.jpg",
		"https://user:pw@8.8.8.8/a.jpg",
		"https://8.8.8.8/" + strings.Repeat("a", maxStoredPosterURL),
	}
	for _, u := range bad {
		if err := ValidateStoredPosterURL(ctx, u); err == nil {
			t.Errorf("expected %q to be refused", u)
		}
	}
	// A public IP literal needs no DNS, so this stays hermetic.
	if err := ValidateStoredPosterURL(ctx, "https://8.8.8.8/poster.jpg"); err != nil {
		t.Errorf("public url refused: %v", err)
	}
}
