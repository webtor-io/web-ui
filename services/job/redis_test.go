package job

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// A purged run drops the stored log and publishes without a Sub, so its
// first push creates the list. That list must expire with the job like one
// subRaw creates; without a TTL it stayed in dragonfly-ui for good.
func TestPub_ListItCreatesExpiresWithTheJob(t *testing.T) {
	mr := miniredis.RunT(t)
	s := NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), "p")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := s.Drop(ctx, "q", "id"); err != nil {
		t.Fatal(err)
	}
	for _, l := range []LogItemLevel{Info, Done, Close} {
		if err := s.Pub(ctx, "q", "id", LogItem{Level: l}); err != nil {
			t.Fatal(err)
		}
	}
	if ttl := mr.TTL(s.makeKey("q", "id")); ttl <= 29*time.Minute || ttl > 30*time.Minute {
		t.Fatalf("list TTL = %v, want the job's 30 min", ttl)
	}
	st, ok, err := s.GetState(ctx, "q", "id")
	if err != nil || !ok || st.TTL <= 0 {
		t.Fatalf("GetState = %+v, %v, %v; want a state with its TTL", st, ok, err)
	}
}

// A list subRaw made keeps the TTL it set: a later push does not move it.
func TestPub_KeepsTheTTLOfAListSubMade(t *testing.T) {
	mr := miniredis.RunT(t)
	s := NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), "p")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	key := s.makeKey("q", "id")
	mr.Lpush(key, "")
	mr.SetTTL(key, 10*time.Minute)

	if err := s.Pub(ctx, "q", "id", LogItem{Level: Info}); err != nil {
		t.Fatal(err)
	}
	if ttl := mr.TTL(key); ttl != 10*time.Minute {
		t.Fatalf("list TTL = %v, want subRaw's 10m untouched", ttl)
	}
}
