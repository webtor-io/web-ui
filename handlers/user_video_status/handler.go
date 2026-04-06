package user_video_status

import (
	"context"
	"net/http"
	"net/url"
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
	gr.POST("/movie/:video_id/rate", h.rateMovie)
	gr.POST("/movie/:video_id/unrate", h.unrateMovie)

	// Series (whole series watched)
	gr.POST("/series/:video_id/mark", h.markSeries)
	gr.POST("/series/:video_id/unmark", h.unmarkSeries)
	gr.POST("/series/:video_id/rate", h.rateSeries)
	gr.POST("/series/:video_id/unrate", h.unrateSeries)

	// Individual episodes
	gr.POST("/series/:video_id/episode/:season/:episode/mark", h.markEpisode)
	gr.POST("/series/:video_id/episode/:season/:episode/unmark", h.unmarkEpisode)

	// User status filter — client sends visible IMDB ids, server returns
	// watched + rating state for each. Drives discover badges.
	gr.POST("/status", h.filterUserStatus)
}

// --- User status filter ---

type userStatusRequest struct {
	IDs []string `json:"ids"`
}

type userStatusItem struct {
	Watched bool  `json:"watched"`
	Rating  int16 `json:"rating,omitempty"`
}

type userStatusResponse struct {
	Statuses map[string]*userStatusItem `json:"statuses"`
}

func (h *Handler) filterUserStatus(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	// auth.HasAuth middleware already guarantees HasAuth() — defensive check.
	if user == nil || !user.HasAuth() {
		c.Status(http.StatusUnauthorized)
		return
	}

	var req userStatusRequest
	if err := c.BindJSON(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	statuses, err := h.svc.FilterUserStatus(c.Request.Context(), user.ID, req.IDs)
	if err != nil {
		_ = c.Error(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	resp := userStatusResponse{
		Statuses: make(map[string]*userStatusItem, len(statuses)),
	}
	for vid, st := range statuses {
		resp.Statuses[vid] = &userStatusItem{
			Watched: st.Watched,
			Rating:  st.Rating,
		}
	}
	c.JSON(http.StatusOK, resp)
}

// --- Movies ---

func (h *Handler) markMovie(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	if err := h.doMarkMovie(c.Request.Context(), user, videoID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	redirectWithRatePrompt(c, "Marked as watched")
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
	redirectWithRatePrompt(c, "Marked as watched")
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

// --- Rating ---

func (h *Handler) rateMovie(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	rating, err := parseRating(c)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}
	if err := h.doRateMovie(c.Request.Context(), user, videoID, rating); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Rating saved")
}

func (h *Handler) doRateMovie(ctx context.Context, user *auth.User, videoID string, rating int16) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	return h.svc.RateMovie(ctx, user.ID, videoID, rating)
}

func (h *Handler) unrateMovie(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	if err := h.doUnrateMovie(c.Request.Context(), user, videoID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Rating removed")
}

func (h *Handler) doUnrateMovie(ctx context.Context, user *auth.User, videoID string) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	return h.svc.UnrateMovie(ctx, user.ID, videoID)
}

func (h *Handler) rateSeries(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	rating, err := parseRating(c)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}
	if err := h.doRateSeries(c.Request.Context(), user, videoID, rating); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Rating saved")
}

func (h *Handler) doRateSeries(ctx context.Context, user *auth.User, videoID string, rating int16) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	return h.svc.RateSeries(ctx, user.ID, videoID, rating)
}

func (h *Handler) unrateSeries(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	videoID := c.Param("video_id")
	if err := h.doUnrateSeries(c.Request.Context(), user, videoID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "Rating removed")
}

func (h *Handler) doUnrateSeries(ctx context.Context, user *auth.User, videoID string) error {
	if user == nil || !user.HasAuth() {
		return errors.New("unauthorized")
	}
	return h.svc.UnrateSeries(ctx, user.ID, videoID)
}

func parseRating(c *gin.Context) (int16, error) {
	r, err := strconv.Atoi(c.PostForm("rating"))
	if err != nil || r < 1 || r > 10 {
		return 0, errors.New("rating must be between 1 and 10")
	}
	return int16(r), nil
}

// redirectWithRatePrompt redirects back to the resource page with both the
// success status and rate-form=true, so the rating modal auto-opens after
// marking as watched.
func redirectWithRatePrompt(c *gin.Context, message string) {
	u, err := url.Parse(c.GetHeader("X-Return-Url"))
	if err != nil || u == nil {
		web.RedirectWithSuccessAndMessage(c, message)
		return
	}
	q := u.Query()
	q.Set("status", "success")
	q.Set("from", c.Request.URL.Path)
	q.Set("message", message)
	q.Set("rate-form", "true")
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
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
