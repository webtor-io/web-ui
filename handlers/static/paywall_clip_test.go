package static_test

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/handlers/static"
	w "github.com/webtor-io/web-ui/services/web"
)

// servePubClips mounts pub/ the way serve.go does, behind the default
// noindex middleware, with the repository root as the working directory.
func servePubClips(t *testing.T) *gin.Engine {
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

// The Stremio paywall clip is fetched by video players, not browsers: it has
// to be typed video/mp4 and answer byte ranges (AVPlayer refuses a file it
// cannot range over; ExoPlayer and libmpv seek with ranges), under GET and
// under the HEAD a player may send first. Not indexable: it is a paywall,
// not content — the default noindex applies, as to every file the sitemap
// does not list.
func TestPaywallClipIsServedAsVideo(t *testing.T) {
	r := servePubClips(t)
	const path = "/pub/stremio/paywall-en.mp4"
	info, err := os.Stat("pub/stremio/paywall-en.mp4")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Errorf("Content-Type = %q, want video/mp4", got)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.FormatInt(info.Size(), 10) {
		t.Errorf("Content-Length = %q, want %d", got, info.Size())
	}
	if got := rec.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Range", "bytes=0-99")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("ranged GET status %d, want 206", rec.Code)
	}
	if got, want := rec.Header().Get("Content-Range"), "bytes 0-99/"+strconv.FormatInt(info.Size(), 10); got != want {
		t.Errorf("Content-Range = %q, want %q", got, want)
	}
	if rec.Body.Len() != 100 {
		t.Errorf("ranged body %d bytes, want 100", rec.Body.Len())
	}
}
