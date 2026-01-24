package vault

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	vault *vault.Vault
}

func RegisterHandler(r *gin.Engine, v *vault.Vault) {
	h := &Handler{
		vault: v,
	}
	gr := r.Group("/vault/pledge")
	gr.Use(auth.HasAuth)
	gr.POST("/add", h.addPledge)
	gr.POST("/remove", h.removePledge)
}

// addPledge handles HTTP request for creating a pledge (Level 1: HTTP interaction)
func (h *Handler) addPledge(c *gin.Context) {
	// Extract parameters from form
	resourceID := c.PostForm("resource_id")
	user := auth.GetUserFromContext(c)
	apiClaims := api.GetClaimsFromContext(c)

	// Call business logic
	err := h.createPledge(c.Request.Context(), resourceID, user, apiClaims)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}

	// Redirect with success
	web.RedirectWithSuccess(c)
}

// createPledge contains the core business logic for pledge creation (Level 2: Business logic)
func (h *Handler) createPledge(ctx context.Context, resourceID string, user *auth.User, apiClaims *api.Claims) error {
	// Validate resource_id
	if resourceID == "" {
		return errors.New("resource_id is required")
	}

	// Validate claims
	if apiClaims == nil {
		return errors.New("failed to get claims")
	}

	// Get or create resource
	resource, err := h.vault.GetOrCreateResource(ctx, apiClaims, resourceID)
	if err != nil {
		return err
	}

	// Create pledge
	_, err = h.vault.CreatePledge(ctx, user, resource)
	if err != nil {
		return err
	}

	return nil
}

// removePledge handles HTTP request for removing a pledge (Level 1: HTTP interaction)
func (h *Handler) removePledge(c *gin.Context) {
	// TODO: Extract parameters from form when implemented
	user := auth.GetUserFromContext(c)

	// Call business logic
	err := h.deletePledge(c.Request.Context(), user)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}

	// Redirect with success
	c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
}

// deletePledge contains the core business logic for pledge removal (Level 2: Business logic)
func (h *Handler) deletePledge(ctx context.Context, user *auth.User) error {
	// TODO: Implement pledge removal logic
	return errors.New("not implemented yet")
}
