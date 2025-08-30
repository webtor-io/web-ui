package job

import (
	"context"
	"time"

	cs "github.com/webtor-io/common-services"
)

type State struct {
	ID  string
	TTL time.Duration
}

type Storage interface {
	Pub(ctx context.Context, queue string, id string, l LogItem) error
	Sub(ctx context.Context, queue string, id string) (res chan LogItem, err error)
	GetState(ctx context.Context, queue string, id string) (state *State, ok bool, err error)
	Drop(ctx context.Context, queue string, id string) (err error)
}

type NilStorage struct{}

func (s *NilStorage) Pub(_ context.Context, _ string, _ string, _ LogItem) error {
	return nil
}

func (s *NilStorage) Drop(_ context.Context, _ string, _ string) (err error) {
	return
}

func (s *NilStorage) Sub(_ context.Context, _ string, _ string) (res chan LogItem, err error) {
	return
}

func (s *NilStorage) GetState(_ context.Context, _ string, _ string) (state *State, ok bool, err error) {
	return nil, false, nil
}

var _ Storage = (*NilStorage)(nil)

func NewStorage(rc *cs.RedisClient, prefix string) Storage {
	cl := rc.Get()
	if cl == nil {
		return &NilStorage{}
	}
	return NewRedis(cl, prefix)
}
