package onboarding

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/models"
)

const (
	withVault = true
	paidUser  = true
	freeUser  = false
	trialOn   = true
	noTrial   = false
)

func progress(age time.Duration, library, watchlist, vault, stremio bool) (*models.OnboardingProgress, time.Time) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	return &models.OnboardingProgress{
		CreatedAt:    now.Add(-age),
		HasLibrary:   library,
		HasWatchlist: watchlist,
		HasVault:     vault,
		HasStremio:   stremio,
	}, now
}

func TestHiddenWhenEverythingDone(t *testing.T) {
	p, now := progress(time.Hour, true, true, true, true)
	if c := build(p, withVault, paidUser, trialOn, now); c != nil {
		t.Fatalf("expected no checklist for a fully activated user, got %+v", c)
	}
}

func TestHiddenAfterActivationWindow(t *testing.T) {
	p, now := progress(ActivationWindow, false, false, false, false)
	if c := build(p, withVault, paidUser, trialOn, now); c != nil {
		t.Fatal("expected no checklist once the account is older than the activation window")
	}
}

func TestVisibleJustInsideActivationWindow(t *testing.T) {
	p, now := progress(ActivationWindow-time.Minute, false, false, false, false)
	if c := build(p, withVault, paidUser, trialOn, now); c == nil {
		t.Fatal("expected a checklist a minute before the window closes")
	}
}

func TestFreshAccountStartsWithAccountStepDone(t *testing.T) {
	p, now := progress(time.Minute, false, false, false, false)
	c := build(p, withVault, paidUser, trialOn, now)
	if c == nil {
		t.Fatal("expected a checklist for a brand new account")
	}
	// The account step is always done, so a brand new account starts at 1/5
	// rather than zero — a list already underway gets finished more often.
	if c.Done != 1 || c.Total != 5 {
		t.Fatalf("expected 1 of 5 done, got %d of %d", c.Done, c.Total)
	}
	if !c.Steps[0].Done {
		t.Error("the account step must always count as done")
	}
	want := []StepKey{StepAccount, StepLibrary, StepDiscover, StepVault, StepStremio}
	for i, k := range want {
		if c.Steps[i].Key != k {
			t.Errorf("step %d: want %q, got %q", i, k, c.Steps[i].Key)
		}
	}
}

// Every step carries the same three parts, so none of them may ship with an
// empty title, description or link — a blank row would look like a bug.
func TestEveryStepIsFullyPopulated(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	for _, s := range build(p, withVault, paidUser, trialOn, now).Steps {
		if s.TitleKey == "" || s.DescKey == "" {
			t.Errorf("step %q is missing a translation key: %+v", s.Key, s)
		}
		// The account step is the one without a destination: it is already
		// done and there is nothing to send anyone to. Every other step must
		// be actionable and measurable.
		if s.Key == StepAccount {
			if s.Path != "" || s.CTAKey != "" {
				t.Errorf("the account step must not link anywhere: %+v", s)
			}
			continue
		}
		if s.Path == "" || s.CTAKey == "" {
			t.Errorf("step %q has no destination", s.Key)
		}
		if s.UmamiEvent == "" {
			t.Errorf("step %q has no analytics event", s.Key)
		}
	}
}

// Each step must lead somewhere different — a checklist that sends two rows to
// the same screen is asking for the same action twice.
func TestStepDestinationsAreDistinct(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	seen := map[string]StepKey{}
	for _, s := range build(p, withVault, paidUser, trialOn, now).Steps {
		if s.Path == "" {
			continue
		}
		dest := s.Path + s.Fragment
		if prev, ok := seen[dest]; ok {
			t.Errorf("steps %q and %q both point at %s", prev, s.Key, dest)
		}
		seen[dest] = s.Key
	}
}

func TestPartialProgressIsCounted(t *testing.T) {
	p, now := progress(time.Hour, true, false, true, false)
	c := build(p, withVault, paidUser, trialOn, now)
	if c == nil {
		t.Fatal("expected a checklist while two steps are outstanding")
	}
	// account + library + vault
	if c.Done != 3 {
		t.Fatalf("expected 3 done, got %d", c.Done)
	}
	if !c.Steps[1].Done || c.Steps[2].Done || !c.Steps[3].Done || c.Steps[4].Done {
		t.Error("done flags do not match the progress passed in")
	}
}

