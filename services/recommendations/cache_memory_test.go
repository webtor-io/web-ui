package recommendations

import (
	"context"
	"sync"
	"time"
)

// memoryChipsCache is a trivial map-backed ChipsCache used by unit tests.
// It does NOT implement TTL expiry — tests that need that behaviour should
// call Del explicitly. Lives in a _test.go file so it never compiles into
// the production binary; nothing in production should reach for it.
type memoryChipsCache struct {
	mu sync.Mutex
	m  map[string]*ChipsResponse
}

func newMemoryChipsCache() *memoryChipsCache {
	return &memoryChipsCache{m: map[string]*ChipsResponse{}}
}

func (c *memoryChipsCache) Get(_ context.Context, key string) (*ChipsResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (c *memoryChipsCache) Set(_ context.Context, key string, val *ChipsResponse, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = val
	return nil
}

func (c *memoryChipsCache) Del(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
	return nil
}
