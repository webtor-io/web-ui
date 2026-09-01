package event

import (
	"context"
	"encoding/json"

	"github.com/go-pg/pg/v10"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/notification"
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
	prevTier := user.Tier
	if user.Tier != cl.Context.Tier.Name {
		user.Tier = cl.Context.Tier.Name
		if err := models.UpdateUserTier(ctx, db, user); err != nil {
			return err
		}
	}

	// 2b. Welcome a freshly paid account. Done here, off the event, and not
	// from the page request that also syncs the tier (services/claims): the
	// request path is whatever the browser asks for first — a poster, an
	// async fragment — and has no business sending mail. The notification's
	// dedupe key makes a second trigger safe should one ever be added.
	// Failing to greet must not fail the tier sync: log and carry on.
	if h.ns != nil && welcomeNeeded(prevTier, user.Tier) {
		if err := h.sendTierWelcome(ctx, db, user); err != nil {
			log.WithError(err).WithField("email", m.Email).Warn("failed to send tier welcome")
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

// welcomeNeeded is the one decision worth pinning with a test: greet on the
// step from nothing/free to a paid tier, and only then. Paid-to-paid changes
// (an upgrade, or a downgrade that is still paid) are not a first welcome,
// and going free is the opposite of one.
func welcomeNeeded(prev, next string) bool {
	free := func(t string) bool { return t == "" || t == "free" }
	return free(prev) && !free(next)
}

func (h *Handler) sendTierWelcome(ctx context.Context, db *pg.DB, user *models.User) error {
	w := notification.TierWelcome{
		Tier:        user.Tier,
		ShowStremio: true,
		ShowVault:   h.vault != nil,
		Billing:     h.billing,
	}
	// Skip the lines about things the account has already done. The
	// onboarding progress query answers exactly these questions; a nil
	// result (unknown account) keeps the defaults above.
	if p, err := models.GetOnboardingProgress(ctx, db, user.UserID, h.vault != nil); err != nil {
		return err
	} else if p != nil {
		w.ShowStremio = !p.HasStremio
		w.ShowVault = h.vault != nil && !p.HasVault
	}
	to := notification.RecipientEmail(user.Email, user.NotificationEmail)
	return h.ns.SendTierWelcome(to, user.UserID, w)
}
