package vault

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

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
	web.RedirectWithSuccessAndMessage(c, "Added to vault")
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
