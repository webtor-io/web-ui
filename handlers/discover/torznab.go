package discover

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/stremio"
)

type torznabStreamsRequest struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type torznabStreamsResponse struct {
	Streams []stremio.StreamItem `json:"streams"`
	// Indexers is the current name of every indexer that was queried.
	//
	// The page bootstrap (window._indexers) is rendered once, at page load,
	// and an indexer's name changes the first time it is searched: the human
	// name lives in the search results, not in the caps probe, so it is
	// learned there (see stremio.TorznabStream.learnTrackerName). Without
	// this the progress row keeps the name the page was loaded with for the
	// rest of the session, while the result rows below it already show the
	// new one — two names for one source in one modal.
	Indexers []indexerView `json:"indexers"`
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
	resp, err := torznabStreamsFor(c.Request.Context(), h.sb, h.pg.Get(), u, contentType, contentID)
	if err != nil {
		log.WithError(err).Error("failed to fetch torznab streams")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch streams"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// torznabStreamsFor is the Level 2 half: no gin, so it is testable with a
// stub builder.
func torznabStreamsFor(ctx context.Context, sb *stremio.Builder, db *pg.DB, u *auth.User, contentType, contentID string) (*torznabStreamsResponse, error) {
	out := &torznabStreamsResponse{Streams: []stremio.StreamItem{}, Indexers: []indexerView{}}
	svc, err := sb.BuildTorznabStreamsService(ctx, u)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return out, nil
	}
	resp, err := svc.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Streams != nil {
		out.Streams = resp.Streams
	}
	// Read the names back after the search, not before: this is the request
	// that teaches them.
	out.Indexers = indexerViews(ctx, db, u)
	return out, nil
}

// indexerViews is the per-indexer bootstrap, used both by the page and by the
// streams response so the two cannot disagree. A failure is logged and
// swallowed: the names are cosmetic, and the streams they came with are not.
func indexerViews(ctx context.Context, db *pg.DB, u *auth.User) []indexerView {
	out := []indexerView{}
	if db == nil || u == nil {
		return out
	}
	indexers, err := models.GetUserTorznabIndexers(ctx, db, u.ID)
	if err != nil {
		log.WithError(err).Warn("failed to list torznab indexers for the client bootstrap")
		return out
	}
	for _, ix := range indexers {
		out = append(out, indexerView{ID: ix.ID.String(), Name: ix.GetName()})
	}
	return out
}
