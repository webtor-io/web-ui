package libapi

import (
	"time"

	"github.com/urfave/cli"
	"github.com/webtor-io/lazymap"
	"golang.org/x/time/rate"
)

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

// RateLimiter bounds requests per API key. It limits *requests to the API*,
// which tier claims do not: those limit traffic through the streaming chain,
// and a runaway integrator loop never touches that chain — it hammers this
// process and everything it proxies to.
//
// One token bucket per key, in this replica's memory. With several replicas
// the effective limit is the configured one times the replica count; that is
// accepted — the limit exists to turn abuse into 429s, not to meter usage, and
// sharing buckets across replicas would put a network dependency in front of
// every request to pin down a number nobody is promised.
type RateLimiter struct {
	rps     float64
	burst   int
	buckets *lazymap.LazyMap[*rate.Limiter]
}

// NewRateLimiter returns nil when the limit is disabled — callers treat a nil
// limiter as "no limiting", same convention as the vault service.
func NewRateLimiter(c *cli.Context) *RateLimiter {
	return NewRateLimiterWith(c.Float64(apiRateLimitFlag), c.Int(apiRateBurstFlag))
}

// NewRateLimiterWith builds a limiter from explicit numbers — the flag-free
// path, used directly by tests.
func NewRateLimiterWith(rps float64, burst int) *RateLimiter {
	if rps <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	buckets := lazymap.New[*rate.Limiter](&lazymap.Config{
		// A bucket idle this long has long since refilled to full; recreating
		// it fresh is indistinguishable from having kept it.
		Expire: 10 * time.Minute,
	})
	return &RateLimiter{rps: rps, burst: burst, buckets: buckets}
}

// Take spends one request from the key's bucket. When the bucket is empty it
// reports how long until a token refills — an upper bound, suitable for a
// Retry-After header — without spending anything.
func (s *RateLimiter) Take(key string) (retryAfter time.Duration, ok bool) {
	bucket, _ := s.buckets.Get(key, func() (*rate.Limiter, error) {
		return rate.NewLimiter(rate.Limit(s.rps), s.burst), nil
	})
	if bucket.Allow() {
		return 0, true
	}
	return time.Duration(float64(time.Second) / s.rps), false
}
