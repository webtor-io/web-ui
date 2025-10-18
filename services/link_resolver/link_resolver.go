package link_resolver

import (
	"context"
	"net/http"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/link_resolver/backends"
	co "github.com/webtor-io/web-ui/services/link_resolver/common"
)

// LinkResolver resolves streaming links across multiple backends (RealDebrid, Torbox, Webtor)
// by checking content availability and generating direct download URLs
type LinkResolver struct {
	pg            *cs.PG
	userBackends  map[models.StreamingBackendType]co.Backend
	webtorBackend *backends.Webtor
}

// New creates a new LinkResolver with configured backends
func New(cl *http.Client, pg *cs.PG, apiService *api.Api) *LinkResolver {
	return &LinkResolver{
		pg: pg,
		userBackends: map[models.StreamingBackendType]co.Backend{
			models.StreamingBackendTypeRealDebrid: backends.NewRealDebrid(cl),
			models.StreamingBackendTypeTorbox:     backends.NewTorbox(),
		},
		webtorBackend: backends.NewWebtor(apiService),
	}
}

func (s *LinkResolver) getEnabledBackends(userID uuid.UUID) ([]*models.StreamingBackend, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	// Get user's streaming backends ordered by priority (highest first)
	userBackends, err := models.GetUserStreamingBackends(db, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user streaming backends")
	}

	// Filter to only enabled backends
	enabledBackends := make([]*models.StreamingBackend, 0)
	for _, backend := range userBackends {
		if backend.Enabled {
			enabledBackends = append(enabledBackends, backend)
		}
	}
	log.WithField("enabled_backends_count", len(enabledBackends)).Debug("found enabled streaming backends")
	return enabledBackends, nil
}

// CheckAvailability checks if content is available across user's enabled streaming backends
// It tries each backend by priority, then falls back to Webtor if payment is not required or user is paid
// Returns nil if content is not available in any backend
func (s *LinkResolver) CheckAvailability(ctx context.Context, userID uuid.UUID, apiClaims *api.Claims, userClaims *claims.Data, hash, path string, requiresPayment bool) (*co.CheckAvailabilityResult, error) {
	log.WithFields(log.Fields{
		"hash":             hash,
		"path":             path,
		"requires_payment": requiresPayment,
	}).Debug("checking availability")
	enabledBackends, err := s.getEnabledBackends(userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get enabled backends")
	}
	return s.checkAvailability(ctx, enabledBackends, apiClaims, userClaims, hash, path, requiresPayment)
}

func (s *LinkResolver) checkAvailability(ctx context.Context, enabledBackends []*models.StreamingBackend, apiClaims *api.Claims, userClaims *claims.Data, hash, path string, requiresPayment bool) (*co.CheckAvailabilityResult, error) {
	// Try each backend by priority
	for _, userBackend := range enabledBackends {
		backend, exists := s.userBackends[userBackend.Type]
		if !exists {
			log.WithField("backend_type", userBackend.Type).Warn("backend implementation not found")
			continue
		}

		cached, err := backend.CheckAvailability(ctx, userBackend.AccessToken, hash, path)
		if err != nil {
			log.WithError(err).
				WithFields(log.Fields{
					"backend_type": userBackend.Type,
					"cached":       cached,
				}).
				Warn("failed to check backend availability")
			continue
		}
		if !cached {
			continue
		}
		log.WithFields(log.Fields{
			"backend_type": userBackend.Type,
			"cached":       cached,
		}).Info("found available content in backend")
		return &co.CheckAvailabilityResult{
			ServiceType: userBackend.Type,
			Cached:      cached,
		}, nil
	}

	// Fallback to webtor if no debrid service worked
	// Webtor is always available as fallback (if payment required, check if user is paid)
	if requiresPayment && !s.isPaidUser(userClaims) {
		return nil, nil
	}

	cached, err := s.webtorBackend.CheckAvailability(ctx, apiClaims, hash, path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check webtor availability")
	}

	log.WithField("cached", cached).
		Info("found available content in webtor")
	return &co.CheckAvailabilityResult{
		ServiceType: models.StreamingBackendTypeWebtor,
		Cached:      cached,
	}, nil
}

// ResolveLink resolves a streaming link for the given content
// It first checks availability, then generates a direct download URL from the appropriate backend
// Returns nil if content is not available or user doesn't have access
func (s *LinkResolver) ResolveLink(ctx context.Context, userID uuid.UUID, apiClaims *api.Claims, userClaims *claims.Data, hash, path string, requiresPayment bool) (*co.LinkResult, error) {
	log.WithFields(log.Fields{
		"hash":             hash,
		"path":             path,
		"requires_payment": requiresPayment,
	}).Debug("resolving link")
	enabledBackends, err := s.getEnabledBackends(userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get enabled backends")
	}
	return s.resolveLink(ctx, enabledBackends, apiClaims, userClaims, hash, path, requiresPayment)
}

func (s *LinkResolver) resolveLink(ctx context.Context, enabledBackends []*models.StreamingBackend, apiClaims *api.Claims, userClaims *claims.Data, hash, path string, requiresPayment bool) (*co.LinkResult, error) {
	// Check availability first
	availability, err := s.checkAvailability(ctx, enabledBackends, apiClaims, userClaims, hash, path, requiresPayment)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check availability")
	}

	if availability == nil {
		log.Debug("no availability found")
		return nil, nil
	}

	if availability.ServiceType == models.StreamingBackendTypeWebtor {
		url, cached, err := s.webtorBackend.ResolveLink(ctx, apiClaims, hash, path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate webtor link")
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

	// Find the access token for the selected backend
	var token string
	for _, b := range enabledBackends {
		if b.Type == availability.ServiceType {
			token = b.AccessToken
			break
		}
	}

	// Generate link using the selected backend
	backend := s.userBackends[availability.ServiceType]
	url, cached, err := backend.ResolveLink(ctx, token, hash, path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to generate link from backend %s", availability.ServiceType)
	}

	log.WithFields(log.Fields{
		"url":          url,
		"cached":       cached,
		"backend_type": availability.ServiceType,
	}).Info("generated streaming link from backend")
	return &co.LinkResult{
		URL:         url,
		ServiceType: availability.ServiceType,
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
