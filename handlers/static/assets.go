package static

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/lazymap"
)

// AssetHashes is the version stamp of a built asset: the md5 of the file.
// web.Helper.Asset appends it as the whole query of the URL it renders
// (/assets/style.css?<md5>), and the asset route below compares the query
// against it before it lets anyone cache that URL for good. One type for
// both sides, so the stamp in the page and the stamp being checked cannot
// drift apart.
//
// Values are kept for the life of the process: the files come with the image
// and do not change under a running replica. A name that fails to hash (no
// such file, a directory) is not kept -- lazymap drops errors -- so the map is
// bounded by the build, not by what clients send.
type AssetHashes struct {
	*lazymap.LazyMap[string]
	path string
}

func NewAssetHashes(path string) *AssetHashes {
	return &AssetHashes{
		LazyMap: lazymap.New[string](&lazymap.Config{}),
		path:    path,
	}
}

// Get returns the md5 of the asset at name, relative to the assets path.
func (s *AssetHashes) Get(name string) (string, error) {
	return s.LazyMap.Get(name, func() (string, error) {
		f, err := os.Open(s.path + "/" + name)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		md5Hash := md5.New()
		if _, err := io.Copy(md5Hash, f); err != nil {
			return "", err
		}
		return hex.EncodeToString(md5Hash.Sum(nil)), nil
	})
}

// Cache-Control values of the asset route.
//
// A URL whose query is the current hash of the file names exactly these
// bytes, so it is cached for a year and never revalidated. Anything else
// gets a short life: 30 minutes is what the edge already gave the browser for
// a response without Cache-Control, so for those URLs browsers see no change.
//
// The hash is compared, not just looked for, because of rollouts: a page
// rendered by a new replica asks for style.css?<new>, and the request may
// land on an old replica that still has the old file. Marked immutable, the
// old bytes would stay pinned under the new URL for a year at the edge and in
// every browser that caught them. Such an answer is not the file the URL
// names, so it is not stored anywhere.
//
// Errors are never cached: a 404 here is most often a chunk the replica that
// answered has not got yet, and a cached 404 would outlive the rollout.
const (
	cacheImmutable = "public, max-age=31536000, immutable"
	cacheShort     = "public, max-age=1800"
	cacheNone      = "no-store"
)

// assetCacheControl picks the Cache-Control for GET /assets/<name>?<query>
// as if the file is there; what the route ends up answering when it is not
// (404, a directory) is cacheWriter's to correct, so the one place that knows
// the final status decides for errors.
func assetCacheControl(hashes *AssetHashes, name, query string) string {
	if query == "" {
		return cacheShort
	}
	// The same cleaning http.Dir.Open applies, so the name that is hashed is
	// the file that is served, and nothing outside the assets path is read.
	hash, err := hashes.Get(strings.TrimPrefix(path.Clean("/"+name), "/"))
	if err != nil || query != hash {
		return cacheNone
	}
	return cacheImmutable
}

// assetCache sets the Cache-Control of the asset route before the file
// server runs. The file server writes the headers when it writes the status,
// so the value is decided up front; cacheWriter then holds it to the
// statuses it was decided for.
func assetCache(hashes *AssetHashes) gin.HandlerFunc {
	return func(c *gin.Context) {
		value := assetCacheControl(hashes, c.Param("filepath"), c.Request.URL.RawQuery)
		c.Header("Cache-Control", value)
		c.Writer = &cacheWriter{ResponseWriter: c.Writer, value: value}
		c.Next()
	}
}

// cacheWriter keeps the chosen Cache-Control only on the answers it was chosen
// for: the file (200, 206) and its revalidation (304). Whatever else ends up
// being written -- the static handler's 404 for a file that is not there, a
// redirect, a 500 from the recovery middleware after a panic -- goes out as
// no-store, so no error can be pinned at the edge for a year.
//
// The value is re-applied on every WriteHeader, not only cleared: gin's static
// handler sets 404 first and lets the file server overwrite it with 200, and
// the headers are only flushed with the last one.
type cacheWriter struct {
	gin.ResponseWriter
	value string
}

func (w *cacheWriter) WriteHeader(code int) {
	switch code {
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified:
		w.Header().Set("Cache-Control", w.value)
	default:
		w.Header().Set("Cache-Control", cacheNone)
	}
	w.ResponseWriter.WriteHeader(code)
}
