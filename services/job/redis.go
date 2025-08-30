package job

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct {
	cl     redis.UniversalClient
	prefix string
}

func (s *Redis) Drop(ctx context.Context, queue, id string) (err error) {
	key := s.makeKey(queue, id)
	cmd := s.cl.Del(ctx, key)
	if cmd.Err() != nil {
		return cmd.Err()
	}
	return nil
}

func NewRedis(cl redis.UniversalClient, prefix string) *Redis {
	return &Redis{
		prefix: prefix,
		cl:     cl,
	}
}

func (s *Redis) Pub(ctx context.Context, queue string, id string, l LogItem) (err error) {
	key := s.makeKey(queue, id)
	j, err := json.Marshal(l)
	if err != nil {
		return err
	}

	cmd := s.cl.RPush(ctx, key, j)
	if cmd.Err() != nil {
		return cmd.Err()
	}
	cmd = s.cl.Publish(ctx, key, string(j))
	if cmd.Err() != nil {
		return cmd.Err()
	}
	return
}

func (s *Redis) GetState(ctx context.Context, queue string, id string) (state *State, ok bool, err error) {
	key := s.makeKey(queue, id)
	ttlCmd := s.cl.TTL(ctx, key)
	if ttlCmd.Err() != nil {
		return nil, false, ttlCmd.Err()
	}
	var dur time.Duration
	val := ttlCmd.Val()
	if val == -2 {
		return
	}
	if val > 0 {
		dur = val
	}
	return &State{
		ID:  id,
		TTL: dur,
	}, true, nil
}

func (s *Redis) Sub(ctx context.Context, queue string, id string) (res chan LogItem, err error) {
	cctx, cancel := context.WithCancel(ctx)
	ch, err := s.subRaw(cctx, queue, id)
	if err != nil || ch == nil {
		cancel()
		return
	}
	res = make(chan LogItem)
	go func() {
		for {
			select {
			case <-cctx.Done():
				close(res)
				return
			case i := <-ch:
				var li LogItem
				err = json.Unmarshal([]byte(i), &li)
				if err != nil {
					res <- LogItem{
						Level:   Error,
						Message: err.Error(),
					}
					cancel()
					close(res)
					return
				}
				res <- li
				if li.Level == Close {
					cancel()
					close(res)
					return
				}
			}

		}
	}()
	return
}
func (s *Redis) subRaw(ctx context.Context, queue string, id string) (res chan string, err error) {
	key := s.makeKey(queue, id)
	exCmd := s.cl.Exists(ctx, key)
	ex, err := exCmd.Result()
	if err != nil {
		return
	}
	if ex == 0 {
		if deadline, ok := ctx.Deadline(); ok {
			pCmd := s.cl.LPush(ctx, key, "")
			if err = pCmd.Err(); err != nil {
				return
			}
			eCmd := s.cl.ExpireAt(ctx, key, deadline)
			if err = eCmd.Err(); err != nil {
				return
			}
		}
		return
	}
	cmd := s.cl.LRange(ctx, key, 0, -1)
	items, err := cmd.Result()
	if err != nil {
		return
	}
	res = make(chan string)
	go func() {
		for _, i := range items {
			if i == "" {
				continue
			}
			res <- i
		}
		if ctx.Err() != nil {
			return
		}
		ps := s.cl.Subscribe(ctx, key)
		defer func(ps *redis.PubSub) {
			_ = ps.Close()
		}(ps)

		if err = ps.Ping(ctx); err != nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				close(res)
				return
			case msg := <-ps.Channel():
				res <- msg.Payload
			}
		}
	}()
	return
}

func (s *Redis) makeKey(queue, id string) string {
	hash := md5.Sum([]byte(fmt.Sprintf("%s-%s-%s", s.prefix, queue, id)))
	return hex.EncodeToString(hash[:])
}

var _ Storage = (*Redis)(nil)
