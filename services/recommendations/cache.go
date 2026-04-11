package recommendations

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
)

// ChipsCache stores per-user chip lists across all web-ui pods. It is
// intentionally a distributed store, not an in-process lazymap — web-ui runs
// multiple replicas behind a load balancer, and a lazymap-backed cache on pod
// A would be invisible to pod B. A free-tier user has just 1 daily quota
// unit; burning it twice because load balancing re-routes a retry to a cold
// pod is unacceptable.
//
// The cache is advisory: a Get returning (nil, nil) means "cache miss, feel
// free to compute and Set", and Set failures are logged but do not fail the
// caller — a degraded cache is still better than a degraded feature.
type ChipsCache interface {
	// Get returns the cached ChipsResponse for the given key, or (nil, nil)
	// on a miss. A non-nil error indicates a transport-level problem.
	Get(ctx context.Context, key string) (*ChipsResponse, error)
	// Set stores the ChipsResponse under the given key with the given TTL.
	Set(ctx context.Context, key string, val *ChipsResponse, ttl time.Duration) error
	// Del removes the entry. Used for force-refresh.
	Del(ctx context.Context, key string) error
}

// --- Redis implementation ---

// RedisChipsCache is the production ChipsCache. Values are JSON-encoded so
// they remain human-readable in `redis-cli GET` (helpful for debugging) and
// survive a schema change on the struct as long as the new fields are
// additive.
type RedisChipsCache struct {
	cl     redis.UniversalClient
	prefix string
}

// NewRedisChipsCache wires a ChipsCache over go-redis. The prefix is fixed
// to "ai_rec:chips:" so keys are grouped in the Redis keyspace browser.
func NewRedisChipsCache(cl redis.UniversalClient) *RedisChipsCache {
	return &RedisChipsCache{cl: cl, prefix: "ai_rec:chips:"}
}

func (c *RedisChipsCache) key(k string) string { return c.prefix + k }

func (c *RedisChipsCache) Get(ctx context.Context, key string) (*ChipsResponse, error) {
	raw, err := c.cl.Get(ctx, c.key(key)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "chips cache get")
	}
	var v ChipsResponse
	if err := json.Unmarshal(raw, &v); err != nil {
		// Corrupt entry — pretend it's a miss so the caller regenerates.
		return nil, nil
	}
	return &v, nil
}

func (c *RedisChipsCache) Set(ctx context.Context, key string, val *ChipsResponse, ttl time.Duration) error {
	if val == nil {
		return nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return errors.Wrap(err, "chips cache marshal")
	}
	if err := c.cl.Set(ctx, c.key(key), raw, ttl).Err(); err != nil {
		return errors.Wrap(err, "chips cache set")
	}
	return nil
}

func (c *RedisChipsCache) Del(ctx context.Context, key string) error {
	if err := c.cl.Del(ctx, c.key(key)).Err(); err != nil {
		return errors.Wrap(err, "chips cache del")
	}
	return nil
}

