package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// OnboardingStepKey identifies a checklist row.
type OnboardingStepKey string

// OnboardingStep is one checklist row, fully resolved so templates stay
// declarative. Translation keys live here rather than in markup, matching how
// tool titles, sort labels and job messages are handled elsewhere.
//
// It lives in models rather than services/onboarding because web.Context
// carries it for the navbar counter, and services/web must not import a
// service that imports it back — same reason UserSettings lives here.
type OnboardingStep struct {
	Key  OnboardingStepKey
	Done bool

	// Locked marks a step the current tier cannot complete. Locked steps still
	// render — greyed, badged PRO, pointing at /donate — because they are the
	// clearest statement of what a subscription buys. A step that is already
	// done is never locked: the user demonstrably had access.
	Locked bool

	TitleKey string
	DescKey  string
	CTAKey   string

	// Path is language-agnostic; templates wrap it in langPath.
	Path     string
	Fragment string

	// Async marks links safe for data-async-target navigation. Anything with a
	// Fragment is not: async nav swaps #main and never resolves the fragment,
	// so those must be full page loads.
	Async bool

	UmamiEvent string
}

// OnboardingChecklist is nil whenever there is nothing to show. Done and Total
// count only steps the user can act on, so the target is always reachable.
type OnboardingChecklist struct {
	Steps []OnboardingStep
	Done  int
	Total int
}

// LockedCount reports how many rows the current tier cannot complete. Used as
// an analytics dimension so free and paid impressions stay separable.
//
// Nil-safe on purpose: templates reach it through a client-supplied X-Layout
// body, so a crafted header can call it on a user with no checklist. A panic
// there escapes template.errRecover as a runtime error and lands after the
// headers are written.
func (c *OnboardingChecklist) LockedCount() int {
	if c == nil {
		return 0
	}
	n := 0
	for _, s := range c.Steps {
		if s.Locked {
			n++
		}
	}
	return n
}

// OnboardingProgress is the raw activation state of a single account, derived
// entirely from tables the product already writes to. There is deliberately no
// progress table: every step is detectable from its own feature's data, so the
// checklist cannot drift out of sync with reality and adds no user-keyed table
// to the GDPR export.
type OnboardingProgress struct {
	CreatedAt    time.Time `pg:"created_at"`
	HasLibrary   bool      `pg:"has_library"`
	HasWatchlist bool      `pg:"has_watchlist"`
	HasVault     bool      `pg:"has_vault"`
	HasStremio   bool      `pg:"has_stremio"`
}

// GetOnboardingProgress loads activation state in one round-trip. It runs on
// every home-page render for a signed-in user, so it stays a single query and
// four of the five EXISTS hit an index on user_id: access_token has UNIQUE
// (user_id, name), library is keyed by (user_id, resource_id) and both
// watchlists by (user_id, video_id).
//
// vault.pledge needed an index of its own: its only user-bearing index was
// UNIQUE (resource_id, user_id) (migration 33), leading column wrong, so the
// predicate could not seek. Added in migration 62.
//
// withVault mirrors whether the Vault service is wired up. Not because the
// schema could be missing — web-ui creates it itself (migration 24) — but
// because /vault is only routed when the service is configured, so the step
// would otherwise link to a 404.
//
// Returns (nil, nil) when the account row is gone, so callers can treat a
// missing user the same as "nothing to show".
func GetOnboardingProgress(ctx context.Context, db *pg.DB, userID uuid.UUID, withVault bool) (*OnboardingProgress, error) {
	vaultExpr := "false"
	if withVault {
		vaultExpr = `EXISTS (SELECT 1 FROM vault.pledge p WHERE p.user_id = u.user_id)`
	}

	p := &OnboardingProgress{}
	_, err := db.QueryOneContext(ctx, p, `
		SELECT
			u.created_at AS created_at,
			EXISTS (SELECT 1 FROM library l WHERE l.user_id = u.user_id) AS has_library,
			(
				EXISTS (SELECT 1 FROM movie_watchlist m WHERE m.user_id = u.user_id)
				OR EXISTS (SELECT 1 FROM series_watchlist s WHERE s.user_id = u.user_id)
			) AS has_watchlist,
			`+vaultExpr+` AS has_vault,
			EXISTS (
				SELECT 1 FROM access_token t
				WHERE t.user_id = u.user_id AND t.name = 'stremio'
			) AS has_stremio
		FROM "user" u
		WHERE u.user_id = ?
	`, userID)
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to load onboarding progress")
	}
	return p, nil
}
