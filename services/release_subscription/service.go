// Package release_subscription owns what a subscription is: who may have
// one, on what, and what has to be true before a row is written. The HTTP
// layer above it only translates requests and errors; the poller below it
// only reads the rows this package produces.
package release_subscription

import (
	"context"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/notification"
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

// Mailer sends the two letters a subscription's life produces outside the
// poller: the confirmation when it starts, and the notice when the user ends
// it. An interface so the service does not depend on SMTP being configured —
// a nil mailer simply sends nothing.
type Mailer interface {
	SendSubscriptionOn(to string, sub notification.SubscriptionView) error
	SendSubscriptionOff(to string, sub notification.SubscriptionView, completed bool) error
}

// store is the database behind the service. An interface for the same
// reason the poller has one: the rules worth testing here are the free-tier
// cap, the eligibility gate and which letter goes out when — none of which
// should need Postgres to exercise.
type store interface {
	Find(ctx context.Context, userID uuid.UUID, kind, videoID string, season *int16) (*models.ReleaseSubscription, error)
	FindByID(ctx context.Context, id, userID uuid.UUID) (*models.ReleaseSubscription, error)
	Get(ctx context.Context, id uuid.UUID) (*models.ReleaseSubscription, error)
	Count(ctx context.Context, userID uuid.UUID) (int, error)
	List(ctx context.Context, userID uuid.UUID) ([]models.ReleaseSubscription, error)
	Create(ctx context.Context, sub *models.ReleaseSubscription) (bool, error)
	DeleteByContent(ctx context.Context, userID uuid.UUID, kind, videoID string, season *int16) (bool, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error
	DeleteByID(ctx context.Context, id uuid.UUID) error
	SetEnabled(ctx context.Context, id, userID uuid.UUID, enabled bool) error
}

// pgStore is the production store.
type pgStore struct{ pg *cs.PG }

func (s pgStore) db() (*pg.DB, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return db, nil
}

func (s pgStore) Find(ctx context.Context, userID uuid.UUID, kind, videoID string, season *int16) (*models.ReleaseSubscription, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return models.FindUserReleaseSubscription(ctx, db, userID, kind, videoID, season)
}

func (s pgStore) FindByID(ctx context.Context, id, userID uuid.UUID) (*models.ReleaseSubscription, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return models.GetUserReleaseSubscriptionByID(ctx, db, id, userID)
}

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (*models.ReleaseSubscription, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return models.GetReleaseSubscriptionByID(ctx, db, id)
}

func (s pgStore) Count(ctx context.Context, userID uuid.UUID) (int, error) {
	db, err := s.db()
	if err != nil {
		return 0, err
	}
	return models.CountUserReleaseSubscriptions(ctx, db, userID)
}

func (s pgStore) List(ctx context.Context, userID uuid.UUID) ([]models.ReleaseSubscription, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return models.GetUserReleaseSubscriptions(ctx, db, userID)
}

func (s pgStore) Create(ctx context.Context, sub *models.ReleaseSubscription) (bool, error) {
	db, err := s.db()
	if err != nil {
		return false, err
	}
	return models.CreateReleaseSubscription(ctx, db, sub)
}

func (s pgStore) DeleteByContent(ctx context.Context, userID uuid.UUID, kind, videoID string, season *int16) (bool, error) {
	db, err := s.db()
	if err != nil {
		return false, err
	}
	return models.DeleteUserReleaseSubscriptionByContent(ctx, db, userID, kind, videoID, season)
}

func (s pgStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	return models.DeleteUserReleaseSubscription(ctx, db, id, userID)
}

func (s pgStore) DeleteByID(ctx context.Context, id uuid.UUID) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	return models.DeleteReleaseSubscriptionByID(ctx, db, id)
}

func (s pgStore) SetEnabled(ctx context.Context, id, userID uuid.UUID, enabled bool) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	return models.SetUserReleaseSubscriptionEnabled(ctx, db, id, userID, enabled)
}

// airingCheck is the "is this series still producing episodes" capability —
// the Enricher's AiringChecker facade, behind an interface so the
// eligibility rule can be tested without a TMDB cache.
type airingCheck interface {
	IsAiringSeries(ctx context.Context, videoID string) bool
}

// metadataLookup fills the title/poster snapshot a profile row and an email
// render from. Interface-shaped so tests need no mappers.
type metadataLookup interface {
	LookupByVideoID(ctx context.Context, videoID string, ct models.ContentType) (*models.VideoMetadata, error)
}

// Service is the subscription business logic. The enricher is optional —
// without it eligibility cannot be verified and season subscriptions are
// refused rather than accepted blind.
type Service struct {
	store  store
	airing airingCheck
	meta   metadataLookup
	mail   Mailer
	domain string
	secret string
	// sync makes the confirmation letters block instead of firing off a
	// goroutine. Production leaves it false; tests set it so an assertion
	// does not race the mailer.
	sync bool
}

func New(pg *cs.PG, en *enrich.Enricher, mail Mailer, domain, secret string) *Service {
	s := &Service{store: pgStore{pg: pg}, mail: mail, domain: domain, secret: secret}
	// A mapper-less enricher answers nothing useful, so it is left out
	// entirely rather than nil-checked at every call site.
	if en != nil && en.HasMappers() {
		s.airing = en
		s.meta = en
	}
	return s
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
func (s *Service) Subscribe(ctx context.Context, u *auth.User, req Request, limit int) (*models.ReleaseSubscription, bool, error) {
	userID := u.ID

	kind, season, err := normalize(req)
	if err != nil {
		return nil, false, err
	}

	// Already subscribed: answer from the existing row without spending a
	// count query or an eligibility check on it.
	existing, err := s.store.Find(ctx, userID, kind, req.VideoID, season)
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
		count, err := s.store.Count(ctx, userID)
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
		// The state machine is the service's, not the schema's: a new row
		// owes the user a silent first look before it may say anything.
		// The column default says the same thing, as a backstop for rows
		// written any other way.
		State:       models.ReleaseSubscriptionStatePendingBaseline,
		Enabled:     true,
		NextCheckAt: time.Now(),
	}
	s.fillMetadata(ctx, sub)

	created, err := s.store.Create(ctx, sub)
	if err != nil {
		return nil, false, err
	}
	if created {
		s.mailOn(u, sub)
	}
	if !created {
		// Lost a race with a concurrent add. Read back what won so the
		// caller still gets a subscription to render.
		existing, err := s.store.Find(ctx, userID, kind, req.VideoID, season)
		if err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}
	return sub, true, nil
}

