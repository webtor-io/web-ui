package libapi

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/webtor-io/lazymap"
	"golang.org/x/time/rate"
)

// errUnexpectedScriptResult means the Lua returned something other than the
// {allowed, retry_ms} pair it is written to return — a mismatch between this
// file's two halves, not a runtime condition. Treated like any other redis
// failure: fall back to the local bucket rather than let it through.
var errUnexpectedScriptResult = errors.New("rate limit script returned an unexpected result")

const (
	apiRateLimitFlag = "api-rate-limit"
	apiRateBurstFlag = "api-rate-burst"
)

func RegisterRateLimitFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.Float64Flag{
			Name:   apiRateLimitFlag,
			Usage:  "sustained JSON API requests per second allowed per key (0 disables)",
			EnvVar: "API_RATE_LIMIT",
			Value:  10,
		},
		cli.IntFlag{
			Name:   apiRateBurstFlag,
			Usage:  "JSON API request burst allowed per key on top of the sustained rate",
			EnvVar: "API_RATE_BURST",
			Value:  50,
		},
	)
}

// takeScript is a token bucket in Lua so the whole read-modify-write is one
// atomic step: without that, two replicas serving the same key concurrently
// both read the same token count and both spend it.
//
// `now` is passed in rather than read from redis.call('TIME') because TIME
// makes a script non-deterministic, and this has to run on Dragonfly as well
// as Redis. The cost is clock skew between replicas, which NTP keeps to
// milliseconds and which can only make a bucket slightly generous or slightly
// strict -- never unbounded.
//
// Returns {allowed, retry_after_ms}. retry_after_ms is only meaningful when
// allowed is 0.
var takeScript = redis.NewScript(`
local key   = KEYS[1]
local rps   = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now   = tonumber(ARGV[3])
local ttl   = tonumber(ARGV[4])

local data   = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts     = tonumber(data[2])
if tokens == nil or ts == nil then
  tokens = burst
  ts = now
end

-- Refill for the time since the last take, capped at the burst size.
local elapsed = math.max(0, now - ts) / 1000.0
tokens = math.min(burst, tokens + elapsed * rps)

local allowed = 0
local retry = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry = math.ceil((1 - tokens) / rps * 1000)
end

redis.call('HSET', key, 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', key, ttl)
return {allowed, retry}
`)

// RateLimiter bounds requests per key. It limits *requests*, which tier claims
// do not: those limit traffic through the streaming chain, and a runaway
// integrator loop never touches that chain — it hammers this process and
// everything it proxies to.
//
// The bucket lives in redis, so all replicas share one budget. That matters
// beyond tidiness: the in-process version this replaced multiplied every limit
// by the replica count and reset it on each deploy, which is tolerable for an
// API quota and not tolerable for anything that sends mail.
//
// When redis is unreachable the limiter does not fail open. It falls back to a
// per-replica bucket — the old behaviour — so a redis blip degrades the limit
// to approximate instead of removing it. Failing closed was the alternative
// and it is worse: it would take the whole API down for the duration.
type RateLimiter struct {
	rps    float64
	burst  int
	prefix string
	redis  redis.UniversalClient
	// local is both the no-redis path and the fallback when redis errors.
	local *lazymap.LazyMap[*rate.Limiter]
}

// NewRateLimiter returns nil when the limit is disabled — callers treat a nil
// limiter as "no limiting", same convention as the vault service.
func NewRateLimiter(c *cli.Context) *RateLimiter {
	return NewRateLimiterWith(c.Float64(apiRateLimitFlag), c.Int(apiRateBurstFlag))
}

// NewRateLimiterWith builds a limiter from explicit numbers — the flag-free
// path, used directly by tests. Without a redis client attached it keeps its
// bucket in this process; see WithRedis.
func NewRateLimiterWith(rps float64, burst int) *RateLimiter {
	if rps <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	local := lazymap.New[*rate.Limiter](&lazymap.Config{
		// A bucket idle this long has long since refilled to full; recreating
		// it fresh is indistinguishable from having kept it.
		Expire: 10 * time.Minute,
	})
	return &RateLimiter{rps: rps, burst: burst, local: local}
}

// WithRedis moves the bucket into redis under the given key prefix, so every
// replica spends from one budget.
//
// The prefix is required rather than defaulted: several limiters coexist in
// this process (the JSON API, the login form, the notification-address
// verification) and they key on different things — an API key, a client IP, an
// account id. Sharing a namespace would let one drain another's budget.
//
// A nil client leaves the limiter on its in-process bucket, which is what
// makes this safe to call unconditionally at wiring time.
func (s *RateLimiter) WithRedis(cl redis.UniversalClient, prefix string) *RateLimiter {
	if s == nil || cl == nil {
		return s
	}
	s.redis = cl
	s.prefix = prefix
	return s
}

// Take spends one request from the key's bucket. When the bucket is empty it
// reports how long until a token refills — an upper bound, suitable for a
// Retry-After header — without spending anything.
func (s *RateLimiter) Take(key string) (retryAfter time.Duration, ok bool) {
	if s.redis != nil {
		if d, allowed, err := s.takeRedis(key); err == nil {
			return d, allowed
		} else {
			// Logged, not swallowed: a limiter quietly degrading to per-replica
			// budgets is exactly the kind of thing that is only noticed when
			// someone wonders why a limit is three times what it says.
			log.WithError(err).WithField("prefix", s.prefix).
				Warn("rate limiter fell back to this replica's own bucket")
		}
	}
	return s.takeLocal(key)
}

func (s *RateLimiter) takeRedis(key string) (time.Duration, bool, error) {
	// Long enough that a bucket cannot expire while still owing tokens: a full
	// refill takes burst/rps seconds. The floor keeps very fast limiters from
	// writing keys that vanish between two requests.
	ttl := time.Duration(float64(s.burst)/s.rps*float64(time.Second)) * 2
	if ttl < time.Minute {
		ttl = time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	res, err := takeScript.Run(ctx, s.redis, []string{s.prefix + ":" + key},
		s.rps, s.burst, time.Now().UnixMilli(), ttl.Milliseconds()).Int64Slice()
	if err != nil {
		return 0, false, err
	}
	if len(res) != 2 {
		return 0, false, errUnexpectedScriptResult
	}
	if res[0] == 1 {
		return 0, true, nil
	}
	return time.Duration(res[1]) * time.Millisecond, false, nil
}

func (s *RateLimiter) takeLocal(key string) (time.Duration, bool) {
	bucket, _ := s.local.Get(key, func() (*rate.Limiter, error) {
		return rate.NewLimiter(rate.Limit(s.rps), s.burst), nil
	})
	if bucket.Allow() {
		return 0, true
	}
	return time.Duration(float64(time.Second) / s.rps), false
}
