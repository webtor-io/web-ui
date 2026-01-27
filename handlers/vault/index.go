package vault

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// index handles HTTP request for displaying user's pledges list (Level 1: HTTP interaction)
func (h *Handler) index(c *gin.Context) {
	user := auth.GetUserFromContext(c)

	pledges, err := h.getPledgesList(c.Request.Context(), user.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	data := &PledgeListData{
		Pledges: pledges,
	}

	h.tb.Build("vault/pledge/index").HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

// getPledgesList contains the core business logic for fetching user's pledges (Level 2: Business logic)
func (h *Handler) getPledgesList(ctx context.Context, userID uuid.UUID) ([]PledgeDisplay, error) {
	db := h.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}

	pledges, err := vaultModels.GetUserPledgesWithResources(ctx, db, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user pledges")
	}

	// Convert pledges to display format with frozen status
	displayPledges := make([]PledgeDisplay, 0, len(pledges))
	for _, pledge := range pledges {
		// Check if pledge is frozen using IsPledgeFrozen method
		isFrozen, err := h.vault.IsPledgeFrozen(ctx, &pledge)
		if err != nil {
			// If error checking frozen status, skip this pledge
			continue
		}

		displayPledges = append(displayPledges, PledgeDisplay{
			PledgeID:   pledge.PledgeID.String(),
			ResourceID: pledge.ResourceID,
			Resource:   pledge.Resource,
			Amount:     pledge.Amount,
			IsFrozen:   isFrozen,
			CreatedAt:  pledge.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	return displayPledges, nil
}
