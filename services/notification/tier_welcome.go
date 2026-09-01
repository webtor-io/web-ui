package notification

import (
	"context"
	"strings"

	uuid "github.com/satori/go.uuid"
)

// Billing describes how this deployment takes money, as far as a welcome
// message needs to know: where the user manages (and cancels) the
// subscription, and whether it may have started as a free trial. A zero
// Billing means "no external billing provider is configured" and the message
// simply says nothing about payments — a self-hosted instance handing out
// tiers by its own means has no Patreon page to send anyone to.
type Billing struct {
	// ManageURL is the provider's subscription-management page.
	ManageURL string
	// TrialDays is the length of the provider's free trial, 0 when there is
	// none. Until the tier-change event carries whether THIS subscription
	// began as a trial, the message states the rule conditionally.
	TrialDays int
}

// TierWelcome is what the caller knows about the account at the moment it
// turned paid; it decides which "what you unlocked" lines are worth saying.
type TierWelcome struct {
	Tier string
	// ShowStremio: the account has not connected the Stremio addon yet.
	// Connecting it is the single strongest retention action we have
	// measured, so it goes first — and is omitted once done, because
	// telling someone to do what they already did reads as noise.
	ShowStremio bool
	// ShowVault: Vault is enabled here and the account has nothing in it.
	ShowVault bool
	Billing   Billing
}

// SendTierWelcome tells a freshly paid account what its tier unlocks and, when
// a billing provider is configured, where the subscription is managed.
//
// Why this exists: 39% of support mail in 2026-08 was "how do I cancel" — the
// charge shows up under the provider's name, not ours, and people who paid by
// card never connected the two. Saying it up front, in the first message
// after payment, is cheaper than answering each letter. The other half of the
// message is activation: paying accounts convert to Stremio worse than free
// ones because nothing points them there after the upgrade.
//
// One key per tier: the 24h dedupe window absorbs the double-fire when both
// the event consumer and a page request notice the same change, while a
// later, genuine second upgrade (bronze → gold months on) still gets its
// message.
func (s *Service) SendTierWelcome(to string, userID uuid.UUID, w TierWelcome) error {
	lang := s.store.AccountLang(context.Background(), userID)
	title := s.tierTitle(lang, w.Tier)
	opts := SendOptions{
		To:       to,
		UserID:   userID,
		Lang:     lang,
		Key:      "tier-welcome-" + w.Tier,
		Title:    s.T(lang, "email.tierWelcome.subject", "Tier", title),
		Template: "tier-welcome.html",
		Data: map[string]any{
			"Tier":        title,
			"ShowStremio": w.ShowStremio,
			"StremioURL":  s.domain + "/stremio/configure",
			"ShowVault":   w.ShowVault,
			"VaultURL":    s.domain + "/vault",
			"ManageURL":   w.Billing.ManageURL,
			"TrialDays":   w.Billing.TrialDays,
			"Domain":      s.domain,
		},
	}
	return s.Send(opts)
}

// tierTitle is the tier's marketing name in the user's language, falling back
// to the capitalised id (silver → Silver) for tiers the donate page has no
// copy for.
func (s *Service) tierTitle(lang, tier string) string {
	key := "donate.crypto.tier." + tier + ".title"
	if t := s.T(lang, key); t != "" && t != key {
		return t
	}
	if tier == "" {
		return ""
	}
	return strings.ToUpper(tier[:1]) + tier[1:]
}