// Unsubscribe removes a subscription addressed by content. Returns whether
// anything was removed.
func (s *Service) Unsubscribe(ctx context.Context, u *auth.User, req Request) (bool, error) {
	kind, season, err := normalize(req)
	if err != nil {
		return false, err
	}
	// Read before deleting: the notice names what it is about, and after the
	// DELETE there is nothing left to name it with.
	sub, err := s.store.Find(ctx, u.ID, kind, req.VideoID, season)
	if err != nil {
		return false, err
	}
	removed, err := s.store.DeleteByContent(ctx, u.ID, kind, req.VideoID, season)
	if err != nil {
		return false, err
	}
	if removed && sub != nil {
		s.mailOff(u, sub)
	}
	return removed, nil
}

// List returns the user's subscriptions for the profile section.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]models.ReleaseSubscription, error) {
	return s.store.List(ctx, userID)
}

// Count returns how many subscriptions a user holds, for the "you have used
// N of 3" line on the profile.
func (s *Service) Count(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.store.Count(ctx, userID)
}

// Delete removes one subscription by row id, scoped to its owner.
func (s *Service) Delete(ctx context.Context, u *auth.User, id uuid.UUID) error {
	sub, err := s.store.FindByID(ctx, id, u.ID)
	if err != nil {
		return err
	}
	if sub == nil {
		return nil
	}
	if err := s.store.Delete(ctx, id, u.ID); err != nil {
		return err
	}
	s.mailOff(u, sub)
	return nil
}

// DeleteByToken serves the one-click unsubscribe link from an email, where
// the signed token is the only authorization there is — the reader is not
// logged in, and requiring them to be defeats the point of the link.
// Returns the subscription that was removed, or nil when the token addresses
// one that is already gone.
func (s *Service) DeleteByToken(ctx context.Context, raw string) (*models.ReleaseSubscription, error) {
	id, err := ParseUnsubscribeToken(s.secret, raw)
	if err != nil {
		return nil, err
	}
	sub, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		// Already unsubscribed. Idempotent by design: a link clicked twice
		// must read as "done", not as an error.
		return nil, nil
	}
	if err := s.store.DeleteByID(ctx, id); err != nil {
		return nil, err
	}
	return sub, nil
}

// SetEnabled flips the profile toggle for one subscription.
func (s *Service) SetEnabled(ctx context.Context, userID, id uuid.UUID, enabled bool) error {
	return s.store.SetEnabled(ctx, id, userID, enabled)
}

// mailOn and mailOff send off the request goroutine. An SMTP dial is a
// remote round-trip, and the user is waiting on a toast that says what
// already happened in the database — the letter is not part of that answer.
func (s *Service) mailOn(u *auth.User, sub *models.ReleaseSubscription) {
	if s.mail == nil || u.Email == "" {
		return
	}
	view := s.view(sub)
	s.run(func() {
		if err := s.mail.SendSubscriptionOn(u.Email, view); err != nil {
			log.WithError(err).
				WithField("feature", "release_subscription").
				WithField("subscription_id", sub.ID).
				Error("failed to send subscription confirmation")
		}
	})
}

func (s *Service) mailOff(u *auth.User, sub *models.ReleaseSubscription) {
	if s.mail == nil || u.Email == "" {
		return
	}
	view := s.view(sub)
	s.run(func() {
		if err := s.mail.SendSubscriptionOff(u.Email, view, false); err != nil {
			log.WithError(err).
				WithField("feature", "release_subscription").
				WithField("subscription_id", sub.ID).
				Error("failed to send subscription removal notice")
		}
	})
}

// run sends the letter off the request goroutine in production, and inline
// under test, where a background send would race the assertions.
func (s *Service) run(f func()) {
	if s.sync {
		f()
		return
	}
	go f()
}

func (s *Service) view(sub *models.ReleaseSubscription) notification.SubscriptionView {
	return notification.SubscriptionView{
		ID:             sub.ID,
		Title:          sub.GetTitle(),
		Season:         sub.GetSeason(),
		Lang:           sub.Lang,
		UnsubscribeURL: UnsubscribeURL(s.domain, s.secret, sub.ID),
	}
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
	if s.airing == nil {
		// Without an airing check the rule cannot run. Refusing is the
		// conservative branch: an accepted row would be unverifiable and
		// might poll a finished series indefinitely.
		return ErrNotEligible
	}
	if !s.airing.IsAiringSeries(ctx, videoID) {
		return ErrNotEligible
	}
	return nil
}

// fillMetadata takes the title/poster snapshot the profile row and the email
// render from. Best-effort: a subscription with no title still works, it
// just shows its video id until the next lookup succeeds.
func (s *Service) fillMetadata(ctx context.Context, sub *models.ReleaseSubscription) {
	if s.meta == nil {
		return
	}
	ct := models.ContentTypeMovie
	if sub.IsSeason() {
		ct = models.ContentTypeSeries
	}
	md, err := s.meta.LookupByVideoID(ctx, sub.VideoID, ct)
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
