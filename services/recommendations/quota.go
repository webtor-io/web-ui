package recommendations

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	uuid "github.com/satori/go.uuid"
)

// RedisQuota is the production Quota implementation. It stores per-user
// counters in Redis keyed by UTC date, so quota automatically rolls over at
// midnight UTC without a scheduled cleanup job.
//
// Key layout: ai_rec:q:{userID}:{YYYY-MM-DD}
// Value:      integer — requests consumed today
// TTL:        seconds until end of current UTC day (set on first INCR)
//
// Atomicity is provided by a tiny Lua script — we cannot rely on
// "INCR; if == 1 then EXPIRE; if > limit then DECR" being executed as a
// single round trip, and a race would either mint free quota or over-charge
// users. The script runs server-side so there is no window.
type RedisQuota struct {
	cl     redis.UniversalClient
	cfg    Config
	script *redis.Script

	// now is overridable for deterministic tests; defaults to time.Now.
	now func() time.Time
}

// NewRedisQuota constructs a Quota backed by the given Redis client.
func NewRedisQuota(cl redis.UniversalClient, cfg Config) *RedisQuota {
	return &RedisQuota{
		cl:  cl,
		cfg: cfg,
		// KEYS[1] = counter key
		// ARGV[1] = limit, ARGV[2] = ttl seconds
		// returns remaining (>=0) on success, -1 if limit already reached
		script: redis.NewScript(`
local key   = KEYS[1]
local limit = tonumber(ARGV[1])
local ttl   = tonumber(ARGV[2])
local current = redis.call('INCR', key)
if current == 1 then
  redis.call('EXPIRE', key, ttl)
end
if current > limit then
  redis.call('DECR', key)
  return -1
end
return limit - current
`),
		now: time.Now,
	}
}

func (q *RedisQuota) limitFor(tier Tier) int {
	if tier == TierPaid {
		return q.cfg.PaidDailyQuota
	}
	return q.cfg.FreeDailyQuota
}

func (q *RedisQuota) key(userID uuid.UUID) string {
	return fmt.Sprintf("ai_rec:q:%s:%s", userID.String(), q.now().UTC().Format("2006-01-02"))
}

// nextUTCMidnight returns the upcoming midnight UTC after `now` — i.e. the
// instant the user's daily quota counter will roll over to a fresh zero.
// Centralised here so both the TTL math (secondsUntilEndOfUTCDay) and the
// public ResetAt() share one definition of "next reset".
func nextUTCMidnight(now time.Time) time.Time {
	t := now.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
}

// secondsUntilEndOfUTCDay returns the TTL we should attach to a freshly
// created counter so it expires exactly at midnight UTC. Minimum of 60s to
// guard against pathological clock drift landing an almost-zero TTL.
func secondsUntilEndOfUTCDay(now time.Time) int64 {
	ttl := int64(nextUTCMidnight(now).Sub(now.UTC()).Seconds())
	if ttl < 60 {
		return 60
	}
	return ttl
}

// ResetAt returns the upcoming moment the user's daily quota will roll
// over (next midnight UTC). Used by Service.QuotaResetAt to expose the
// reset point to the UI without coupling the frontend to "midnight UTC"
// semantics — if we ever change to rolling 24h windows, only this
// function needs to move.
func (q *RedisQuota) ResetAt() time.Time {
	return nextUTCMidnight(q.now())
}

// Consume increments the user's counter and returns the remaining allowance.
// If the user is already at their limit, returns ErrQuotaExceeded without
// mutating Redis state.
func (q *RedisQuota) Consume(ctx context.Context, userID uuid.UUID, tier Tier) (int, error) {
	limit := q.limitFor(tier)
	if limit <= 0 {
		return 0, ErrQuotaExceeded
	}
	ttl := secondsUntilEndOfUTCDay(q.now())
	res, err := q.script.Run(ctx, q.cl, []string{q.key(userID)}, limit, ttl).Int()
	if err != nil {
		return 0, errors.Wrap(err, "quota script failed")
	}
	if res < 0 {
		return 0, ErrQuotaExceeded
	}
	return res, nil
}

// Remaining reports how many requests the user has left today without
// mutating state. Used by read-only endpoints (chips GET) that must not
// themselves spend quota.
func (q *RedisQuota) Remaining(ctx context.Context, userID uuid.UUID, tier Tier) (int, error) {
	limit := q.limitFor(tier)
	if limit <= 0 {
		return 0, nil
	}
	v, err := q.cl.Get(ctx, q.key(userID)).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, errors.Wrap(err, "quota get failed")
	}
	remaining := limit - v
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}
