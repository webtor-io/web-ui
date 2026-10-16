package vault

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// removePledge handles HTTP request for removing pledges (Level 1: HTTP
// interaction). One resource_id comes from the resource page's remove modal,
// several from the Vault page's "remove selected" (partials/bulk_select.html).
func (h *Handler) removePledge(c *gin.Context) {
	// Extract parameters from form
	resourceIDs, err := web.FormBulkIDs(c)
	user := auth.GetUserFromContext(c)

	// Call business logic
	if err == nil {
		err = h.deletePledges(c.Request.Context(), resourceIDs, user)
	}
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}

	// Redirect with success
	web.RedirectWithSuccessAndMessage(c, "toast.removedFromVault")
}

// deletePledges removes every pledge it is given (Level 2: Business logic).
// One that fails does not stop the rest: each removal is its own transaction,
// so the ones before it are already done, and the user is better served by
// as many as can go than by a selection cut at an arbitrary row. The first
// failure is what the user is told (the dashboard shows ?err=, index.go);
// the table they land on shows what is left.
func (h *Handler) deletePledges(ctx context.Context, resourceIDs []string, user *auth.User) error {
	if len(resourceIDs) == 0 {
		return errors.New("resource_id is required")
	}
	var first error
	failed := 0
	for _, id := range resourceIDs {
		if err := h.deletePledge(ctx, id, user); err != nil {
			failed++
			if first == nil {
				first = err
			}
		}
	}
	if first != nil && len(resourceIDs) > 1 {
		return errors.Wrapf(first, "failed to remove %d of %d pledges", failed, len(resourceIDs))
	}
	return first
}

// deletePledge contains the core business logic for pledge removal (Level 2: Business logic)
func (h *Handler) deletePledge(ctx context.Context, resourceID string, user *auth.User) error {
	// Validate resource_id
	if resourceID == "" {
		return errors.New("resource_id is required")
	}

	// Get vault resource
	resource, err := h.vault.GetResource(ctx, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to get vault resource")
	}

	// If resource doesn't exist, nothing to remove
	if resource == nil {
		return errors.New("resource not found")
	}

	// Get user's pledge for this resource
	pledge, err := h.vault.GetPledge(ctx, user, resource)
	if err != nil {
		return errors.Wrap(err, "failed to get user pledge")
	}

	// If pledge doesn't exist, nothing to remove
	if pledge == nil {
		return errors.New("pledge not found")
	}

	// Check if pledge is frozen
	isFrozen, err := h.vault.IsPledgeFrozen(ctx, pledge)
	if err != nil {
		return errors.Wrap(err, "failed to check pledge frozen status")
	}

	if isFrozen {
		return errors.New("pledge is frozen and cannot be removed")
	}

	// Remove the pledge
	err = h.vault.RemovePledge(ctx, pledge)
	if err != nil {
		return errors.Wrap(err, "failed to remove pledge")
	}

	return nil
}
