package stremio_addon_url

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/stremio"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	pg        *cs.PG
	validator *stremio.AddonValidator
	domain    string
}

func RegisterHandler(c *cli.Context, av *stremio.AddonValidator, r *gin.Engine, pg *cs.PG) error {
	d := c.String(common.DomainFlag)
	if d != "" {
		u, err := url.Parse(d)
		if err != nil {
			return err
		}
		d = u.Hostname()
	}

	h := &Handler{
		pg:        pg,
		validator: av,
		domain:    d,
	}

	gr := r.Group("/stremio/addon-url")
	gr.Use(auth.HasAuth)
	gr.POST("/add", h.add)
	gr.POST("/delete/:id", h.delete)
	gr.POST("/update", h.update)
	return nil
}

func (s *Handler) add(c *gin.Context) {
	addonUrl := strings.TrimSpace(c.PostForm("url"))
	user := auth.GetUserFromContext(c)
	cla := claims.GetFromContext(c)
	err := s.addAddonUrl(c.Request.Context(), addonUrl, user, cla)
	if err != nil {
		log.WithError(err).Error("failed to add addon URL")
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) delete(c *gin.Context) {
	id := c.Param("id")
	user := auth.GetUserFromContext(c)
	err := s.deleteAddonUrl(c.Request.Context(), id, user)
	if err != nil {
		log.WithError(err).Error("failed to delete addon URL")
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) addAddonUrl(ctx context.Context, addonUrl string, user *auth.User, cla *claims.Data) (err error) {
	// Get URL from form data
	if addonUrl == "" {
		return errors.New("no addon URL provided")
	}

	// Validate URL format
	parsedUrl, err := url.Parse(addonUrl)
	if err != nil {
		return errors.New("invalid URL format")
	}

	// Ensure it's HTTP or HTTPS
	if parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https" {
		return errors.New("URL must use http or https protocol")
	}

	// Ensure it ends with manifest.json for Stremio addons
	if !strings.HasSuffix(parsedUrl.Path, "/manifest.json") && !strings.HasSuffix(parsedUrl.Path, "manifest.json") {
		return errors.New("URL must point to a Stremio addon manifest.json file")
	}

	// Prevent users from adding Webtor's own manifest URL
	if s.domain != "" && (parsedUrl.Hostname() == s.domain || parsedUrl.Hostname() == "localhost" || parsedUrl.Hostname() == "127.0.0.1") {
		return errors.New("cannot add Webtor's own manifest URL")
	}

	// Validate addon URL availability and manifest structure
	if err := s.validator.ValidateURL(addonUrl); err != nil {
		return err
	}

	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}

	if cla.Context.Tier.Id == 0 {
		// Check current addon URL count for user
		currentCount, err := models.CountUserStremioAddonUrls(ctx, db, user.ID)
		if err != nil {
			return err
		}

		// Restrict to maximum 3 addon URLs for free tier (more than domains since they're just URLs)
		if currentCount >= 3 {
			return errors.New("maximum 3 addon URLs allowed for free tier")
		}
	}

	// Check if URL already exists
	urlExists, err := models.StremioAddonUrlExists(ctx, db, user.ID, addonUrl)
	if err != nil {
		return
	}
	if urlExists {
		return errors.New("addon URL already exists")
	}

	// Create new addon URL
	return models.CreateStremioAddonUrl(ctx, db, user.ID, addonUrl)
}

func (s *Handler) update(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	// Parse form data
	deletedAddonsStr := c.PostForm("deleted_addons")
	addonOrder := c.PostForm("addon_order")

	err := s.updateAddonUrls(deletedAddonsStr, addonOrder, c, user)
	if err != nil {
		log.WithError(err).Error("failed to update addon URLs")
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) updateAddonUrls(deletedAddonsStr, addonOrder string, c *gin.Context, user *auth.User) error {
	// Get database connection
	db := s.pg.Get()
	if db == nil {
		return errors.New("no database connection available")
	}

	// Process deleted addons first
	if deletedAddonsStr != "" {
		deletedAddonIDs := strings.Split(deletedAddonsStr, ",")
		for _, idStr := range deletedAddonIDs {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}

			addonID, err := uuid.FromString(idStr)
			if err != nil {
				log.WithField("user_id", user.ID).
					WithField("addon_id", idStr).
					Warn("invalid addon ID in deleted list")
				continue
			}

			err = s.deleteAddonUrl(c.Request.Context(), idStr, user)
			if err != nil {
				log.WithError(err).
					WithField("user_id", user.ID).
					WithField("addon_id", addonID).
					Error("failed to delete addon URL")
			}
		}
	}

	// Get user's remaining addons
	addons, err := models.GetAllUserStremioAddonUrls(c.Request.Context(), db, user.ID)
	if err != nil {
		return errors.Wrap(err, "failed to get user addon URLs")
	}

	// Update enabled status for each addon based on form data
	for _, addon := range addons {
		enabledFieldName := "addon_" + addon.ID.String() + "_enabled"
		enabled := c.PostForm(enabledFieldName) == "on"

		if addon.Enabled != enabled {
			addon.Enabled = enabled
			err = models.UpdateStremioAddonUrl(c.Request.Context(), db, &addon)
			if err != nil {
				return errors.Wrap(err, "failed to update addon URL")
			}
		}
	}

	// Handle addon reordering if order is provided
	if addonOrder != "" {
		orderSlice := strings.Split(addonOrder, ",")
		for i, idStr := range orderSlice {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}

			addonID, err := uuid.FromString(idStr)
			if err != nil {
				continue // skip invalid IDs
			}

			// Find addon and update priority
			for _, addon := range addons {
				if addon.ID == addonID {
					newPriority := int16(len(orderSlice) - i) // Higher index = lower priority
					if addon.Priority != newPriority {
						addon.Priority = newPriority
						err = models.UpdateStremioAddonUrl(c.Request.Context(), db, &addon)
						if err != nil {
							log.WithError(err).
								WithField("user_id", user.ID).
								WithField("addon_id", addon.ID).
								Error("failed to update addon URL priority")
						}
					}
					break
				}
			}
		}
	}

	log.WithField("user_id", user.ID).Info("addon URLs updated successfully")
	return nil
}

func (s *Handler) deleteAddonUrl(ctx context.Context, idStr string, user *auth.User) (err error) {
	id, err := uuid.FromString(idStr)
	if err != nil {
		return
	}

	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}

	// Delete addon URL owned by the current user
	return models.DeleteUserStremioAddonUrl(ctx, db, id, user.ID)
}
