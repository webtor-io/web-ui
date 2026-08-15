package release_subscription

import (
	"context"
	"sync"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/stremio"
)

// The production implementations of the poller's four dependencies. They
// hold no logic worth testing — each one is a call to something that already
// exists — which is exactly why they live apart from poller.go.

// PGPollStore is the poller's database.
type PGPollStore struct {
	db *pg.DB
}

func NewPGPollStore(db *pg.DB) *PGPollStore {
	return &PGPollStore{db: db}
}

func (s *PGPollStore) ListDue(ctx context.Context, now time.Time, limit int) ([]models.ReleaseSubscription, error) {
	return models.ListDueReleaseSubscriptions(ctx, s.db, now, limit)
}

func (s *PGPollStore) InsertHits(ctx context.Context, hits []models.ReleaseSubscriptionHit, baseline bool) (int, error) {
	return models.InsertReleaseSubscriptionHits(ctx, s.db, hits, baseline)
}

func (s *PGPollStore) ListPendingHits(ctx context.Context, subscriptionID uuid.UUID) ([]models.ReleaseSubscriptionHit, error) {
	return models.ListPendingReleaseSubscriptionHits(ctx, s.db, subscriptionID)
}

func (s *PGPollStore) MarkHitsNotified(ctx context.Context, subscriptionID uuid.UUID, infohashes []string) error {
	return models.MarkReleaseSubscriptionHitsNotified(ctx, s.db, subscriptionID, infohashes)
}

func (s *PGPollStore) MarkChecked(ctx context.Context, id uuid.UUID, state string, nextCheckAt time.Time) error {
	return models.MarkReleaseSubscriptionChecked(ctx, s.db, id, state, nextCheckAt)
}

func (s *PGPollStore) MarkNotified(ctx context.Context, id uuid.UUID) error {
	return models.MarkReleaseSubscriptionNotified(ctx, s.db, id)
}

func (s *PGPollStore) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	return models.MarkReleaseSubscriptionCompleted(ctx, s.db, id)
}

func (s *PGPollStore) SeasonEpisodes(ctx context.Context, videoID string, season int16) ([]models.EpisodeMetadata, error) {
	return models.ListEpisodeMetadataBySeason(ctx, s.db, videoID, season)
}

// AccountLang returns the language the account browses in, or "" when it has
// never been observed. Errors are swallowed: the caller has a fallback (the
// language the subscription was created in), and a lookup failure must not
// stop a letter.
func (s *PGPollStore) AccountLang(ctx context.Context, userID uuid.UUID) string {
	us, err := models.GetUserSettings(ctx, s.db, userID)
	if err != nil {
		log.WithError(err).WithField("user_id", userID).Warn("failed to read account language")
		return ""
	}
	return us.GetLang()
}

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
