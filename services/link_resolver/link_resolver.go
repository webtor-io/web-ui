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
	vmodels "github.com/webtor-io/web-ui/models/vault"
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
	enabledBackendsCache *lazymap.LazyMap[[]*models.StreamingBackend]
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

// ResolveLink resolves a streaming link for the file at (hash, fileIdx).
// All backends speak fileIdx directly: Webtor passes it through as a
// numeric content_id to rest-api, RD/Torbox use it as the index into
// their own torrent.Files slice. No path lookup is needed anywhere.
// Returns nil if content is not available or user doesn't have access.
func (s *LinkResolver) ResolveLink(ctx context.Context, userID uuid.UUID, apiClaims *api.Claims, userClaims *claims.Data, hash string, fileIdx int, requiresPayment bool) (*co.LinkResult, error) {
	log.WithFields(log.Fields{
		"hash":             hash,
		"file_idx":         fileIdx,
		"requires_payment": requiresPayment,
	}).Debug("resolving link")
	enabledBackends, err := s.GetUserEnabledBackends(ctx, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load user enabled backends")
	}

	for _, userBackend := range enabledBackends {
		backend, ok := s.userBackends[userBackend.Type]
		if !ok {
			log.WithField("backend_type", userBackend.Type).Warn("backend implementation not found")
			continue
		}
		url, cached, berr := backend.ResolveLink(ctx, userBackend.AccessToken, hash, fileIdx)
		if berr != nil {
			log.WithError(berr).WithField("backend_type", userBackend.Type).Warn("failed to generate link from backend")
			continue
		}
		if !cached {
			log.WithField("backend_type", userBackend.Type).Warn("link is not cached")
			continue
		}
		if merr := s.cacheIndex.MarkAsCached(ctx, userBackend.Type, hash, fileIdx); merr != nil {
			return nil, errors.Wrap(merr, "failed to mark as cached in cache index")
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

	// Fallback to webtor. Free users hit the paywall here.
	if requiresPayment && !s.isPaidUser(userClaims) {
		return nil, nil
	}
	url, cached, err := s.webtorBackend.ResolveLink(ctx, apiClaims, hash, fileIdx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate webtor link")
	}
	if cached {
		if merr := s.cacheIndex.MarkAsCached(ctx, models.StreamingBackendTypeWebtor, hash, fileIdx); merr != nil {
			return nil, errors.Wrap(merr, "failed to mark as cached in cache index")
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

// CheckAvailability reports whether the file (hash, fileIdx) is streamable
// on any of the user's enabled backends, falling back to Webtor.
// fileIdx is always known at the call site — Stremio addons and Library
// streams both populate StreamItem.FileIdx — which lets us skip the
// rest-api ListResourceContent round-trip that path-based resolution
// previously required.
func (s *LinkResolver) CheckAvailability(ctx context.Context, id uuid.UUID, cla *claims.Data, hash string, fileIdx int, requiresPayment bool) (*co.CheckAvailabilityResult, error) {
	r, err := s.cacheIndex.IsCached(ctx, hash, fileIdx)
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
	// Fallback: a resource that is vaulted (vault.resource.vaulted=true) is
	// guaranteed to be in Webtor's hot storage. The cacheIndex only learns
	// about this after a play has gone through ResolveLink, so a freshly
	// vaulted file would otherwise miss the ⚡ marker until first stream.
	// One indexed row read on vault.resource closes that gap cheaply.
	if !cached {
		if db := s.pg.Get(); db != nil {
			res, verr := vmodels.GetResource(ctx, db, hash)
			if verr != nil {
				log.WithError(verr).WithField("hash", hash).Debug("vault resource lookup failed")
			} else if res != nil && res.Vaulted {
				cached = true
				if merr := s.cacheIndex.MarkAsCached(ctx, models.StreamingBackendTypeWebtor, hash, fileIdx); merr != nil {
					log.WithError(merr).WithField("hash", hash).Debug("failed to mark vaulted resource as cached")
				}
			}
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
