package watch_history

import (
	"net/http"

	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
)

type Handler struct {
	pg *cs.PG
}

type PositionRequest struct {
	ResourceID string  `json:"resource_id"`
	Path       string  `json:"path"`
	Position   float32 `json:"position"`
	Duration   float32 `json:"duration"`
}

type PositionResponse struct {
	Position float32 `json:"position"`
	Duration float32 `json:"duration"`
	Watched  bool    `json:"watched"`
}

func RegisterHandler(r *gin.Engine, pg *cs.PG) {
	h := &Handler{pg: pg}
	r.PUT("/watch/position", h.updatePosition)
	r.GET("/watch/position", h.getPosition)
}

func (h *Handler) updatePosition(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	if !user.HasAuth() {
		c.Status(http.StatusNoContent)
		return
	}

	var req PositionRequest
	if err := c.BindJSON(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if req.ResourceID == "" || req.Path == "" || req.Duration <= 0 {
		c.Status(http.StatusBadRequest)
		return
	}

	db := h.pg.Get()
	if db == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	wh := &models.WatchHistory{
		UserID:     user.ID,
		ResourceID: req.ResourceID,
		Path:       req.Path,
		Position:   req.Position,
		Duration:   req.Duration,
	}

	if err := models.UpsertWatchPosition(c.Request.Context(), db, wh); err != nil {
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) getPosition(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	if !user.HasAuth() {
		c.Status(http.StatusNoContent)
		return
	}

	resourceID := c.Query("resource-id")
	path := c.Query("path")
	if resourceID == "" || path == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	db := h.pg.Get()
	if db == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	wh, err := models.GetWatchPosition(c.Request.Context(), db, user.ID, resourceID, path)
	if err != nil {
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}

	if wh == nil {
		c.Status(http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, &PositionResponse{
		Position: wh.Position,
		Duration: wh.Duration,
		Watched:  wh.Watched,
	})
}
