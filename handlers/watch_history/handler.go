package watch_history

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/user_video_status"
)

type Handler struct {
	pg          *cs.PG
	videoStatus *user_video_status.Service
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

func RegisterHandler(r *gin.Engine, pg *cs.PG, videoStatus *user_video_status.Service) {
	h := &Handler{pg: pg, videoStatus: videoStatus}
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

	transitioned, err := models.UpsertWatchPosition(c.Request.Context(), db, wh)
	if err != nil {
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}

	// On the false → true transition (user just crossed 90% for the first
	// time), resolve the file to an IMDB video_id and auto-mark it in
	// user_video_status. Failures here are logged but do NOT fail the request:
	// the resume position is the critical path; the IMDB-level profile update
	// is a best-effort side effect.
	if transitioned && h.videoStatus != nil {
		ctx := c.Request.Context()
		ref, rerr := models.ResolveVideoFromResourcePath(ctx, db, req.ResourceID, req.Path)
		if rerr != nil {
			log.WithError(rerr).
				WithField("resource_id", req.ResourceID).
				WithField("path", req.Path).
				Warn("failed to resolve video_id for auto-watched mark")
		} else if ref != nil {
			var merr error
			switch ref.Kind {
			case models.VideoRefKindMovie:
				merr = h.videoStatus.MarkMovieWatched(ctx, user.ID, ref.VideoID, models.UserVideoSourceAuto90pct)
			case models.VideoRefKindEpisode:
				merr = h.videoStatus.MarkEpisodeWatched(ctx, user.ID, ref.VideoID, ref.Season, ref.Episode, models.UserVideoSourceAuto90pct)
			}
			if merr != nil {
				log.WithError(merr).
					WithField("video_id", ref.VideoID).
					WithField("kind", ref.Kind).
					Warn("failed to auto-mark user video status")
			}
		}
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
