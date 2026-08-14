package discover

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/stremio"
)

type torznabStreamsRequest struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type torznabStreamsResponse struct {
	Streams []stremio.StreamItem `json:"streams"`
}

// torznabStreams is the Level 1 handler for POST /discover/torznab/streams.
//
// This endpoint exists because Discover's client-side model stops at
// Torznab: indexers answer without CORS headers, and the self-hosted ones
// live on plain http where a browser on an https page cannot reach them at
// all. So the browser fetches Stremio addon streams itself and asks the
// server for the indexer half, then merges the two lists.
func (h *Handler) torznabStreams(c *gin.Context) {
	if h.sb == nil {
		c.JSON(http.StatusOK, torznabStreamsResponse{Streams: []stremio.StreamItem{}})
		return
	}
	var req torznabStreamsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad payload"})
		return
	}
	contentType := strings.TrimSpace(req.Type)
	contentID := strings.TrimSpace(req.ID)
	// The type reaches an outbound URL path (the Cinemeta lookup) and a
	// cache key, so it is whitelisted rather than trimmed: these are the
	// only two types Webtor streams anyway.
	if contentType != "movie" && contentType != "series" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be movie or series"})
		return
	}
	if contentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	u := auth.GetUserFromContext(c)
	streams, err := torznabStreamsFor(c.Request.Context(), h.sb, u, contentType, contentID)
	if err != nil {
		log.WithError(err).Error("failed to fetch torznab streams")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch streams"})
		return
	}
	c.JSON(http.StatusOK, torznabStreamsResponse{Streams: streams})
}

// torznabStreamsFor is the Level 2 half: no gin, so it is testable with a
// stub builder.
func torznabStreamsFor(ctx context.Context, sb *stremio.Builder, u *auth.User, contentType, contentID string) ([]stremio.StreamItem, error) {
	svc, err := sb.BuildTorznabStreamsService(ctx, u)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return []stremio.StreamItem{}, nil
	}
	resp, err := svc.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Streams == nil {
		return []stremio.StreamItem{}, nil
	}
	return resp.Streams, nil
}
