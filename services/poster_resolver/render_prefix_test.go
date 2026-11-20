package poster_resolver

import (
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/thumbnail"
)

// Erase finds a thumbnail's renders by thumbnail.RenderPrefix: every render
// key of the thumbnail starts with it, and no key of another source does.
func TestRenderPrefixMatchesTheResolver(t *testing.T) {
	const h = "0123456789abcdef0123456789abcdef01234567"
	prefix := thumbnail.RenderPrefix(h)
	src := thumbnailSource(&models.Thumbnail{Hash: h}, nil)
	for _, m := range []Mode{{Width: 240}, {OG: true, Blur: true}} {
		if key := cacheKey(src, m); !strings.HasPrefix(key, prefix) {
			t.Errorf("%+v: %q does not start with %q", m, key, prefix)
		}
	}
	if key := cacheKey(posterSource(SourceIMDbMovie, "tt0000001", "", nil), Mode{Width: 240}); strings.HasPrefix(key, prefix) {
		t.Errorf("a catalog poster's %q starts with %q", key, prefix)
	}
}
