package libapi

import (
	"testing"
	"time"
)

func TestRateLimiterTake(t *testing.T) {
	// rps 1 with burst 3: the refill is slow enough that the whole test runs
	// inside one token, so the counts below are deterministic.
	s := NewRateLimiterWith(1, 3)

	for i := 0; i < 3; i++ {
		if _, ok := s.Take("key-a"); !ok {
			t.Fatalf("request %d within burst was denied", i+1)
		}
	}
	retryAfter, ok := s.Take("key-a")
	if ok {
		t.Fatal("request past the burst was allowed")
	}
	if retryAfter <= 0 || retryAfter > time.Second {
		t.Fatalf("retryAfter = %v, want within (0s, 1s]", retryAfter)
	}

	// Another key must have its own bucket — one abusive integration must not
	// starve the rest.
	if _, ok := s.Take("key-b"); !ok {
		t.Fatal("a fresh key was denied because another key spent its bucket")
	}
}

func TestRateLimiterDisabled(t *testing.T) {
	if s := NewRateLimiterWith(0, 50); s != nil {
		t.Fatal("rps 0 must disable limiting entirely, not limit to zero")
	}
}
