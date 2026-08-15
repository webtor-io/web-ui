// Package release_subscription owns what a subscription is: who may have
// one, on what, and what has to be true before a row is written. The HTTP
// layer above it only translates requests and errors; the poller below it
// only reads the rows this package produces.
package release_subscription

import (
	"context"
	"strings"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
)

// FreeTierLimit caps how many subscriptions a free account may hold. Each
// one is standing work for the poller — an outbound query to the user's own
// addons and indexers, on a schedule, forever — so the cap is about the load
// an account can park on us, not about the value of the feature.
const FreeTierLimit = 3

// Sources a subscription can come from. Validated here so the column stays
// clean enough to compare the three entry points against each other.
var validSources = map[string]struct{}{
	"discover_season": {}, // season selector in the Discover stream modal
	"resource_banner": {}, // banner on the torrent page
	"empty_streams":   {}, // stream modal found nothing at all
	"empty_filters":   {}, // stream modal found nothing matching the filters
	"profile":         {}, // re-added from the profile list
}

var (
	// ErrLimitExceeded is the free-tier cap, surfaced as 402 by the handler.
	ErrLimitExceeded = errors.New("release subscription limit exceeded")
	// ErrNotEligible means the content cannot be subscribed to: a season of
	// a series that has finished airing has no next episode to wait for.
	ErrNotEligible = errors.New("content is not eligible for a subscription")
	// ErrBadRequest covers malformed input the handler maps to 400.
	ErrBadRequest = errors.New("invalid subscription request")
)

// Service is the subscription business logic. The enricher is optional —
// without it eligibility cannot be verified and season subscriptions are
// refused rather than accepted blind.
type Service struct {
	pg *cs.PG
	en *enrich.Enricher
}

func New(pg *cs.PG, en *enrich.Enricher) *Service {
	return &Service{pg: pg, en: en}
}

// Request is one "subscribe me to this" from any of the entry points.
type Request struct {
	Kind    string
	VideoID string
	Season  int
	Source  string
	Lang    string
}

// Limit returns the cap for a tier: FreeTierLimit for free accounts, -1 for
// paid. Callers that have no claims at all (claims-provider disabled) treat
// the user as paid, matching the convention in the rest of the codebase.
func Limit(freeTier bool) int {
	if freeTier {
		return FreeTierLimit
	}
	return -1
}

// Subscribe validates the request, enforces the cap and writes the row.
// Returns the subscription and whether it was created now — subscribing
// twice is not an error, it just doesn't produce a second row.
func (s *Service) Subscribe(ctx context.Context, userID uuid.UUID, req Request, limit int) (*models.ReleaseSubscription, bool, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, false, errors.New("no db")
	}

	kind, season, err := normalize(req)
	if err != nil {
		return nil, false, err
	}

	// Already subscribed: answer from the existing row without spending a
	// count query or an eligibility check on it.
	existing, err := models.FindUserReleaseSubscription(ctx, db, userID, kind, req.VideoID, season)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}

	if err := s.checkEligible(ctx, kind, req.VideoID); err != nil {
		return nil, false, err
	}

	if limit > 0 {
		count, err := models.CountUserReleaseSubscriptions(ctx, db, userID)
		if err != nil {
			return nil, false, err
		}
		if count >= limit {
			return nil, false, ErrLimitExceeded
		}
	}

	sub := &models.ReleaseSubscription{
		UserID:  userID,
		Kind:    kind,
		VideoID: req.VideoID,
		Season:  season,
		Lang:    req.Lang,
		Source:  normalizeSource(req.Source),
	}
	s.fillMetadata(ctx, db, sub)

	created, err := models.CreateReleaseSubscription(ctx, db, sub)
	if err != nil {
		return nil, false, err
	}
	if !created {
		// Lost a race with a concurrent add. Read back what won so the
		// caller still gets a subscription to render.
		existing, err := models.FindUserReleaseSubscription(ctx, db, userID, kind, req.VideoID, season)
		if err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}
	return sub, true, nil
}

// Unsubscribe removes a subscription addressed by content. Returns whether
// anything was removed.
func (s *Service) Unsubscribe(ctx context.Context, userID uuid.UUID, req Request) (bool, error) {
	db := s.pg.Get()
	if db == nil {
		return false, errors.New("no db")
	}
	kind, season, err := normalize(req)
	if err != nil {
		return false, err
	}
	return models.DeleteUserReleaseSubscriptionByContent(ctx, db, userID, kind, req.VideoID, season)
}

