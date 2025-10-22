package backends

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
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
	gr := r.Group("/streaming/backends")
	gr.Use(auth.HasAuth)
	gr.POST("/create", h.createBackend)
	gr.POST("/update", h.updateBackends)
}

func (s *Handler) createBackend(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	// Parse form data
	backendType := models.StreamingBackendType(c.PostForm("type"))
	accessToken := strings.TrimSpace(c.PostForm("access_token"))
	enabled := c.PostForm("enabled") == "on"

	err := s.addStreamingBackend(c.Request.Context(), backendType, accessToken, enabled, user)
	if err != nil {
		log.WithError(err).Error("failed to add streaming backend")
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) addStreamingBackend(ctx context.Context, backendType models.StreamingBackendType, accessToken string, enabled bool, user *auth.User) error {
	// Validate backend type
	if backendType != models.StreamingBackendTypeWebtor &&
		backendType != models.StreamingBackendTypeRealDebrid &&
		backendType != models.StreamingBackendTypeTorbox {
		return errors.New("invalid backend type")
	}

	// Get database connection
	db := s.pg.Get()
	if db == nil {
		return errors.New("no database connection available")
	}

	// Check if backend already exists
	exists, err := models.StreamingBackendExists(ctx, db, user.ID, backendType)
	if err != nil {
		return errors.Wrap(err, "failed to check if streaming backend exists")
	}

	if exists {
		return errors.New("backend already exists")
	}

	// Set default priority (no longer user-configurable)
	priority := int64(1)

	// Create backend
	backend := &models.StreamingBackend{
		UserID:   user.ID,
		Type:     backendType,
		Priority: int16(priority),
		Enabled:  enabled,
		Config:   make(models.StreamingBackendConfig),
	}

	// Set access token if provided
	if accessToken != "" {
		backend.AccessToken = accessToken
	}

	err = models.CreateStreamingBackend(ctx, db, backend)
	if err != nil {
		return errors.Wrap(err, "failed to create streaming backend")
	}

	log.WithField("user_id", user.ID).
		WithField("type", backendType).
		WithField("backend_id", backend.ID).
		Info("streaming backend created successfully")

	return nil
}

func (s *Handler) updateBackends(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	// Parse form data
	deletedBackendsStr := c.PostForm("deleted_backends")
	backendOrder := c.PostForm("backend_order")

	err := s.updateStreamingBackends(deletedBackendsStr, backendOrder, c, user)
	if err != nil {
		log.WithError(err).Error("failed to update streaming backends")
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

func (s *Handler) updateStreamingBackends(deletedBackendsStr, backendOrder string, c *gin.Context, user *auth.User) error {
	// Get database connection
	db := s.pg.Get()
	if db == nil {
		return errors.New("no database connection available")
	}

	// Process deleted backends first
	if deletedBackendsStr != "" {
		deletedBackendIDs := strings.Split(deletedBackendsStr, ",")
		for _, idStr := range deletedBackendIDs {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}

			backendID, err := uuid.FromString(idStr)
			if err != nil {
				log.WithField("user_id", user.ID).
					WithField("backend_id", idStr).
					Warn("invalid backend ID in deleted list")
				continue
			}

			err = s.deleteStreamingBackend(c.Request.Context(), backendID, user)
			if err != nil {
				log.WithError(err).
					WithField("user_id", user.ID).
					WithField("backend_id", backendID).
					Error("failed to delete streaming backend")
			}
		}
	}

	// Get user's remaining backends
	backends, err := models.GetUserStreamingBackends(c.Request.Context(), db, user.ID)
	if err != nil {
		return errors.Wrap(err, "failed to get user streaming backends")
	}

	// Update enabled status for each backend based on form data
	for _, backend := range backends {
		enabledFieldName := "backend_" + backend.ID.String() + "_enabled"
		enabled := c.PostForm(enabledFieldName) == "on"

		if backend.Enabled != enabled {
			backend.Enabled = enabled
			err = models.UpdateStreamingBackend(c.Request.Context(), db, backend)
			if err != nil {
				return errors.Wrap(err, "failed to update streaming backend")
			}
		}
	}

	// Handle backend reordering if order is provided
	if backendOrder != "" {
		orderSlice := strings.Split(backendOrder, ",")
		for i, idStr := range orderSlice {
			idStr = strings.TrimSpace(idStr)
			if idStr == "" {
				continue
			}

			backendID, err := uuid.FromString(idStr)
			if err != nil {
				continue // skip invalid IDs
			}

			// Find backend and update priority
			for _, backend := range backends {
				if backend.ID == backendID {
					newPriority := int16(len(orderSlice) - i) // Higher index = lower priority
					if backend.Priority != newPriority {
						backend.Priority = newPriority
						err = models.UpdateStreamingBackend(c.Request.Context(), db, backend)
						if err != nil {
							log.WithError(err).
								WithField("user_id", user.ID).
								WithField("backend_id", backend.ID).
								Error("failed to update streaming backend priority")
						}
					}
					break
				}
			}
		}
	}

	log.WithField("user_id", user.ID).Info("streaming backends updated successfully")
	return nil
}

func (s *Handler) deleteStreamingBackend(ctx context.Context, backendID uuid.UUID, user *auth.User) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("no database connection available")
	}

	// Get backend to verify ownership before deletion
	backend, err := models.GetStreamingBackendByID(ctx, db, backendID)
	if err != nil {
		return errors.Wrap(err, "failed to get streaming backend")
	}
	if backend == nil {
		return errors.New("streaming backend not found")
	}
	if backend.UserID != user.ID {
		return errors.New("access denied")
	}

	// Don't allow deleting Webtor backend
	if backend.Type == models.StreamingBackendTypeWebtor {
		return errors.New("cannot delete webtor streaming backend")
	}

	err = models.DeleteStreamingBackend(ctx, db, backendID)
	if err != nil {
		return errors.Wrap(err, "failed to delete streaming backend")
	}

	log.WithField("user_id", user.ID).
		WithField("backend_id", backendID).
		Info("streaming backend deleted successfully")

	return nil
}

// BackendValidator interface for validating streaming backends
type BackendValidator interface {
	ValidateBackend(backendType models.StreamingBackendType, accessToken string) error
}

// StubValidator is a placeholder validator
type StubValidator struct{}

func (sv *StubValidator) ValidateBackend(backendType models.StreamingBackendType, accessToken string) error {
	// TODO: Implement actual validation logic for each backend type
	// This is a stub as mentioned in the issue description
	log.WithField("backend_type", backendType).
		WithField("has_token", accessToken != "").
		Info("stub validation called - not yet implemented")
	return nil
}
