package settings

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	at *at.AccessToken
	pg *cs.PG
}

func NewHandler(at *at.AccessToken, pg *cs.PG) *Handler {
	return &Handler{at: at, pg: pg}
}

func RegisterHandler(r *gin.Engine, at *at.AccessToken, pg *cs.PG) {
	h := NewHandler(at, pg)
	gr := r.Group("/stremio/settings")
	gr.Use(auth.HasAuth)
	gr.POST("/update", h.updateSettings)
}

func (s *Handler) updateSettings(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	// Parse form data
	settingsData := &models.StremioSettingsData{}

	// Get preferred resolutions from form
	resolution4k := c.PostForm("resolution_4k") == "on"
	resolution1080p := c.PostForm("resolution_1080p") == "on"
	resolution720p := c.PostForm("resolution_720p") == "on"
	resolutionOther := c.PostForm("resolution_other") == "on"

	// Parse resolution_order field to determine ordering
	resolutionOrder := c.PostForm("resolution_order")

	// Create map of resolution settings for easy lookup
	resolutionSettings := map[string]bool{
		"4k":    resolution4k,
		"1080p": resolution1080p,
		"720p":  resolution720p,
		"other": resolutionOther,
	}

	// Build PreferredResolutions array according to resolution_order
	var orderedQualities []models.ResolutionSetting
	orderSlice := strings.Split(resolutionOrder, ",")
	if len(orderSlice) == 0 {
		orderSlice = []string{"4k", "1080p", "720p", "other"}
	}
	// Split the order string (assuming comma-separated values)
	for _, resolution := range orderSlice {
		resolution = strings.TrimSpace(resolution)
		if enabled, exists := resolutionSettings[resolution]; exists {
			orderedQualities = append(orderedQualities, models.ResolutionSetting{
				Resolution: resolution,
				Enabled:    enabled,
			})
			// Remove from map to avoid duplicates
			delete(resolutionSettings, resolution)
		}
	}
	// Add any remaining resolutions that weren't in the order
	for resolution, enabled := range resolutionSettings {
		orderedQualities = append(orderedQualities, models.ResolutionSetting{
			Resolution: resolution,
			Enabled:    enabled,
		})
	}

	settingsData.PreferredResolutions = orderedQualities

	// Get database connection
	db := s.pg.Get()
	if db == nil {
		log.WithField("user_id", user.ID).Error("no database connection available")
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no database connection available"))
		return
	}

	// Save to database
	err := models.CreateOrUpdateStremioSettings(c.Request.Context(), db, user.ID, settingsData)
	if err != nil {
		log.WithError(err).
			WithField("user_id", user.ID).
			Error("failed to save stremio settings")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	log.WithField("user_id", user.ID).Info("stremio settings updated successfully")

	web.RedirectWithSuccessAndMessage(c, "Settings saved")
}
