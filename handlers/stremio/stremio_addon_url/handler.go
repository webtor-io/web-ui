package stremio_addon_url

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	gr.POST("/batch-add", h.batchAdd)
	gr.POST("/delete/:id", h.delete)
	gr.POST("/update", h.update)
	gr.POST("/:id/refresh-snapshot", h.refreshSnapshot)
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
	web.RedirectWithSuccessAndMessage(c, "toast.addonAdded")
}

type batchAddRequest struct {
	URLs []string `json:"urls"`
}

type batchAddResponse struct {
	Added        int  `json:"added"`
	Skipped      int  `json:"skipped"`
	Limit        int  `json:"limit"`
	LimitReached bool `json:"limitReached"`
}

func (s *Handler) batchAdd(c *gin.Context) {
	var req batchAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user := auth.GetUserFromContext(c)
	cla := claims.GetFromContext(c)
	resp, err := s.batchAddAddonUrls(c.Request.Context(), req.URLs, user, cla)
	if err != nil {
		log.WithError(err).Error("failed to batch add addon URLs")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) batchAddAddonUrls(ctx context.Context, urls []string, user *auth.User, cla *claims.Data) (*batchAddResponse, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}

	maxLimit := 0 // 0 = unlimited (paid tier)
	if cla.Context.Tier.Id == 0 {
		maxLimit = 3
	}

	currentCount, err := models.CountUserStremioAddonUrls(ctx, db, user.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to count addon URLs")
	}

	resp := &batchAddResponse{Limit: maxLimit}
	for _, addonUrl := range urls {
		addonUrl = strings.TrimSpace(addonUrl)
		if addonUrl == "" {
			continue
		}

		// Validate URL format
		parsedUrl, err := url.Parse(addonUrl)
		if err != nil {
			resp.Skipped++
			continue
		}
		if parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https" {
			resp.Skipped++
			continue
		}
		if !strings.HasSuffix(parsedUrl.Path, "/manifest.json") && !strings.HasSuffix(parsedUrl.Path, "manifest.json") {
			resp.Skipped++
			continue
		}
		if s.domain != "" && (parsedUrl.Hostname() == s.domain || parsedUrl.Hostname() == "localhost" || parsedUrl.Hostname() == "127.0.0.1") {
			resp.Skipped++
			continue
		}

		// Check tier limit
		if maxLimit > 0 && currentCount >= maxLimit {
			resp.LimitReached = true
			resp.Skipped++
			continue
		}

		// Skip duplicates
		exists, err := models.StremioAddonUrlExists(ctx, db, user.ID, addonUrl)
		if err != nil {
			return nil, errors.Wrap(err, "failed to check addon URL existence")
		}
		if exists {
			resp.Skipped++
			continue
		}

		// Snapshot the manifest so the UI gets a name + capabilities up
		// front without waiting for a client-side fetch. Failures are
		// tolerated here: wizard items come from curated lists, and a
		// transient outage shouldn't block the user from binding the
		// addon to their account. The lazy refresh on Discover will
		// pick the snapshot up once the addon recovers.
		snapshot, snapErr := s.validator.ValidateAndFetch(addonUrl)
		if snapErr != nil {
			log.WithError(snapErr).WithField("url", addonUrl).Warn("batch-add: addon manifest unreachable, saving URL without snapshot")
			snapshot = nil
		}

		if err := models.CreateStremioAddonUrl(ctx, db, user.ID, addonUrl, snapshot); err != nil {
			return nil, errors.Wrap(err, "failed to create addon URL")
		}
		resp.Added++
		currentCount++
	}

	return resp, nil
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
	web.RedirectWithSuccessAndMessage(c, "toast.addonDeleted")
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

	// Validate addon URL availability and manifest structure, capturing
	// the snapshot so we don't waste the work the validator already did.
	snapshot, err := s.validator.ValidateAndFetch(addonUrl)
	if err != nil {
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
	return models.CreateStremioAddonUrl(ctx, db, user.ID, addonUrl, snapshot)
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
	web.RedirectWithSuccessAndMessage(c, "toast.settingsSaved")
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

// refreshSnapshot re-fetches an addon's manifest server-side and updates
// the cached fields. Used by:
//   - the lazy backfill from the Discover client (POST after a fresh
//     manifest fetch lands on an addon with stale or NULL fields)
//   - the per-addon "Refresh" button in the profile UI
//
// Returns the new snapshot in JSON so the caller can update its UI
// without an extra round-trip. 404 if the addon doesn't belong to the
// user; 502 if the upstream manifest fetch fails (so the client can keep
// the existing snapshot rather than nuking it).
type refreshSnapshotResponse struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Logo              string    `json:"logo"`
	ManifestID        string    `json:"manifestId"`
	ManifestVersion   string    `json:"manifestVersion"`
	ManifestResources []string  `json:"resources"`
	ManifestTypes     []string  `json:"types"`
	ManifestFetchedAt time.Time `json:"fetchedAt"`
}

func (s *Handler) refreshSnapshot(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	idStr := c.Param("id")
	addonID, err := uuid.FromString(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon id"})
		return
	}

	db := s.pg.Get()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no db"})
		return
	}

	addon, err := models.GetStremioAddonUrlByID(c.Request.Context(), db, addonID)
	if err != nil {
		log.WithError(err).Error("failed to load addon for refresh-snapshot")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}
	if addon == nil || addon.UserID != user.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "addon not found"})
		return
	}

	snapshot, err := s.validator.ValidateAndFetch(addon.Url)
	if err != nil {
		log.WithError(err).WithField("addon_id", addonID).Info("refresh-snapshot: upstream manifest fetch failed")
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	if err := models.UpdateStremioAddonUrlSnapshot(c.Request.Context(), db, addonID, user.ID, snapshot); err != nil {
		log.WithError(err).Error("refresh-snapshot: db update failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}

	now := time.Now()
	c.JSON(http.StatusOK, refreshSnapshotResponse{
		ID:                addonID.String(),
		Name:              snapshot.Name,
		Logo:              snapshot.Logo,
		ManifestID:        snapshot.ID,
		ManifestVersion:   snapshot.Version,
		ManifestResources: snapshot.Resources,
		ManifestTypes:     snapshot.Types,
		ManifestFetchedAt: now,
	})
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
