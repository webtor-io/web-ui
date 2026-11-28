package discover

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/poster_resolver"
)

var infoHashRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

type savePosterRequest struct {
	ResourceID string `json:"resource_id"`
	PosterURL  string `json:"poster_url"`
}

// savePoster remembers the poster the user was looking at when they started a
// torrent from Discover, so Continue Watching can show the same artwork. The
// row is first-writer-wins (see models.SaveResourcePoster). Best effort for
// the page: it never blocks playback, so bad input is a 400 and nothing more.
func (h *Handler) savePoster(c *gin.Context) {
	var req savePosterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	req.ResourceID = strings.ToLower(strings.TrimSpace(req.ResourceID))
	req.PosterURL = strings.TrimSpace(req.PosterURL)
	if !infoHashRe.MatchString(req.ResourceID) {
		c.Status(http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := poster_resolver.ValidateStoredPosterURL(ctx, req.PosterURL); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	db := h.pg.Get()
	if db == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if err := models.SaveResourcePoster(ctx, db, req.ResourceID, req.PosterURL); err != nil {
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusNoContent)
}
