package models

import "testing"

// Reachable through a client-supplied X-Layout body on any main-layout route,
// so a nil receiver must not take the request down after headers are written.
func TestLockedCountNilSafe(t *testing.T) {
	var c *OnboardingChecklist
	if got := c.LockedCount(); got != 0 {
		t.Fatalf("want 0 for a nil checklist, got %d", got)
	}
}

func TestLockedCountCountsOnlyLocked(t *testing.T) {
	c := &OnboardingChecklist{Steps: []OnboardingStep{
		{Done: true}, {}, {Locked: true}, {Locked: true},
	}}
	if got := c.LockedCount(); got != 2 {
		t.Fatalf("want 2, got %d", got)
	}
}
