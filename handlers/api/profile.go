package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/libapi"
)

// getProfile godoc
//
//	@Summary		Account state
//	@Description	Who the key belongs to, which plan the account is on, what the key is allowed to do, and the
//	@Description	preferences that affect what the other endpoints return.
//	@Tags			profile
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	libapi.ProfileResponse
//	@Failure		401	{object}	libapi.ErrorResponse
//	@Failure		402	{object}	libapi.ErrorResponse
//	@Failure		429	{object}	libapi.ErrorResponse	"Too many requests with this key — the `Retry-After` header says how long to wait"
//	@Router			/profile [get]
func (s *Handler) getProfile(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	scope, _ := c.Request.Context().Value(at.TokenScope{}).([]string)
	res := &libapi.ProfileResponse{
		UserID:   u.ID.String(),
		Email:    u.Email,
		Scopes:   scope,
		Settings: libapi.ProfileSettings{},
	}
	if cl := claims.GetFromContext(c); cl != nil && cl.Context != nil && cl.Context.Tier != nil {
		res.Tier = libapi.ProfileTier{ID: cl.Context.Tier.Id, Name: cl.Context.Tier.Name}
	}
	us, err := s.userSettings.Get(c.Request.Context(), u.ID)
	if err != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to load settings", err))
		return
	}
	if us != nil {
		res.Settings.ShowAdult = us.ShowAdult
	}
	c.JSON(http.StatusOK, res)
}

// patchProfile godoc
//
//	@Summary		Update account preferences
//	@Description	Partial update: a field left out of the body keeps its current value. Requires the `api:write`
//	@Description	scope.
//	@Description
//	@Description	Preferences are read on the hot path and cached for a few minutes, so a change can take that long
//	@Description	to show up in poster URLs — this endpoint invalidates its own cache entry, but other replicas
//	@Description	expire on their own schedule.
//	@Tags			profile
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		libapi.ProfileSettingsRequest	true	"Fields to change"
//	@Success		200		{object}	libapi.ProfileResponse
//	@Failure		400		{object}	libapi.ErrorResponse
//	@Failure		401		{object}	libapi.ErrorResponse
//	@Failure		402		{object}	libapi.ErrorResponse
//	@Failure		403		{object}	libapi.ErrorResponse
//	@Failure		429		{object}	libapi.ErrorResponse	"Too many requests with this key — the `Retry-After` header says how long to wait"
//	@Router			/profile [patch]
func (s *Handler) patchProfile(c *gin.Context) {
	var req libapi.ProfileSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest,
			"body must be a JSON object", err))
		return
	}
	u := auth.GetUserFromContext(c)
	current, err := s.userSettings.Get(c.Request.Context(), u.ID)
	if err != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to load settings", err))
		return
	}
	// Read-modify-write, because the store has no partial update: the struct is
	// small and PATCH semantics live here rather than in the model (see
	// models.UpsertUserSettings).
	next := &models.UserSettings{UserID: u.ID}
	if current != nil {
		next.ShowAdult = current.ShowAdult
	}
	if req.ShowAdult != nil {
		next.ShowAdult = *req.ShowAdult
	}
	if err := s.userSettings.Set(c.Request.Context(), next); err != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to save settings", err))
		return
	}
	s.getProfile(c)
}
