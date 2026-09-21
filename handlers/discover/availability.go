package discover

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/cache_index"
)

// POST /discover/availability -- which of these streams start at once.
//
// The Stremio addon marks its own answers with the bolt (EnrichStream), but
// Discover asks the user's addons from the browser, so its stream list never
// passes that code. The page sends the hashes it got and paints the bolt
// itself; cached streams are also sorted first, which is the point of the
// exercise -- steer viewers to what is already here instead of spreading them
// over forty releases of the same film (owner, 2026-09-21).
//
// It answers from our own index only (cache_index + vaulted resources): two
// indexed queries, no torrent is touched, nothing is asked of the seeders.

// maxAvailabilityItems bounds one request. A busy title returns 100-200
// streams over all addons; more than this is not a stream list.
const maxAvailabilityItems = 500

const availabilityTimeout = 3 * time.Second

var availabilityHashRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

type availabilityItem struct {
	InfoHash string `json:"infoHash"`
	// FileIdx is absent when the addon did not name a file; any cached file
	// of the torrent then counts.
	FileIdx *int `json:"fileIdx"`
}

type availabilityRequest struct {
	Items []availabilityItem `json:"items"`
}

type availabilityResponse struct {
	// Cached holds the positions (in the request's items) that are cached.
	// Positions, not hashes: the same hash can come with different files.
	Cached []int `json:"cached"`
}

// availabilityLookup is the part of services/cache_index this needs.
type availabilityLookup interface {
	Lookup(ctx context.Context, hashes []string) (*cache_index.Availability, error)
}

func (h *Handler) availability(c *gin.Context) {
	var req availabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload"})
		return
	}
	if len(req.Items) > maxAvailabilityItems {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too many items"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), availabilityTimeout)
	defer cancel()
	resp, err := availabilityFor(ctx, h.ci, req.Items)
	if err != nil {
		// The bolt is a hint. A list without it is still a list, so the page
		// gets an empty answer rather than an error to handle.
		log.WithError(err).Warn("failed to look up stream availability")
		c.JSON(http.StatusOK, availabilityResponse{Cached: []int{}})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// availabilityFor is the Level 2 half.
func availabilityFor(ctx context.Context, ci availabilityLookup, items []availabilityItem) (*availabilityResponse, error) {
	out := &availabilityResponse{Cached: []int{}}
	if ci == nil || len(items) == 0 {
		return out, nil
	}
	seen := map[string]struct{}{}
	hashes := make([]string, 0, len(items))
	norm := make([]string, len(items))
	for i, it := range items {
		hash := strings.ToLower(strings.TrimSpace(it.InfoHash))
		if !availabilityHashRe.MatchString(hash) {
			continue
		}
		norm[i] = hash
		if _, ok := seen[hash]; !ok {
			seen[hash] = struct{}{}
			hashes = append(hashes, hash)
		}
	}
	if len(hashes) == 0 {
		return out, nil
	}
	av, err := ci.Lookup(ctx, hashes)
	if err != nil {
		return nil, err
	}
	for i, it := range items {
		if norm[i] == "" {
			continue
		}
		idx := -1
		if it.FileIdx != nil && *it.FileIdx >= 0 {
			idx = *it.FileIdx
		}
		if av.Cached(norm[i], idx) {
			out.Cached = append(out.Cached, i)
		}
	}
	return out, nil
}
