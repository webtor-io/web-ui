package release_subscription

// The outside world the poller talks to: the user's own stream sources, and
// the claims provider that decides how often we may ask them.

import (
	"context"
	"sync"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/stremio"
)

// BuilderSearch runs the user's own stream pipeline.
//
// The built service is cached for the length of a run: one account's
// subscriptions all query the same addons and indexers, and rebuilding the
// pipeline for each one would re-read the same two tables every time.
type BuilderSearch struct {
	b     *stremio.Builder
	mu    sync.Mutex
	cache map[uuid.UUID]stremio.StreamsService
}

func NewBuilderSearch(b *stremio.Builder) *BuilderSearch {
	return &BuilderSearch{b: b, cache: map[uuid.UUID]stremio.StreamsService{}}
}

// ErrNoSources means the account has no addons and no indexers to ask. The
// poller treats it like any other search failure — the query did not reach a
// source — which is what keeps an empty pass from being promoted to a
// baseline the user never actually took.
var ErrNoSources = errors.New("account has no stream sources")

func (s *BuilderSearch) Search(ctx context.Context, u *auth.User, contentType, contentID string) ([]stremio.StreamItem, error) {
	svc, err := s.serviceFor(ctx, u)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return nil, nil
	}
	resp, err := svc.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	// The composite swallows per-source failures by design — the
	// interactive list would rather show something than nothing. For the
	// poller that leniency is a trap: an empty answer from zero reachable
	// sources reads exactly like "nothing new", and the baseline decision
	// turns on the difference. The response's source accounting is what
	// tells the two apart.
	if resp.Sources == 0 {
		return nil, ErrNoSources
	}
	if resp.SourcesFailed >= resp.Sources {
		return nil, errors.New("every stream source failed")
	}
	return resp.Streams, nil
}

func (s *BuilderSearch) serviceFor(ctx context.Context, u *auth.User) (stremio.StreamsService, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, ok := s.cache[u.ID]; ok {
		return svc, nil
	}
	svc, err := s.b.BuildPollStreamsService(ctx, u)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build poll streams service")
	}
	s.cache[u.ID] = svc
	return svc, nil
}

// ClaimsTier answers the tier question through the claims provider.
type ClaimsTier struct {
	cl *claims.Claims
}

func NewClaimsTier(cl *claims.Claims) *ClaimsTier {
	return &ClaimsTier{cl: cl}
}

// IsFree treats every failure as free. The consequence of guessing wrong is
// only how often we poll, and the safe direction is less often: a paid user
// polled on the free cadence still gets their releases, a little later.
func (s *ClaimsTier) IsFree(email string, patreonUserID *string) bool {
	if s == nil || s.cl == nil {
		return false
	}
	d, err := s.cl.Get(&claims.Request{Email: email, PatreonUserID: patreonUserID})
	if err != nil || d == nil || d.Context == nil || d.Context.Tier == nil {
		return true
	}
	return d.Context.Tier.Id == 0
}
