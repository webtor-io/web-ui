package vault

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
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
		Pledges:               pledges,
		FreezePeriod:          h.vault.GetFreezePeriod(),
		ExpirePeriod:          h.vault.GetExpirePeriod(),
		TransferTimeoutPeriod: h.vault.GetTransferTimeoutPeriod(),
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
			log.WithError(err).WithField("pledgeID", pledge.PledgeID).Warn("failed to check pledge frozen status, skipping")
			continue
		}

		// Calculate ExpiresIn for unfunded pledges with expired resource
		var expiresIn time.Duration
		if !pledge.Funded && pledge.Resource != nil && pledge.Resource.ExpiredAt != nil {
			remaining := time.Until(pledge.Resource.ExpiredAt.Add(h.vault.GetExpirePeriod()))
			if remaining > 0 {
				expiresIn = remaining
			}
		}

		displayPledges = append(displayPledges, PledgeDisplay{
			PledgeID:   pledge.PledgeID.String(),
			ResourceID: pledge.ResourceID,
			Resource:   pledge.Resource,
			Amount:     pledge.Amount,
			IsFrozen:   isFrozen,
			Funded:     pledge.Funded,
			CreatedAt:  pledge.CreatedAt.Format("2006-01-02 15:04:05"),
			ExpiresIn:  expiresIn,
		})
	}

	return displayPledges, nil
}
