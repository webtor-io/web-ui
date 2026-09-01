// Package onboarding builds the activation checklist shown to freshly
// registered users on the home page.
//
// Why a checklist and not a post-signup wizard: 88% of /login hits carry a
// return-url (63% of them from the Discover auth gate), so the overwhelming
// majority of registrations happen mid-task. An interstitial would interrupt
// the very thing the user signed up to do; a passive card on the home page
// costs them nothing and is still there when they come back.
//
// Every registered user gets the card, but two of the five steps are unusable
// on the free tier and render locked. Vault shows an upsell instead of
// contents, and while a free user can install the Stremio addon, webtor itself
// is not offered as a streaming backend — link_resolver.CheckAvailability
// returns nothing for unpaid users when requiresPayment is set, which is how
// the addon calls it. See docs/onboarding.md.
package onboarding

import (
	"context"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
)

// ActivationWindow bounds how long the checklist follows a new account.
//
// Retention is decided early — users still inactive after two weeks are
// already lost by every cohort we have. Showing the card forever would nag the
// wrong people, so it expires instead of asking the user to dismiss it. That is
// also what keeps this feature free of a progress table, a dismiss flag and a
// GDPR export entry: nothing about the checklist is persisted.
const ActivationWindow = 14 * 24 * time.Hour

// The checklist types live in models so services/web can carry them on
// Context for the navbar counter without importing this package back.
type (
	StepKey   = models.OnboardingStepKey
	Step      = models.OnboardingStep
	Checklist = models.OnboardingChecklist
)

const (
	StepAccount  StepKey = "account"
	StepLibrary  StepKey = "library"
	StepDiscover StepKey = "discover"
	StepVault    StepKey = "vault"
	StepStremio  StepKey = "stremio"
)

// Deliberately uncached. The obvious optimisation — a short lazymap TTL, as
// user_settings does — makes the checklist lie: the user completes a step and
// the card, the counter and the modal keep showing it outstanding until the
// entry expires. A checklist that does not tick when you tick it is worse than
// an extra query.
//
// The query is cheap to skip rather than cheap to cache: accounts past the
// activation window never reach here at all (the middleware checks age from
// the session's user row), so this runs only for the narrow cohort the card is
// aimed at.
type Service struct {
	pg           *cs.PG
	vaultEnabled bool
	// trialAvailable: some tier is fronted by a free trial (see
	// donate.TrialAvailable) — locked rows then invite the user to try the
	// feature instead of asking them to buy it sight unseen.
	trialAvailable bool
}

func New(pg *cs.PG, vaultEnabled, trialAvailable bool) *Service {
	return &Service{pg: pg, vaultEnabled: vaultEnabled, trialAvailable: trialAvailable}
}

// Get returns the checklist for a signed-in user, or nil when it should not be
// rendered: no DB, unknown account, account older than ActivationWindow, or
// every step the user can actually complete is already done.
//
// paid does not decide visibility — every registered user gets the checklist.
// It decides which steps are actionable and which render locked.
func (s *Service) Get(ctx context.Context, userID uuid.UUID, paid bool, now time.Time) (*Checklist, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, nil
	}
	p, err := models.GetOnboardingProgress(ctx, db, userID, s.vaultEnabled)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get onboarding progress")
	}
	if p == nil {
		return nil, nil
	}
	return build(p, s.vaultEnabled, paid, s.trialAvailable, now), nil
}

// Preview renders the checklist exactly as a freshly registered account of the
// given tier sees it: pristine progress, no database, no activation-window
// concern. It exists for the dev-only `?onboarding=free|paid` override — the
// real resolver is useless for review on a developer's own account, which is
// both past the activation window and has every step done, so the card would
// be nil long before the tier mattered.
func (s *Service) Preview(paid bool, now time.Time) *Checklist {
	return build(&models.OnboardingProgress{CreatedAt: now}, s.vaultEnabled, paid, s.trialAvailable, now)
}

// paidOnlySteps are the steps a free account cannot complete. Vault shows an
// upsell instead of contents, and while a free user can install the Stremio
// addon, webtor itself is not offered as a streaming backend —
// link_resolver.CheckAvailability returns nothing for unpaid users when
// requiresPayment is set, which is how the addon calls it.
var paidOnlySteps = map[StepKey]bool{
	StepVault:   true,
	StepStremio: true,
}

