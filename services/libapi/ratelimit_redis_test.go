package libapi

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	s := miniredis.RunT(t)
	cl := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = cl.Close() })
	return cl
}

// The whole reason this limiter moved into redis: two replicas must spend from
// one budget. The in-process version multiplied every limit by the replica
// count, which is tolerable for an API quota and not tolerable for anything
// that sends mail on request.
//
// Two RateLimiter values over one redis stand in for two replicas — they share
// nothing in this process, so anything they agree on came through redis.
func TestRedisBucketIsSharedBetweenReplicas(t *testing.T) {
	cl := newTestRedis(t)
	const burst = 3
	replicaA := NewRateLimiterWith(0.01, burst).WithRedis(cl, "rl:test")
	replicaB := NewRateLimiterWith(0.01, burst).WithRedis(cl, "rl:test")

	// Spend the whole burst on A.
	for i := 0; i < burst; i++ {
		if _, ok := replicaA.Take("same-key"); !ok {
			t.Fatalf("replica A was refused on take %d of a burst of %d", i+1, burst)
		}
	}
	// B must see an empty bucket. In-process, it would see a full one.
	if retry, ok := replicaB.Take("same-key"); ok {
		t.Error("replica B was allowed after replica A spent the shared burst; the budget is per-replica, not shared")
	} else if retry <= 0 {
		t.Errorf("refusal reported retry-after %v; a caller cannot tell when to come back", retry)
	}
}

// Different keys are different budgets — otherwise one busy API key would lock
// out every other caller.
func TestRedisBucketIsPerKey(t *testing.T) {
	cl := newTestRedis(t)
	l := NewRateLimiterWith(0.01, 1).WithRedis(cl, "rl:test")

	if _, ok := l.Take("key-one"); !ok {
		t.Fatal("first take on key-one was refused")
	}
	if _, ok := l.Take("key-one"); ok {
		t.Error("key-one was allowed twice with a burst of one")
	}
	if _, ok := l.Take("key-two"); !ok {
		t.Error("key-two was refused; budgets are not per key")
	}
}

// Prefixes exist so the several limiters in this process cannot drain each
// other: they key on different things (an API key, a client IP, an account id)
// and a collision would be invisible.
func TestRedisPrefixSeparatesLimiters(t *testing.T) {
	cl := newTestRedis(t)
	api := NewRateLimiterWith(0.01, 1).WithRedis(cl, "rl:api")
	mail := NewRateLimiterWith(0.01, 1).WithRedis(cl, "rl:email")

	if _, ok := api.Take("same-id"); !ok {
		t.Fatal("api limiter refused its first take")
	}
	if _, ok := mail.Take("same-id"); !ok {
		t.Error("the mail limiter was refused after the api limiter spent its own budget for the same id")
	}
}

// A redis outage must not remove the limit. It degrades to the per-replica
// bucket -- the old behaviour -- rather than failing open, and equally not
// failing closed, which would take the API down for the duration of a blip.
func TestFallsBackToLocalBucketWhenRedisIsDown(t *testing.T) {
	s := miniredis.RunT(t)
	cl := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = cl.Close() })

	l := NewRateLimiterWith(0.01, 1).WithRedis(cl, "rl:test")
	s.Close() // redis is gone from here on

	if _, ok := l.Take("k"); !ok {
		t.Fatal("first take was refused with redis down; the limiter failed closed instead of falling back")
	}
	if _, ok := l.Take("k"); ok {
		t.Error("second take was allowed with redis down; the limiter failed open and stopped limiting")
	}
}

// Without a client attached the limiter keeps its bucket in this process, which
// is what makes WithRedis safe to call unconditionally at wiring time and keeps
// every existing caller working.
func TestWithoutRedisStaysLocal(t *testing.T) {
	l := NewRateLimiterWith(0.01, 1)
	if l.WithRedis(nil, "rl:test") != l {
		t.Error("WithRedis(nil) replaced the limiter instead of leaving it alone")
	}
	if _, ok := l.Take("k"); !ok {
		t.Fatal("first take refused")
	}
	if _, ok := l.Take("k"); ok {
		t.Error("second take allowed with a burst of one")
	}
}

// Tokens come back over time, at the configured rate -- a limiter that only
// ever subtracts would lock a key out permanently after one burst.
func TestRedisBucketRefills(t *testing.T) {
	cl := newTestRedis(t)
	// 100/s so a refill is observable without making the test slow.
	l := NewRateLimiterWith(100, 1).WithRedis(cl, "rl:test")

	if _, ok := l.Take("k"); !ok {
		t.Fatal("first take refused")
	}
	if _, ok := l.Take("k"); ok {
		t.Fatal("second take allowed immediately with a burst of one")
	}
	time.Sleep(50 * time.Millisecond) // 5 tokens' worth at 100/s
	if _, ok := l.Take("k"); !ok {
		t.Error("still refused after enough time for several tokens; the bucket does not refill")
	}
}
