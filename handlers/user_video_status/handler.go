package user_video_status

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	uvs "github.com/webtor-io/web-ui/services/user_video_status"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	svc *uvs.Service
}

func RegisterHandler(r *gin.Engine, svc *uvs.Service) {
	h := &Handler{svc: svc}

	gr := r.Group("/library")
	gr.Use(auth.HasAuth)

	// Movies
	gr.POST("/movie/:video_id/mark", h.markMovie)
	gr.POST("/movie/:video_id/unmark", h.unmarkMovie)

	// Series (whole series watched)
	gr.POST("/series/:video_id/mark", h.markSeries)
	gr.POST("/series/:video_id/unmark", h.unmarkSeries)

	// Individual episodes
	gr.POST("/series/:video_id/episode/:season/:episode/mark", h.markEpisode)
	gr.POST("/series/:video_id/episode/:season/:episode/unmark", h.unmarkEpisode)

	// Watched-ids filter — client sends visible IMDB ids, server returns
	// the subset the user has marked watched. Drives the discover marker.
	gr.POST("/watched/ids", h.filterWatchedIDs)
}

// --- Watched IDs filter ---

type watchedIDsRequest struct {
	IDs []string `json:"ids"`
}

type watchedIDsResponse struct {
	Watched []string `json:"watched"`
}

func (h *Handler) filterWatchedIDs(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	// auth.HasAuth middleware already guarantees HasAuth() — defensive check.
	if user == nil || !user.HasAuth() {
		c.Status(http.StatusUnauthorized)
		return
	}

	var req watchedIDsRequest
	if err := c.BindJSON(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	watched, err := h.svc.FilterWatchedIDs(c.Request.Context(), user.ID, req.IDs)
	if err != nil {
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	if watched == nil {
		watched = []string{}
	}
	c.JSON(http.StatusOK, watchedIDsResponse{Watched: watched})
}

// --- Movies ---

func (h *Handler) markMovie(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	if err := h.doMarkMovie(c.Request.Context(), user, videoID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Marked as watched")
}

func (h *Handler) doMarkMovie(ctx context.Context, user *auth.User, videoID string) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	if videoID == "" {
		return errors.New("video_id is required")
	}
	return h.svc.MarkMovieWatched(ctx, user.ID, videoID, models.UserVideoSourceManual)
}

func (h *Handler) unmarkMovie(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	if err := h.doUnmarkMovie(c.Request.Context(), user, videoID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Unmarked")
}

func (h *Handler) doUnmarkMovie(ctx context.Context, user *auth.User, videoID string) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	if videoID == "" {
		return errors.New("video_id is required")
	}
	return h.svc.UnmarkMovie(ctx, user.ID, videoID)
}

// --- Series ---

func (h *Handler) markSeries(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	if err := h.doMarkSeries(c.Request.Context(), user, videoID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Marked as watched")
}

func (h *Handler) doMarkSeries(ctx context.Context, user *auth.User, videoID string) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	if videoID == "" {
		return errors.New("video_id is required")
	}
	return h.svc.MarkSeriesWatched(ctx, user.ID, videoID, models.UserVideoSourceManual)
}

func (h *Handler) unmarkSeries(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	if err := h.doUnmarkSeries(c.Request.Context(), user, videoID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Unmarked")
}

func (h *Handler) doUnmarkSeries(ctx context.Context, user *auth.User, videoID string) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	if videoID == "" {
		return errors.New("video_id is required")
	}
	return h.svc.UnmarkSeries(ctx, user.ID, videoID)
}

// --- Episodes ---

func (h *Handler) markEpisode(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	season, episode, perr := parseSeasonEpisode(c)
	if perr != nil {
		web.RedirectWithError(c, perr)
		return
	}
	if err := h.doMarkEpisode(c.Request.Context(), user, videoID, season, episode); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Marked as watched")
}

func (h *Handler) doMarkEpisode(ctx context.Context, user *auth.User, videoID string, season, episode int16) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	if videoID == "" {
		return errors.New("video_id is required")
	}
	return h.svc.MarkEpisodeWatched(ctx, user.ID, videoID, season, episode, models.UserVideoSourceManual)
}

func (h *Handler) unmarkEpisode(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	season, episode, perr := parseSeasonEpisode(c)
	if perr != nil {
		web.RedirectWithError(c, perr)
		return
	}
	if err := h.doUnmarkEpisode(c.Request.Context(), user, videoID, season, episode); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Unmarked")
}

func (h *Handler) doUnmarkEpisode(ctx context.Context, user *auth.User, videoID string, season, episode int16) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	if videoID == "" {
		return errors.New("video_id is required")
	}
	return h.svc.UnmarkEpisode(ctx, user.ID, videoID, season, episode)
}

func parseSeasonEpisode(c *gin.Context) (int16, int16, error) {
	s, err := strconv.Atoi(c.Param("season"))
	if err != nil || s < 0 || s > 32767 {
		return 0, 0, errors.New("invalid season")
	}
	e, err := strconv.Atoi(c.Param("episode"))
	if err != nil || e < 0 || e > 32767 {
		return 0, 0, errors.New("invalid episode")
	}
	return int16(s), int16(e), nil
}
