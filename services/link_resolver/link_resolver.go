package link_resolver

import (
	"context"
	"net/http"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	ci "github.com/webtor-io/web-ui/services/cache_index"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/link_resolver/backends"
	co "github.com/webtor-io/web-ui/services/link_resolver/common"
)

// LinkResolver resolves streaming links across multiple backends (RealDebrid, Torbox, Webtor)
// by checking content availability and generating direct download URLs
type LinkResolver struct {
	pg                   *cs.PG
	cacheIndex           *ci.CacheIndex
	userBackends         map[models.StreamingBackendType]co.Backend
	webtorBackend        *backends.Webtor
	enabledBackendsCache lazymap.LazyMap[[]*models.StreamingBackend]
}

// New creates a new LinkResolver with configured backends
func New(cl *http.Client, pg *cs.PG, apiService *api.Api, cacheIndex *ci.CacheIndex) *LinkResolver {
	return &LinkResolver{
		pg:         pg,
		cacheIndex: cacheIndex,
		userBackends: map[models.StreamingBackendType]co.Backend{
			models.StreamingBackendTypeRealDebrid: backends.NewRealDebrid(cl),
			models.StreamingBackendTypeTorbox:     backends.NewTorbox(cl),
		},
		webtorBackend: backends.NewWebtor(apiService),
		enabledBackendsCache: lazymap.New[[]*models.StreamingBackend](&lazymap.Config{
			Expire:      1 * time.Minute,
			ErrorExpire: 30 * time.Second,
		}),
	}
}

func (s *LinkResolver) GetUserEnabledBackends(ctx context.Context, userID uuid.UUID) ([]*models.StreamingBackend, error) {
	return s.enabledBackendsCache.Get(userID.String(), func() ([]*models.StreamingBackend, error) {
		return s.getUserEnabledBackends(ctx, userID)
	})
}

func (s *LinkResolver) getUserEnabledBackends(ctx context.Context, userID uuid.UUID) ([]*models.StreamingBackend, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	// Get user's streaming backends ordered by priority (highest first)
	userBackends, err := models.GetUserStreamingBackends(ctx, db, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user streaming backends")
	}

	// Filter to only enabled backends
	enabledBackends := make([]*models.StreamingBackend, 0)
	for _, backend := range userBackends {
		if _, ok := s.userBackends[backend.Type]; !ok {
			log.WithField("backend_type", backend.Type).Warn("backend implementation not found")
			continue
		}
		if backend.Enabled {
			enabledBackends = append(enabledBackends, backend)
		}
	}
	log.WithField("enabled_backends_count", len(enabledBackends)).Debug("found enabled streaming backends")
	return enabledBackends, nil
}

// ResolveLink resolves a streaming link for the given content
// It first checks availability, then generates a direct download URL from the appropriate backend
// Returns nil if content is not available or user doesn't have access
func (s *LinkResolver) ResolveLink(ctx context.Context, userID uuid.UUID, apiClaims *api.Claims, userClaims *claims.Data, hash, path string, requiresPayment bool) (*co.LinkResult, error) {
	var (
		err             error
		enabledBackends []*models.StreamingBackend
		url             string
		cached          bool
	)
	log.WithFields(log.Fields{
		"hash":             hash,
		"path":             path,
		"requires_payment": requiresPayment,
	}).Debug("resolving link")
	enabledBackends, err = s.GetUserEnabledBackends(ctx, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load user enabled backends")
	}
	for _, userBackend := range enabledBackends {
		backend, ok := s.userBackends[userBackend.Type]
		if !ok {
			log.WithField("backend_type", userBackend.Type).Warn("backend implementation not found")
			continue
		}
		url, cached, err = backend.ResolveLink(ctx, userBackend.AccessToken, hash, path)
		if err != nil {
			log.WithError(err).WithField("backend_type", userBackend.Type).Warn("failed to generate link from backend")
			continue
		}
		if !cached {
			log.WithField("backend_type", userBackend.Type).Warn("link is not cached")
			continue
		}

		// Mark as cached in cache index
		err = s.cacheIndex.MarkAsCached(ctx, userBackend.Type, path, hash)
		if err != nil {
			return nil, errors.Wrap(err, "failed to mark as cached in cache index")
		}

		log.WithFields(log.Fields{
			"url":          url,
			"cached":       cached,
			"backend_type": userBackend.Type,
		}).Info("generated streaming link from backend")
		return &co.LinkResult{
			URL:         url,
			ServiceType: userBackend.Type,
			Cached:      cached,
		}, nil
	}

	// Fallback to webtor if no debrid service worked
	// Webtor is always available as fallback (if payment required, check if user is paid)
	if requiresPayment && !s.isPaidUser(userClaims) {
		return nil, nil
	}
	url, cached, err = s.webtorBackend.ResolveLink(ctx, apiClaims, hash, path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate webtor link")
	}

	// Mark as cached in cache index if cached
	if cached {
		err = s.cacheIndex.MarkAsCached(ctx, models.StreamingBackendTypeWebtor, path, hash)
		if err != nil {
			return nil, errors.Wrap(err, "failed to mark as cached in cache index")
		}
	}

	log.WithFields(log.Fields{
		"url":          url,
		"cached":       cached,
		"backend_type": "webtor",
	}).Info("generated webtor link")
	return &co.LinkResult{
		URL:         url,
		ServiceType: models.StreamingBackendTypeWebtor,
		Cached:      cached,
	}, nil
}

// isPaidUser checks if the user has paid tier
func (s *LinkResolver) isPaidUser(userClaims *claims.Data) bool {
	if userClaims == nil || userClaims.Context == nil || userClaims.Context.Tier == nil {
		return true
	}
	return userClaims.Context.Tier.Id > 0
}

func (s *LinkResolver) CheckAvailability(ctx context.Context, id uuid.UUID, cla *claims.Data, hash string, p string, requiresPayment bool) (*co.CheckAvailabilityResult, error) {
	r, err := s.cacheIndex.IsCached(ctx, hash, p)
	if err != nil {
		return nil, err
	}
	eb, err := s.GetUserEnabledBackends(ctx, id)
	if err != nil {
		return nil, err
	}
	var (
		cached bool
		bt     models.StreamingBackendType
	)
	for _, userBackend := range eb {
		for _, cir := range r {
			if cir.BackendType == userBackend.Type {
				cached = true
				bt = cir.BackendType
				break
			}
		}
	}
	if cached {
		return &co.CheckAvailabilityResult{
			Cached:      true,
			ServiceType: bt,
		}, nil
	}
	if requiresPayment && !s.isPaidUser(cla) {
		return nil, nil
	}
	for _, cir := range r {
		if cir.BackendType == models.StreamingBackendTypeWebtor {
			cached = true
			break
		}
	}
	return &co.CheckAvailabilityResult{
		Cached:      cached,
		ServiceType: models.StreamingBackendTypeWebtor,
	}, nil
}

func (s *LinkResolver) Validate(ctx context.Context, backend *models.StreamingBackend) error {
	if _, ok := s.userBackends[backend.Type]; !ok {
		return errors.New("backend implementation not found")
	}
	return s.userBackends[backend.Type].Validate(ctx, backend.AccessToken)
}
