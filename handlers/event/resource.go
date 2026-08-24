package event

import (
	"context"
	"encoding/json"

	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/notification"
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

	users := make([]models.User, 0, len(userIds))
	for idStr := range userIds {
		u := &models.User{}
		err := db.Model(u).
			Context(ctx).
			Where("user_id = ?", idStr).
			Select()
		if err != nil {
			return err
		}
		users = append(users, *u)
	}

	if err := notifyVaulted(h.ns, users, r); err != nil {
		return err
	}

	log.WithField("resource_id", m.ResourceID).Info("resource vaulted successfully")

	return nil
}

// vaultedNotifier is the notification service seen through the single call
// this handler makes. It is an interface for the reason vault.go's reaper
// uses them: the fan-out below is worth testing, and a database, a NATS
// connection and an SMTP server are not needed to test it.
type vaultedNotifier interface {
	SendVaulted(to string, userID uuid.UUID, r *vaultModels.Resource) error
}

// notifyVaulted tells every user who pledged the resource that it is now in
// the vault.
//
// There is deliberately no notification.Deliverable guard around the call.
// SendVaulted goes through notification.Service.Send, which writes the feed
// entry unconditionally and decides about mail on its own -- it fills the
// To column only when the address is deliverable, and returns without
// touching SMTP when it is not. Skipping the call for an undeliverable
// address (the self-hosted admin's "admin" sentinel, say) would therefore
// skip the feed entry too, and the feed entry IS the notification for
// exactly the users who have no mailbox. Do not "restore" the check.
func notifyVaulted(ns vaultedNotifier, users []models.User, r *vaultModels.Resource) error {
	for i := range users {
		u := &users[i]
		addr := notification.RecipientEmail(u.Email, u.NotificationEmail)
		if err := ns.SendVaulted(addr, u.UserID, r); err != nil {
			return err
		}
	}
	return nil
}
