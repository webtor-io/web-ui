package static_test

import (
	"bytes"
	"flag"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"

	hc "github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/handlers/static"
	w "github.com/webtor-io/web-ui/services/web"
)

// servePub mounts pub/ the way serve.go does, behind the default noindex
// middleware, with the repository root as the working directory.
func servePub(t *testing.T) *gin.Engine {
	t.Helper()
	t.Chdir("../..")
	gin.SetMode(gin.TestMode)
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String(static.AssetsPathFlag, t.TempDir(), "")
	set.String(static.AssetsHostFlag, "", "")
	r := gin.New()
	r.Use(w.NoindexDefault(false))
	if err := static.RegisterHandler(cli.NewContext(nil, set, nil), r); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestShareImagesAreServedAsDeclared: the templates promise og:image:width
// 1200 and height 630 for the brand card (templates/partials/og_card.html),
// and the embed creative is drawn at 1280x720 for the player slot. A redrawn
// file of another size, or one served with noindex, breaks the preview
// without breaking anything else.
func TestShareImagesAreServedAsDeclared(t *testing.T) {
	r := servePub(t)
	cases := []struct {
		path, mime    string
		width, height int
		maxBytes      int
	}{
		{"/og-card.png", "image/png", 1200, 630, 300 << 10},
		{"/webtor.jpg", "image/jpeg", 1280, 720, 300 << 10},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.mime {
				t.Errorf("Content-Type = %q, want %q", got, tc.mime)
			}
			if got := rec.Header().Get("X-Robots-Tag"); got != "" {
				t.Errorf("X-Robots-Tag = %q, want none", got)
			}
			if n := rec.Body.Len(); n > tc.maxBytes {
				t.Errorf("%d bytes, the budget is %d", n, tc.maxBytes)
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if cfg.Width != tc.width || cfg.Height != tc.height {
				t.Errorf("%dx%d, want %dx%d", cfg.Width, cfg.Height, tc.width, tc.height)
			}
		})
	}
}

// TestLLMsTxtKeepsToTheFacts guards pub/llms.txt, which language models read
// as the site's own description of itself. It used to say Discover lets you
// "search for movies" and "start streaming instantly" (Webtor does not search
// for content, and a stream starts once the swarm delivers), linked a /library
// that does not exist, sold "No ads" as a paid feature, and listed no tools.
func TestLLMsTxtKeepsToTheFacts(t *testing.T) {
	b, err := os.ReadFile("../../pub/llms.txt")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	lower := strings.ToLower(text)
	for _, banned := range []string{"instantly", "search for movies", "/library", "no ads"} {
		if strings.Contains(lower, banned) {
			t.Errorf("pub/llms.txt says %q", banned)
		}
	}
	for _, tool := range hc.Tools {
		if link := "(https://webtor.io/" + tool.Url + ")"; !strings.Contains(text, link) {
			t.Errorf("pub/llms.txt does not list the tool page %s", link)
		}
	}
}

// TestLLMsTxtIsServed: the file is reachable at the root, as crawlers look
// for it, and as plain text.
func TestLLMsTxtIsServed(t *testing.T) {
	r := servePub(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q", got)
	}
}
