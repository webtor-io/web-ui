package support

import "testing"

func TestNeedsInfohash(t *testing.T) {
	for cause, want := range map[Cause]bool{-1: false, 0: true, 1: true, 2: true, 3: false} {
		if got := needsInfohash(cause); got != want {
			t.Errorf("cause %d: got %v, want %v", cause, got, want)
		}
	}
}
