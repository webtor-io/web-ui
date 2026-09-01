package event

import (
	"testing"
	"time"
)

// The welcome fires exactly once per account's life as a payer: on the step
// from free (or no tier yet) to paid. Everything else — paid↔paid moves, going
// free, staying put — is not a first welcome.
func TestWelcomeNeeded(t *testing.T) {
	cases := []struct {
		prev, next string
		want       bool
	}{
		{"free", "silver", true},
		{"", "bronze", true},
		{"free", "free", false},
		{"", "free", false},
		{"", "", false},
		{"silver", "gold", false},
		{"gold", "bronze", false},
		{"silver", "free", false},
		{"silver", "silver", false},
	}
	for _, c := range cases {
		if got := welcomeNeeded(c.prev, c.next); got != c.want {
			t.Errorf("welcomeNeeded(%q, %q) = %v, want %v", c.prev, c.next, got, c.want)
		}
	}
}

func TestParseChargeDate(t *testing.T) {
	if got := parseChargeDate("2026-09-09T00:00:00.000+00:00"); got == nil || !got.Equal(time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("patreon format: got %v", got)
	}
	if got := parseChargeDate(""); got != nil {
		t.Errorf("empty must be unknown, got %v", got)
	}
	if got := parseChargeDate("soon"); got != nil {
		t.Errorf("garbage must be unknown, got %v", got)
	}
}
