package static

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func writeAssets(t *testing.T) (dir string, cssHash string) {
	t.Helper()
	dir = t.TempDir()
	files := map[string]string{
		"style.css":            "body{color:red}",
		"1845.0e9497d86b.js":   "console.log(1)",
		"night/favicon.svg":    "<svg/>",
		"sub/.keep":            "",
		"../outside-root.css":  "secret{}",
		"nested/deep/file.css": "a{}",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sum := md5.Sum([]byte(files["style.css"]))
	return dir, hex.EncodeToString(sum[:])
}

func newAssetsRouter(t *testing.T, dir string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	registerAssets(r, dir)
	return r
}

// The version stamp web.Helper.Asset puts in the URL is the md5 of the file;
// the route compares against the same function, so this pins what both sides
// compute.
func TestAssetHashesIsTheMD5OfTheFile(t *testing.T) {
	dir, want := writeAssets(t)
	got, err := NewAssetHashes(dir).Get("style.css")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("hash %s, want %s", got, want)
	}
	if _, err := NewAssetHashes(dir).Get("missing.css"); err == nil {
		t.Error("a missing file must not get a hash")
	}
}

func TestAssetCacheControl(t *testing.T) {
	dir, hash := writeAssets(t)
	r := newAssetsRouter(t, dir)

	cases := []struct {
		name    string
		method  string
		target  string
		headers map[string]string
		status  int
		cache   string
	}{
		// The URL web.Helper.Asset renders: exactly these bytes, for good.
		{"current hash", http.MethodGet, "/assets/style.css?" + hash, nil, http.StatusOK, cacheImmutable},
		{"current hash HEAD", http.MethodHead, "/assets/style.css?" + hash, nil, http.StatusOK, cacheImmutable},
		{"current hash range", http.MethodGet, "/assets/style.css?" + hash, map[string]string{"Range": "bytes=0-3"}, http.StatusPartialContent, cacheImmutable},
		{"current hash revalidated", http.MethodGet, "/assets/style.css?" + hash,
			map[string]string{"If-Modified-Since": time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)}, http.StatusNotModified, cacheImmutable},
		// Rollout: the page of a new replica asks an old one for a hash it
		// does not have. The bytes are not what the URL names.
		{"other hash", http.MethodGet, "/assets/style.css?0123456789abcdef0123456789abcdef", nil, http.StatusOK, cacheNone},
		{"hash with extra query", http.MethodGet, "/assets/style.css?" + hash + "&x=1", nil, http.StatusOK, cacheNone},
		// No stamp at all: lazy chunks, images, source maps.
		{"no hash", http.MethodGet, "/assets/style.css", nil, http.StatusOK, cacheShort},
		{"chunk", http.MethodGet, "/assets/1845.0e9497d86b.js", nil, http.StatusOK, cacheShort},
		{"nested", http.MethodGet, "/assets/night/favicon.svg", nil, http.StatusOK, cacheShort},
		// Errors are never cached.
		{"missing with hash", http.MethodGet, "/assets/missing.css?" + hash, nil, http.StatusNotFound, cacheNone},
		{"missing", http.MethodGet, "/assets/missing.css", nil, http.StatusNotFound, cacheNone},
		{"directory", http.MethodGet, "/assets/sub/", nil, http.StatusNotFound, cacheNone},
		{"bad range", http.MethodGet, "/assets/style.css?" + hash, map[string]string{"Range": "bytes=999-1000"}, http.StatusRequestedRangeNotSatisfiable, cacheNone},
		{"outside the root", http.MethodGet, "/assets/../outside-root.css", nil, http.StatusNotFound, cacheNone},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.target, nil)
		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, w.Code, tc.status)
		}
		if got := w.Header().Get("Cache-Control"); got != tc.cache {
			t.Errorf("%s: Cache-Control %q, want %q", tc.name, got, tc.cache)
		}
	}
}

// A panic after the verdict was taken ends in the recovery middleware's 500;
// the immutable value decided for the file must not ride along on it.
func TestAssetCacheControl_ErrorAfterVerdictIsNotCached(t *testing.T) {
	dir, hash := writeAssets(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Group("/assets", assetCache(NewAssetHashes(dir))).GET("/*filepath", func(c *gin.Context) {
		panic("file server blew up")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/style.css?"+hash, nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != cacheNone {
		t.Errorf("Cache-Control %q on a 500, want %q", got, cacheNone)
	}
}
