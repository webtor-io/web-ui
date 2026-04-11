package recommendations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	uuid "github.com/satori/go.uuid"
)

func newTestQuota(t *testing.T, cfg Config) (*RedisQuota, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	cl := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	q := NewRedisQuota(cl, cfg)
	// Anchor "now" to a stable moment mid-day so TTL math is predictable.
	anchor := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return anchor }
	return q, mr
}

func TestRedisQuota_Free_AllowsOne(t *testing.T) {
	q, _ := newTestQuota(t, Config{FreeDailyQuota: 1, PaidDailyQuota: 100})
	uid := uuid.NewV4()

	remaining, err := q.Consume(context.Background(), uid, TierFree)
	if err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected 0 remaining after first consume, got %d", remaining)
	}

	_, err = q.Consume(context.Background(), uid, TierFree)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded on second consume, got %v", err)
	}
}

func TestRedisQuota_Paid_Allows100(t *testing.T) {
	q, _ := newTestQuota(t, Config{FreeDailyQuota: 1, PaidDailyQuota: 100})
	uid := uuid.NewV4()

	for i := 0; i < 100; i++ {
		remaining, err := q.Consume(context.Background(), uid, TierPaid)
		if err != nil {
			t.Fatalf("consume #%d: %v", i+1, err)
		}
		if remaining != 100-i-1 {
			t.Fatalf("consume #%d: want remaining %d, got %d", i+1, 100-i-1, remaining)
		}
	}

	_, err := q.Consume(context.Background(), uid, TierPaid)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded on 101st consume, got %v", err)
	}
}

func TestRedisQuota_Remaining_DoesNotMutate(t *testing.T) {
	q, _ := newTestQuota(t, Config{FreeDailyQuota: 5, PaidDailyQuota: 50})
	uid := uuid.NewV4()

	r, err := q.Remaining(context.Background(), uid, TierFree)
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if r != 5 {
		t.Fatalf("want 5, got %d", r)
	}

	if _, err := q.Consume(context.Background(), uid, TierFree); err != nil {
		t.Fatalf("consume: %v", err)
	}

	r, err = q.Remaining(context.Background(), uid, TierFree)
	if err != nil {
		t.Fatalf("remaining after consume: %v", err)
	}
	if r != 4 {
		t.Fatalf("want 4, got %d", r)
	}

	// Calling Remaining twice must not decrement further.
	r2, _ := q.Remaining(context.Background(), uid, TierFree)
	if r2 != 4 {
		t.Fatalf("remaining is not idempotent: %d", r2)
	}
}

func TestRedisQuota_PerUserIsolation(t *testing.T) {
	q, _ := newTestQuota(t, Config{FreeDailyQuota: 1})
	a := uuid.NewV4()
	b := uuid.NewV4()

	if _, err := q.Consume(context.Background(), a, TierFree); err != nil {
		t.Fatalf("a: %v", err)
	}
	if _, err := q.Consume(context.Background(), b, TierFree); err != nil {
		t.Fatalf("b: %v", err)
	}
}

func TestRedisQuota_DailyReset(t *testing.T) {
	q, _ := newTestQuota(t, Config{FreeDailyQuota: 1})
	uid := uuid.NewV4()

	if _, err := q.Consume(context.Background(), uid, TierFree); err != nil {
		t.Fatalf("day1 first: %v", err)
	}
	if _, err := q.Consume(context.Background(), uid, TierFree); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("day1 second should be blocked: %v", err)
	}

	// Advance clock to the next day — key name changes, counter resets.
	q.now = func() time.Time { return time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC) }

	if _, err := q.Consume(context.Background(), uid, TierFree); err != nil {
		t.Fatalf("day2 first should succeed: %v", err)
	}
}

func TestSecondsUntilEndOfUTCDay(t *testing.T) {
	t.Run("midday", func(t *testing.T) {
		got := secondsUntilEndOfUTCDay(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC))
		if got != 12*3600 {
			t.Fatalf("want 12h, got %ds", got)
		}
	})
	t.Run("one second before midnight", func(t *testing.T) {
		got := secondsUntilEndOfUTCDay(time.Date(2026, 4, 10, 23, 59, 59, 0, time.UTC))
		if got != 60 { // clamped to minimum
			t.Fatalf("want floor 60, got %ds", got)
		}
	})
	t.Run("midnight start of day", func(t *testing.T) {
		got := secondsUntilEndOfUTCDay(time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC))
		if got != 24*3600 {
			t.Fatalf("want 24h, got %ds", got)
		}
	})
}

func TestRedisQuota_ZeroLimitBlocks(t *testing.T) {
	q, _ := newTestQuota(t, Config{FreeDailyQuota: 0})
	uid := uuid.NewV4()
	_, err := q.Consume(context.Background(), uid, TierFree)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("zero limit must block, got %v", err)
	}
}
