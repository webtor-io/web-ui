package library

import (
	"bytes"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/poster_resolver"
)

// posterResource is the unified resource-keyed poster endpoint. One
// route, two modes:
//
//	GET /lib/poster/<resource_id>/<width>.jpg   → resized JPEG for UI cards
//	GET /lib/poster/<resource_id>/og.jpg        → 1200x630 OG canvas
//
// Source-resolution (IMDb → thumbnail → default) and caching live in
// services/poster_resolver. This handler is just HTTP packaging.
func (s *Handler) posterResource(c *gin.Context) {
	resourceID := c.Param("resource_id")
	if resourceID == "" {
		_ = c.AbortWithError(http.StatusBadRequest, errors.New("empty resource_id"))
		return
	}

	// ?force=1 bypasses both lazymap and S3 cache so a poster tweak
	// can be previewed without waiting for TTL expiry / S3 invalidation.
	// Gated to non-release builds so prod can't be DoS'd by burning
	// Lanczos cycles on demand.
	force := false
	if gin.Mode() != gin.ReleaseMode {
		q := c.Query("force")
		force = q != "" && q != "0" && q != "false"
	}

	res, err := s.posterResolver.Get(c.Request.Context(), resourceID, c.Param("file"), force)
	if err != nil {
		if errors.Is(err, poster_resolver.ErrNotFound) {
			_ = c.AbortWithError(http.StatusNotFound, err)
			return
		}
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Strip Set-Cookie before flushing so Cloudflare actually caches
	// the response. Session + CSRF middleware register a session
	// cookie on each request; CF defaults to bypassing the cache when
	// Set-Cookie is present in the origin response. Posters are pure
	// functions of (resource_id, file), no per-user variation — safe
	// to drop the cookie so the 24h max-age becomes effective edge-side.
	c.Writer.Header().Del("Set-Cookie")

	if match := c.Request.Header.Get("If-None-Match"); match != "" && match == res.ETag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("Content-Type", res.Mime)
	c.Header("Content-Length", strconv.Itoa(len(res.Body)))
	c.Header("ETag", res.ETag)
	c.Header("Cache-Control", "public, max-age=86400")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, bytes.NewReader(res.Body))
}
