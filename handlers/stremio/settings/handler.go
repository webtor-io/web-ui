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

	// Get preferred qualities from form
	qualities4k := c.PostForm("quality_4k") == "on"
	qualities1080p := c.PostForm("quality_1080p") == "on"
	qualities720p := c.PostForm("quality_720p") == "on"
	qualitiesOther := c.PostForm("quality_other") == "on"

	// Parse quality_order field to determine ordering
	qualityOrder := c.PostForm("quality_order")

	// Create map of quality settings for easy lookup
	qualitySettings := map[string]bool{
		"4k":    qualities4k,
		"1080p": qualities1080p,
		"720p":  qualities720p,
		"other": qualitiesOther,
	}

	// Build PreferredQualities array according to quality_order
	var orderedQualities []models.QualitySetting
	orderSlice := strings.Split(qualityOrder, ",")
	if len(orderSlice) == 0 {
		orderSlice = []string{"4k", "1080p", "720p", "other"}
	}
	// Split the order string (assuming comma-separated values)
	for _, quality := range orderSlice {
		quality = strings.TrimSpace(quality)
		if enabled, exists := qualitySettings[quality]; exists {
			orderedQualities = append(orderedQualities, models.QualitySetting{
				Quality: quality,
				Enabled: enabled,
			})
			// Remove from map to avoid duplicates
			delete(qualitySettings, quality)
		}
	}
	// Add any remaining qualities that weren't in the order
	for quality, enabled := range qualitySettings {
		orderedQualities = append(orderedQualities, models.QualitySetting{
			Quality: quality,
			Enabled: enabled,
		})
	}

	settingsData.PreferredQualities = orderedQualities

	// Get database connection
	db := s.pg.Get()
	if db == nil {
		log.WithField("user_id", user.ID).Error("no database connection available")
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no database connection available"))
		return
	}

	// Save to database
	err := models.CreateOrUpdateStremioSettings(db, user.ID, settingsData)
	if err != nil {
		log.WithError(err).
			WithField("user_id", user.ID).
			Error("failed to save stremio settings")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	log.WithField("user_id", user.ID).Info("stremio settings updated successfully")

	// Redirect back to originating page
	returnURL := c.GetHeader("X-Return-Url")
	if returnURL == "" {
		returnURL = "/profile"
	}
	c.Redirect(http.StatusFound, returnURL)
}
