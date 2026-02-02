package event

import (
	"context"
	"encoding/json"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

type resourceVaultedMsg struct {
	ResourceID string `json:"resource_id"`
}

func (h *Handler) resourceVaulted(msg []byte) error {
	var m resourceVaultedMsg
	if err := json.Unmarshal(msg, &m); err != nil {
		return err
	}
	if m.ResourceID == "" {
		return nil
	}

	ctx := context.Background()
	db := h.pg.Get()
	if err := vaultModels.UpdateResourceVaulted(ctx, db, m.ResourceID); err != nil {
		return err
	}
	log.WithField("resource_id", m.ResourceID).Info("resource vaulted status updated successfully")

	// Notify users
	r, err := vaultModels.GetResource(ctx, db, m.ResourceID)
	if err != nil {
		return err
	}
	if r == nil {
		log.WithField("resource_id", m.ResourceID).Warn("resource not found for notification")
		return nil
	}

	pledges, err := vaultModels.GetResourcePledges(ctx, db, m.ResourceID)
	if err != nil {
		return err
	}

	userIds := make(map[string]struct{})
	for _, p := range pledges {
		userIds[p.UserID.String()] = struct{}{}
	}

	for idStr := range userIds {
		u := &models.User{}
		err := db.Model(u).
			Context(ctx).
			Where("user_id = ?", idStr).
			Select()
		if err != nil {
			return err
		}
		if u.Email != "" {
			if err := h.ns.SendVaulted(u.Email, r); err != nil {
				return err
			}
		}
	}

	log.WithField("resource_id", m.ResourceID).Info("resource vaulted successfully")

	return nil
}