// Without the Vault service /vault is not routed, so the step must disappear
// rather than link to a 404.
func TestVaultStepOmittedWhenVaultDisabled(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	c := build(p, false, paidUser, trialOn, now)
	if c == nil {
		t.Fatal("expected a checklist")
	}
	if c.Total != 4 {
		t.Fatalf("expected 4 steps without vault, got %d", c.Total)
	}
	for _, s := range c.Steps {
		if s.Key == StepVault {
			t.Error("vault step must not render when the vault service is absent")
		}
	}
}

// With vault disabled the remaining three steps still complete the checklist.
func TestHiddenWhenAllRemainingStepsDoneWithoutVault(t *testing.T) {
	p, now := progress(time.Hour, true, true, false, true)
	if c := build(p, false, paidUser, trialOn, now); c != nil {
		t.Fatal("expected no checklist when every rendered step is done")
	}
}

// Fragment links cannot go through async navigation: it swaps #main and never
// resolves the fragment, dropping the user at the top of the profile page.
func TestFragmentStepsAreNotAsync(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	for _, s := range build(p, withVault, paidUser, trialOn, now).Steps {
		if s.Fragment != "" && s.Async {
			t.Errorf("step %q has a fragment and must not be async", s.Key)
		}
	}
}

// A free account still gets the checklist — the two paid steps render locked
// rather than disappearing, because that is the clearest statement of what a
// subscription buys.
func TestFreeUserSeesLockedPaidSteps(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	c := build(p, withVault, freeUser, trialOn, now)
	if c == nil {
		t.Fatal("expected a checklist for a free account")
	}
	if len(c.Steps) != 5 {
		t.Fatalf("expected all 5 rows to render, got %d", len(c.Steps))
	}
	// The counter covers only what a free account can complete, so it can
	// actually be finished instead of stalling at 2/5.
	if c.Total != 3 || c.Done != 1 {
		t.Fatalf("expected 1/3 for a fresh free account, got %d/%d", c.Done, c.Total)
	}
	locked := map[StepKey]bool{}
	for _, s := range c.Steps {
		if s.Locked {
			locked[s.Key] = true
			// Locked rows send the user to the plans page, not to a section
			// that would only show them another upsell.
			if s.Path != "/donate" || s.Fragment != "" {
				t.Errorf("locked step %q must point at /donate: %+v", s.Key, s)
			}
			// Upsell clicks must stay separable from real step completions.
			if s.UmamiEvent != "onboarding-pro-"+string(s.Key) {
				t.Errorf("locked step %q has the wrong analytics event: %q", s.Key, s.UmamiEvent)
			}
		}
	}
	if !locked[StepVault] || !locked[StepStremio] {
		t.Errorf("vault and stremio must be locked on the free tier, locked=%v", locked)
	}
	if len(locked) != 2 {
		t.Errorf("only the paid steps may be locked, got %v", locked)
	}
}

func TestPaidUserHasNothingLocked(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	for _, s := range build(p, withVault, paidUser, trialOn, now).Steps {
		if s.Locked {
			t.Errorf("step %q must not be locked for a paying user", s.Key)
		}
	}
}

// A free user who connected Stremio while paying keeps the step shown as done.
// Greying out something already finished would read as a bug.
func TestDoneStepIsNeverLocked(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, true)
	for _, s := range build(p, withVault, freeUser, trialOn, now).Steps {
		if s.Key == StepStremio {
			if !s.Done {
				t.Error("stremio should be done")
			}
			if s.Locked {
				t.Error("a completed step must never render locked")
			}
		}
	}
}

// Locked steps are not counted as outstanding work: once a free user has done
// everything available to them the card disappears instead of nagging forever
// about steps they cannot complete.
func TestFreeUserChecklistHidesWhenActionableStepsDone(t *testing.T) {
	p, now := progress(time.Hour, true, true, false, false)
	if c := build(p, withVault, freeUser, trialOn, now); c != nil {
		t.Fatalf("expected no checklist once every actionable step is done, got %+v", c)
	}
	// The same state still shows for a paying user — they have real work left.
	if c := build(p, withVault, paidUser, trialOn, now); c == nil {
		t.Fatal("a paying user still has the vault and stremio steps outstanding")
	}
}

