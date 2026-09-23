package poster_resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBannerCacheIDFollowsTheFile: the rendered brand banner is cached on S3
// under this ID and never expires, so a redrawn pub/webtor.jpg must land
// under a new key — or resource pages without artwork keep sharing the old
// picture.
func TestBannerCacheIDFollowsTheFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	oldBanner := write("old.jpg", "the banner as it was")
	newBanner := write("new.jpg", "the banner redrawn")
	sameAsOld := write("copy.jpg", "the banner as it was")

	a, b, c := bannerCacheID(oldBanner), bannerCacheID(newBanner), bannerCacheID(sameAsOld)
	if a == b {
		t.Errorf("a redrawn banner keeps the cache ID %q", a)
	}
	if a != c {
		t.Errorf("the same bytes give different IDs: %q and %q", a, c)
	}
	for _, id := range []string{a, b} {
		if !strings.HasPrefix(id, "default-") {
			t.Errorf("ID %q should stay under the default- prefix", id)
		}
	}
	if got := bannerCacheID(filepath.Join(dir, "missing.jpg")); got != "default" {
		t.Errorf("an unreadable banner gives %q, want the plain default", got)
	}
}
