package vault

import (
	"context"
	"testing"
	"time"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// FrozenFor is what is left of the freeze, 0 once it is over or when none
// is configured, and IsPledgeFrozen is FrozenFor > 0: a page that says
// "frozen" always has a time left to say, never "0 min".
func TestFrozenForIsTheFreezeLeft(t *testing.T) {
	s := &Vault{}
	for _, c := range []struct {
		name   string
		period time.Duration
		ago    time.Duration
		min    time.Duration
		max    time.Duration
	}{
		{"10 h 40 min in", 24 * time.Hour, 10*time.Hour + 40*time.Minute, 13*time.Hour + 19*time.Minute, 13*time.Hour + 20*time.Minute},
		{"just pledged", 24 * time.Hour, 0, 23*time.Hour + 59*time.Minute, 24 * time.Hour},
		{"over", 24 * time.Hour, 25 * time.Hour, 0, 0},
		{"no freeze", 0, 0, 0, 0},
	} {
		s.freezePeriod = c.period
		p := &vaultModels.Pledge{FrozenAt: time.Now().Add(-c.ago)}
		left := s.FrozenFor(p)
		if left < c.min || left > c.max {
			t.Errorf("%s: %v left, want %v..%v", c.name, left, c.min, c.max)
		}
		frozen, err := s.IsPledgeFrozen(context.Background(), p)
		if err != nil || frozen != (s.FrozenFor(p) > 0) {
			t.Errorf("%s: frozen %v (%v), but %v left", c.name, frozen, err, left)
		}
	}
}