// build is the whole decision, kept pure so it can be tested without a DB.
func build(p *models.OnboardingProgress, vaultEnabled, paid, trialAvailable bool, now time.Time) *Checklist {
	if now.Sub(p.CreatedAt) >= ActivationWindow {
		return nil
	}

	steps := []Step{
		{
			// Always done: the goal-gradient effect — a list that is already
			// underway gets finished more often than one starting at zero.
			// It is the only step without a destination; there is nothing
			// left to do about it.
			Key:      StepAccount,
			Done:     true,
			TitleKey: "onboarding.account.title",
			DescKey:  "onboarding.account.desc",
		},
		{
			Key:      StepLibrary,
			Done:     p.HasLibrary,
			TitleKey: "onboarding.library.title",
			DescKey:  "onboarding.library.desc",
			CTAKey:   "onboarding.library.cta",
			// "/lib/", not "/library" — the route group is /lib and the index
			// is registered on "/", so the trailing slash matters. Matches the
			// navbar link.
			Path:       "/lib/",
			Async:      true,
			UmamiEvent: "onboarding-library",
		},
		{
			Key:        StepDiscover,
			Done:       p.HasWatchlist,
			TitleKey:   "onboarding.discover.title",
			DescKey:    "onboarding.discover.desc",
			CTAKey:     "onboarding.discover.cta",
			Path:       "/discover",
			Async:      true,
			UmamiEvent: "onboarding-discover",
		},
	}

	// /vault is only routed when the Vault service is configured, so without
	// it this step would link to a 404. (The schema itself always exists —
	// web-ui creates it in migration 24.)
	if vaultEnabled {
		steps = append(steps, Step{
			Key:        StepVault,
			Done:       p.HasVault,
			TitleKey:   "onboarding.vault.title",
			DescKey:    "onboarding.vault.desc",
			CTAKey:     "onboarding.vault.cta",
			Path:       "/vault",
			Async:      true,
			UmamiEvent: "onboarding-vault",
		})
	}

	steps = append(steps, Step{
		Key:      StepStremio,
		Done:     p.HasStremio,
		TitleKey: "onboarding.stremio.title",
		DescKey:  "onboarding.stremio.desc",
		CTAKey:   "onboarding.stremio.cta",
		Path:     "/profile",
		Fragment: "#stremio",
		// Fragment-bearing, so no async navigation.
		UmamiEvent: "onboarding-stremio",
	})

	// A step already done is never locked — the user demonstrably had access,
	// and greying out something they have finished would read as a bug.
	done := 0
	outstanding := 0
	for i := range steps {
		if steps[i].Done {
			done++
			continue
		}
		if !paid && paidOnlySteps[steps[i].Key] {
			// A locked step points at /donate instead of its own section:
			// following it to a page that would only show another upsell is a
			// detour. The link is deliberately quiet in the template — the
			// checklist is an onboarding aid first and an upsell second.
			// Its own analytics event keeps upsell clicks separable from
			// genuine step completions.
			steps[i].Locked = true
			steps[i].Path = "/donate"
			steps[i].Fragment = ""
			steps[i].Async = true
			// The row keeps its PRO badge either way; only the invitation
			// changes. When a trial is configured the CTA leads with it —
			// "try it" outsells "buy it" for a feature the user has never
			// touched, and the 2026-09 cohort measure showed trial-first
			// payers retain at least as well as direct ones.
			steps[i].CTAKey = "onboarding.proCta"
			if trialAvailable {
				steps[i].CTAKey = "onboarding.trialCta"
			}
			steps[i].UmamiEvent = "onboarding-pro-" + string(steps[i].Key)
			continue
		}
		outstanding++
	}

	// Hide once nothing actionable is left. Locked steps are deliberately not
	// counted here: keeping the card alive on steps the user cannot complete
	// would turn a checklist into a permanent advert.
	if outstanding == 0 {
		return nil
	}

	// Total counts only what this user can actually complete. Including locked
	// steps would give a free account a target it can never reach — the counter
	// would stall at 2/5 and then the card would vanish, never showing done.
	return &Checklist{
		Steps: steps,
		Done:  done,
		Total: done + outstanding,
	}
}