// List returns the user's subscriptions for the profile section.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]models.ReleaseSubscription, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return models.GetUserReleaseSubscriptions(ctx, db, userID)
}

// Count returns how many subscriptions a user holds, for the "you have used
// N of 3" line on the profile.
func (s *Service) Count(ctx context.Context, userID uuid.UUID) (int, error) {
	db := s.pg.Get()
	if db == nil {
		return 0, errors.New("no db")
	}
	return models.CountUserReleaseSubscriptions(ctx, db, userID)
}

// Delete removes one subscription by row id, scoped to its owner.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	return models.DeleteUserReleaseSubscription(ctx, db, id, userID)
}

// SetEnabled flips the profile toggle for one subscription.
func (s *Service) SetEnabled(ctx context.Context, userID, id uuid.UUID, enabled bool) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	return models.SetUserReleaseSubscriptionEnabled(ctx, db, id, userID, enabled)
}

// Eligible reports whether content can be subscribed to. Exposed so the
// resource-page banner and the Discover surfaces can ask the same question
// the write path asks, instead of each inventing its own rule.
func (s *Service) Eligible(ctx context.Context, kind, videoID string) bool {
	return s.checkEligible(ctx, kind, videoID) == nil
}

// checkEligible is the one rule that decides whether a subscription makes
// sense.
//
// For a season it is "is this series still producing episodes" — the same
// AiringChecker capability the resource page has used since the fake-door.
// A season of a finished series has no next episode, so a subscription to it
// would poll forever and never fire.
//
// A movie is always eligible: the entry point is a search that found
// nothing, and no local signal tells us whether a release is coming. That
// judgement belongs to the user.
func (s *Service) checkEligible(ctx context.Context, kind, videoID string) error {
	if kind != models.ReleaseSubscriptionKindSeason {
		return nil
	}
	if s.en == nil || !s.en.HasMappers() {
		// Without an enricher the airing check cannot run. Refusing is the
		// conservative branch: an accepted row would be unverifiable and
		// might poll a finished series indefinitely.
		return ErrNotEligible
	}
	if !s.en.IsAiringSeries(ctx, videoID) {
		return ErrNotEligible
	}
	return nil
}

// fillMetadata takes the title/poster snapshot the profile row and the email
// render from. Best-effort: a subscription with no title still works, it
// just shows its video id until the next lookup succeeds.
func (s *Service) fillMetadata(ctx context.Context, db *pg.DB, sub *models.ReleaseSubscription) {
	if s.en == nil || !s.en.HasMappers() {
		return
	}
	ct := models.ContentTypeMovie
	if sub.IsSeason() {
		ct = models.ContentTypeSeries
	}
	md, err := s.en.LookupByVideoID(ctx, sub.VideoID, ct)
	if err != nil {
		log.WithError(err).
			WithField("feature", "release_subscription").
			WithField("video_id", sub.VideoID).
			Warn("metadata lookup failed; subscribing without a title")
		return
	}
	if md == nil {
		return
	}
	if md.Title != "" {
		title := md.Title
		sub.Title = &title
	}
	if md.PosterURL != "" {
		poster := md.PosterURL
		sub.PosterURL = &poster
	}
}

// normalize validates the request and returns the storage shape of its
// content key.
func normalize(req Request) (string, *int16, error) {
	videoID := strings.TrimSpace(req.VideoID)
	// IMDB ids are the only identifier every stream source speaks: the
	// addons key on them and the Torznab queries carry them. A row with
	// anything else could never be polled.
	if !strings.HasPrefix(videoID, "tt") || strings.Contains(videoID, ":") {
		return "", nil, errors.Wrap(ErrBadRequest, "video_id must be a bare IMDB title id")
	}
	switch req.Kind {
	case models.ReleaseSubscriptionKindMovie:
		return models.ReleaseSubscriptionKindMovie, nil, nil
	case models.ReleaseSubscriptionKindSeason:
		if req.Season <= 0 {
			return "", nil, errors.Wrap(ErrBadRequest, "season must be positive")
		}
		season := int16(req.Season)
		return models.ReleaseSubscriptionKindSeason, &season, nil
	}
	return "", nil, errors.Wrap(ErrBadRequest, "kind must be 'movie' or 'season'")
}

// normalizeSource keeps unknown source strings out of the column without
// failing the request — an older client posting a source we've since renamed
// should still be able to subscribe.
func normalizeSource(source string) string {
	if _, ok := validSources[source]; ok {
		return source
	}
	return "other"
}
