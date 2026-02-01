package event

import (
	"context"
	"encoding/json"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
)

type userUpdatedMsg struct {
	Email string `json:"email"`
}

func (h *Handler) userUpdated(msg []byte) error {
	var m userUpdatedMsg
	if err := json.Unmarshal(msg, &m); err != nil {
		return err
	}
	if m.Email == "" {
		return nil
	}

	ctx := context.Background()

	// 1. Get new claims by email
	cl, err := h.claims.Get(&claims.Request{Email: m.Email})
	if err != nil {
		return err
	}

	db := h.pg.Get()
	user, _, err := models.GetOrCreateUser(ctx, db, m.Email, nil)
	if err != nil {
		return err
	}

	// 2. UpdateUserTier
	if user.Tier != cl.Context.Tier.Name {
		user.Tier = cl.Context.Tier.Name
		if err := models.UpdateUserTier(ctx, db, user); err != nil {
			return err
		}
	}

	// 3. UpdateUserVPIfExists if Vault exists
	if h.vault != nil {
		authUser := &auth.User{
			ID:            user.UserID,
			Email:         user.Email,
			PatreonUserID: user.PatreonUserID,
		}
		if _, err := h.vault.UpdateUserVPIfExists(ctx, authUser); err != nil {
			return err
		}
	}

	log.WithField("email", m.Email).Info("user updated successfully")
	return nil
}
