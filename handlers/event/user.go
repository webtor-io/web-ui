package event

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/handlers/donate"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/offer"
)

// userUpdatedMsg is the webhook service's user.updated event. Email is the
// contract every version has had; the rest arrived in 2026-09 so the welcome
// letter can name the trial and the charge date instead of hedging, and so a
// failed first charge can be told from a failed renewal. All optional — an
// older publisher sends only the email.
type userUpdatedMsg struct {
	Email            string `json:"email"`
	Source           string `json:"source"`
	Event            string `json:"event"`
	PatronStatus     string `json:"patron_status"`
	IsFreeTrial      *bool  `json:"is_free_trial"`
	NextChargeDate   string `json:"next_charge_date"`
	LastChargeStatus string `json:"last_charge_status"`
	// LifetimeSupportCents is what the member has ever paid; nil when the
	// publisher did not say, which is never read as "paid nothing".
	LifetimeSupportCents *int `json:"lifetime_support_cents"`
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
		if err := h.sendTierWelcome(ctx, db, user, m); err != nil {
			log.WithError(err).WithField("email", m.Email).Warn("failed to send tier welcome")
		}
	}

	// 2c. A membership that ended without a single payment: offer the way
	// back. Same rule as the welcome — failing to write must not fail the
	// tier sync.
	if h.ns != nil {
		if reason := winbackReason(m, time.Now(), h.winbackTrialEndedFrom); reason != 0 {
			if err := h.sendWinBack(user, prevTier, reason); err != nil {
				log.WithError(err).WithField("email", m.Email).Warn("failed to send winback letter")
			}
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
	return freeTier(prev) && !freeTier(next)
}

func freeTier(t string) bool { return t == "" || t == "free" }

// winbackReason says whether this event ends a membership that never paid
// a cent, and how it ended; 0 when it does not. Only the end counts: the
// provider's discount codes are for people without a paid membership, and a
// declined card keeps the membership in the provider's retry state for about
// a month (median 31 days, trials of 2026-07-20..08-31).
//
//   - Trial cancelled: the trial's members:delete, which lands exactly when
//     the trial runs out (677 of 681 cancelled trials ended this way). Sent
//     only from trialEndedFrom on — zero keeps this reason off.
//   - Card declined until the provider gave up: former_patron with the last
//     charge Declined. About a quarter of such ends carry no charge status
//     and are missed rather than confused with a cancelled trial.
//
// Lifetime support must be a known 0: a publisher that does not say is
// never read as "paid nothing", and anyone who ever paid is not a new
// member the code could be for.
func winbackReason(m userUpdatedMsg, now, trialEndedFrom time.Time) notification.WinBackReason {
	if m.LifetimeSupportCents == nil || *m.LifetimeSupportCents != 0 {
		return 0
	}
	switch {
	case m.Event == "members:delete" && m.IsFreeTrial != nil && *m.IsFreeTrial:
		if trialEndedFrom.IsZero() || now.Before(trialEndedFrom) {
			return 0
		}
		return notification.WinBackTrialEnded
	case m.PatronStatus == "former_patron" && m.LastChargeStatus == "Declined":
		return notification.WinBackPaymentFailed
	}
	return 0
}

// sendWinBack writes the letter unless there is no live code or the account
// is in the control group (winbackHeldOut).
func (h *Handler) sendWinBack(user *models.User, prevTier string, reason notification.WinBackReason) error {
	w := winbackLetter(h.offers, prevTier, reason, time.Now())
	if w == nil {
		log.WithField("user_id", user.UserID).Info("winback letter not sent: no live discount code")
		return nil
	}
	if winbackHeldOut(user.UserID, h.winbackHoldout) {
		log.WithField("user_id", user.UserID).WithField("reason", reason).Info("winback letter held out: control group")
		return nil
	}
	to := notification.RecipientEmail(user.Email, user.NotificationEmail)
	return h.ns.SendWinBack(to, user.UserID, *w)
}

// winbackLetter assembles the letter from the catalog, nil when there is no
// live code. The tier is named only for a cancelled trial — the event
// arrives while the account still holds the trial's tier; a declined card
// dropped it weeks earlier. The cap is the free tier's, which the account is
// back on; the button goes to the full-price checkout of the plan the app
// sells (the promo plan: Silver monthly today), where the code is typed in.
func winbackLetter(offers *offer.Service, prevTier string, reason notification.WinBackReason, now time.Time) *notification.WinBack {
	d := offers.Discount(now)
	if d == nil {
		return nil
	}
	w := &notification.WinBack{Reason: reason, Discount: *d}
	if reason == notification.WinBackTrialEnded && !freeTier(prevTier) {
		w.Tier = prevTier
	}
	if t := offers.Catalog().TierNamed("free"); t != nil && t.DownloadRate != nil {
		w.CapMbps = *t.DownloadRate
	}
	if promo := offers.Promo(); promo != nil {
		w.CheckoutURL = offers.DiscountCheckout(promo.Tier, d)
	}
	return w
}

// winbackHeldOut puts an account in the control group of the winback
// letter, percent out of 100. The bucket is the first 28 bits
// of md5(user_id) mod 100, so the split is stable across pods, restarts and
// Patreon's retries of the same card, and can be recomputed in SQL when the
// effect is measured:
//
//	('x' || substr(md5(user_id::text), 1, 7))::bit(28)::int % 100 < percent
func winbackHeldOut(userID uuid.UUID, percent int) bool {
	sum := md5.Sum([]byte(userID.String()))
	v, err := strconv.ParseUint(hex.EncodeToString(sum[:])[:7], 16, 32)
	if err != nil {
		return false
	}
	return int(v%100) < percent
}

func (h *Handler) sendTierWelcome(ctx context.Context, db *pg.DB, user *models.User, m userUpdatedMsg) error {
	w := notification.TierWelcome{
		Tier:         user.Tier,
		Benefits:     donate.TierBenefits(user.Tier, h.offers.Catalog().TierNamed(user.Tier)),
		ShowStremio:  true,
		ShowVault:    h.vault != nil,
		ShowDiscover: true,
		Billing:      h.billing,
		IsFreeTrial:  m.IsFreeTrial,
		NextCharge:   parseChargeDate(m.NextChargeDate),
	}
	if w.Billing.Provider != "" {
		w.Billing.TrialDays = h.offers.TrialDays(user.Tier)
	}
	// Skip the lines about things the account has already done. The
	// onboarding progress query answers exactly these questions; a nil
	// result (unknown account) keeps the defaults above.
	if p, err := models.GetOnboardingProgress(ctx, db, user.UserID, h.vault != nil); err != nil {
		return err
	} else if p != nil {
		w.ShowStremio = !p.HasStremio
		w.ShowVault = h.vault != nil && !p.HasVault
		w.ShowDiscover = !p.HasWatchlist
	}
	to := notification.RecipientEmail(user.Email, user.NotificationEmail)
	return h.ns.SendTierWelcome(to, user.UserID, w)
}

// parseChargeDate reads Patreon's next_charge_date. Patreon sends RFC 3339
// with milliseconds ("2026-09-09T00:00:00.000+00:00"); anything unparseable
// is treated as unknown rather than failing the welcome.
func parseChargeDate(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t
		}
	}
	return nil
}
