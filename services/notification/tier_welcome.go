package notification

import (
	"context"
	"strings"
	"time"

	uuid "github.com/satori/go.uuid"
)

// Billing describes how this deployment takes money, as far as a welcome
// message needs to know: where the user manages (and cancels) the
// subscription, and whether it may have started as a free trial. A zero
// Billing means "no external billing provider is configured" and the message
// simply says nothing about payments — a self-hosted instance handing out
// tiers by its own means has no Patreon page to send anyone to.
type Billing struct {
	// Provider is the name the charge appears under on the user's
	// statement — the one word support keeps having to supply.
	Provider string
	// ManageURL is the provider's subscription-management page.
	ManageURL string
	// CancelGuideURL is the provider's own step-by-step cancellation help
	// page — three support requests in August came back "I can't" after
	// being given ManageURL alone.
	CancelGuideURL string
	// TrialDays is the length of the provider's free trial, 0 when there is
	// none. Until the tier-change event carries whether THIS subscription
	// began as a trial, the message states the rule conditionally.
	TrialDays int
}

// TierWelcome is what the caller knows about the account at the moment it
// turned paid; it decides which "what you unlocked" lines are worth saying.
type TierWelcome struct {
	Tier string
	// BenefitKeys are the tier's benefit lines as i18n keys (the donate
	// page's card copy); empty for tiers the shop has no copy for.
	BenefitKeys []string
	// ShowStremio: the account has not connected the Stremio addon yet.
	// Connecting it is the single strongest retention action we have
	// measured, so it goes first — and is omitted once done, because
	// telling someone to do what they already did reads as noise.
	ShowStremio bool
	// ShowVault: Vault is enabled here and the account has nothing in it.
	ShowVault bool
	// ShowDiscover: the account has no watchlist yet. Release subscriptions
	// are always mentioned — they are made from Discover and nothing in the
	// progress query says whether the account has any.
	ShowDiscover bool
	Billing      Billing
	// IsFreeTrial and NextCharge come from the membership event when the
	// publisher knows them (nil otherwise): with both, the letter names the
	// day the trial converts or the next charge lands; without, it states
	// the trial rule conditionally.
	IsFreeTrial *bool
	NextCharge  *time.Time
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
	return s.Send(SendOptions{
		To:       to,
		UserID:   userID,
		Lang:     lang,
		Key:      "tier-welcome-" + w.Tier,
		Title:    s.tierWelcomeSubject(lang, w),
		Template: tierWelcomeTemplate,
		Data:     s.tierWelcomeData(lang, w),
	})
}

const tierWelcomeTemplate = "tier-welcome.html"

func (s *Service) tierWelcomeSubject(lang string, w TierWelcome) string {
	return s.T(lang, "email.tierWelcome.subject", "Tier", s.tierTitle(lang, w.Tier))
}

func (s *Service) tierWelcomeData(lang string, w TierWelcome) map[string]any {
	return map[string]any{
		"Tier":           s.tierTitle(lang, w.Tier),
		"BenefitKeys":    w.BenefitKeys,
		"SupportURL":     withUTM(s.domain+"/support", "tier-welcome"),
		"ShowStremio":    w.ShowStremio,
		"StremioURL":     withUTM(s.domain+"/stremio/configure", "tier-welcome"),
		"ShowVault":      w.ShowVault,
		"VaultURL":       withUTM(s.domain+"/vault", "tier-welcome"),
		"ShowDiscover":   w.ShowDiscover,
		"DiscoverURL":    withUTM(s.domain+"/discover", "tier-welcome"),
		"Provider":       w.Billing.Provider,
		"ManageURL":      w.Billing.ManageURL,
		"CancelGuideURL": w.Billing.CancelGuideURL,
		"TrialDays":      w.Billing.TrialDays,
		// TrialKnown: the event said whether this is a trial. Then the
		// letter states the fact (with the date when known) instead of the
		// conditional "if you started with a trial" sentence.
		"TrialKnown":     w.IsFreeTrial != nil,
		"IsTrial":        w.IsFreeTrial != nil && *w.IsFreeTrial,
		"NextChargeDate": chargeDate(w.NextCharge),
		"Domain":         s.domain,
	}
}

// chargeDate renders a charge date for the letter. ISO date on purpose: it is
// the one format every locale reads the same way, and a wrong guess at a
// locale's day/month order would be worse than plain.
func chargeDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

// PreviewTierWelcome renders the letter exactly as it would go on the wire
// (fragment wrapped in the mail layout) without sending or journaling it.
// For the dev-only preview route: a welcome fires once per account's life
// as a payer, so there is no other way to look at it before it reaches a
// real customer. Returns subject and HTML.
func (s *Service) PreviewTierWelcome(lang string, w TierWelcome) (string, string, error) {
	body, err := s.render(tierWelcomeTemplate, lang, s.tierWelcomeData(lang, w))
	if err != nil {
		return "", "", err
	}
	letter, err := s.wrapEmail(body, lang)
	if err != nil {
		return "", "", err
	}
	return s.tierWelcomeSubject(lang, w), letter, nil
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