// Every destination must be a route that actually exists. The navbar is the
// canonical list of top-level section links, so cross-checking against it
// catches typos that a hand-written expected-value list would only repeat —
// this is how "/library" (the real route is "/lib/") shipped and 404'd.
//
// If a future step points somewhere the navbar does not link, add it to
// exempt below deliberately rather than deleting the check.
func TestStepPathsMatchNavbarLinks(t *testing.T) {
	nav, err := os.ReadFile("../../templates/partials/nav.html")
	if err != nil {
		t.Fatalf("cannot read navbar: %v", err)
	}
	exempt := map[string]bool{}

	p, now := progress(time.Hour, false, false, false, false)
	for _, paid := range []bool{true, false} {
		for _, s := range build(p, withVault, paid, trialOn, now).Steps {
			if s.Path == "" || exempt[s.Path] {
				continue
			}
			want := `langPath $.Lang "` + s.Path + `"`
			if !strings.Contains(string(nav), want) {
				t.Errorf("step %q points at %q, which the navbar does not link — is it a real route?", s.Key, s.Path)
			}
		}
	}
}

// The counter must never present a target the user cannot reach: locked steps
// are excluded from the denominator, so a free account can finish at 3/3 while
// still seeing the two PRO rows.
func TestCounterExcludesLockedSteps(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	p := &models.OnboardingProgress{CreatedAt: now.Add(-time.Hour), HasLibrary: true}

	free := build(p, withVault, freeUser, trialOn, now)
	if free.Done != 2 || free.Total != 3 {
		t.Errorf("free: want 2/3, got %d/%d", free.Done, free.Total)
	}
	paid := build(p, withVault, paidUser, trialOn, now)
	if paid.Done != 2 || paid.Total != 5 {
		t.Errorf("paid: want 2/5, got %d/%d", paid.Done, paid.Total)
	}
}

// Locked rows keep the PRO badge either way (the template keys off Locked);
// what changes with trial availability is only the invitation on the button.
func TestLockedStepsInviteToTrialWhenAvailable(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	for _, s := range build(p, withVault, freeUser, trialOn, now).Steps {
		if !s.Locked {
			continue
		}
		if s.CTAKey != "onboarding.trialCta" {
			t.Errorf("step %s: expected trial CTA, got %s", s.Key, s.CTAKey)
		}
		if s.Path != "/donate" {
			t.Errorf("step %s: trial CTA must still lead to /donate, got %s", s.Key, s.Path)
		}
	}
}

func TestLockedStepsFallBackToProCtaWithoutTrial(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	locked := 0
	for _, s := range build(p, withVault, freeUser, noTrial, now).Steps {
		if !s.Locked {
			continue
		}
		locked++
		if s.CTAKey != "onboarding.proCta" {
			t.Errorf("step %s: expected plans CTA without a trial, got %s", s.Key, s.CTAKey)
		}
	}
	if locked != 2 {
		t.Errorf("expected 2 locked steps, got %d", locked)
	}
}

// Trial availability is an upsell concern; for paying users it must change
// nothing at all.
func TestTrialAvailabilityDoesNotAffectPaidUsers(t *testing.T) {
	p, now := progress(time.Hour, false, false, false, false)
	for _, s := range build(p, withVault, paidUser, trialOn, now).Steps {
		if s.Locked {
			t.Errorf("step %s: paid user must not see locked steps", s.Key)
		}
		if s.CTAKey == "onboarding.trialCta" || s.CTAKey == "onboarding.proCta" {
			t.Errorf("step %s: paid user must keep the step's own CTA, got %s", s.Key, s.CTAKey)
		}
	}
}

// Preview is the dev-only `?onboarding=` review mode: a pristine account of the
// requested tier, past no gates. Free must show the locked rows (that is what
// the mode exists to review); paid must show every step actionable.
func TestPreviewRendersFreshAccountOfRequestedTier(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	svc := &Service{vaultEnabled: true, trialAvailable: true}

	free := svc.Preview(false, now)
	if free == nil {
		t.Fatal("free preview must render")
	}
	locked := 0
	for _, s := range free.Steps {
		if s.Locked {
			locked++
			if s.CTAKey != "onboarding.trialCta" {
				t.Errorf("step %s: free preview with a trial must invite to it, got %s", s.Key, s.CTAKey)
			}
		}
	}
	if locked != 2 {
		t.Errorf("free preview: expected 2 locked steps, got %d", locked)
	}

	paid := svc.Preview(true, now)
	if paid == nil {
		t.Fatal("paid preview must render")
	}
	for _, s := range paid.Steps {
		if s.Locked {
			t.Errorf("step %s: paid preview must not lock steps", s.Key)
		}
	}
	if paid.Done != 1 {
		t.Errorf("fresh account starts at 1 done (account), got %d", paid.Done)
	}
}
