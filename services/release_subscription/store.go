package release_subscription

// The database, in one place. One Postgres backs both halves of the
// feature, so one adapter serves both interfaces: the service's `store`
// (what a request does) and the poller's `pollStore` (what the cron does).
// They were two near-identical wrappers over models until they were not.

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
)

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
	// AccountLang is here, not only on the poller's side, so a letter sent
	// from a request goes out in the same language as one sent from cron.
	AccountLang(ctx context.Context, userID uuid.UUID) string
	// StreamPrefs reads the account's stream settings, which a new
	// subscription starts out as a copy of.
	StreamPrefs(ctx context.Context, userID uuid.UUID) (resolutions []string, lang string)
	// HasStreamSources answers whether the account has anything the poller
	// could query — an addon or an enabled indexer. What Subscribe refuses
	// on, so a subscription that can never fire is not written.
	HasStreamSources(ctx context.Context, userID uuid.UUID) (bool, error)
	// UpsertMetadata makes sure movie_metadata / series_metadata has a row
	// for this video id. The profile renders posters through our own
	// endpoint, which resolves them from exactly those tables.
	UpsertMetadata(ctx context.Context, ct models.ContentType, md *models.VideoMetadata) error
	UpdatePreferences(ctx context.Context, id, userID uuid.UUID, resolutions []string, lang *string) error
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

// --- the poller's half ---

func (s pgStore) ListDue(ctx context.Context, now time.Time, limit int) ([]models.ReleaseSubscription, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return models.ListDueReleaseSubscriptions(ctx, db, now, limit)
}

func (s pgStore) ListOpenSeasons(ctx context.Context) ([]models.ReleaseSubscription, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return models.ListOpenSeasonReleaseSubscriptions(ctx, db)
}

func (s pgStore) InsertHits(ctx context.Context, hits []models.ReleaseSubscriptionHit, baseline bool) (int, error) {
	db, err := s.db()
	if err != nil {
		return 0, err
	}
	return models.InsertReleaseSubscriptionHits(ctx, db, hits, baseline)
}

func (s pgStore) ListPendingHits(ctx context.Context, subscriptionID uuid.UUID) ([]models.ReleaseSubscriptionHit, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return models.ListPendingReleaseSubscriptionHits(ctx, db, subscriptionID)
}

func (s pgStore) MarkHitsNotified(ctx context.Context, subscriptionID uuid.UUID, infohashes []string) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	return models.MarkReleaseSubscriptionHitsNotified(ctx, db, subscriptionID, infohashes)
}

func (s pgStore) MarkChecked(ctx context.Context, id uuid.UUID, state string, nextCheckAt time.Time) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	return models.MarkReleaseSubscriptionChecked(ctx, db, id, state, nextCheckAt)
}

func (s pgStore) MarkNotified(ctx context.Context, id uuid.UUID) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	return models.MarkReleaseSubscriptionNotified(ctx, db, id)
}

func (s pgStore) SeasonEpisodes(ctx context.Context, videoID string, season int16) ([]models.EpisodeMetadata, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	return models.ListEpisodeMetadataBySeason(ctx, db, videoID, season)
}

// AccountLang returns the language the account browses in, or "" when it has
// never been observed. Errors are swallowed: every caller has a fallback —
// the language the subscription was created in — and a lookup failure must
// not stop a letter.
func (s pgStore) AccountLang(ctx context.Context, userID uuid.UUID) string {
	db, err := s.db()
	if err != nil {
		return ""
	}
	us, err := models.GetUserSettings(ctx, db, userID)
	if err != nil {
		log.WithError(err).WithField("user_id", userID).Warn("failed to read account language")
		return ""
	}
	return us.GetLang()
}

// StreamPrefs returns the account's enabled resolutions and preferred
// language — the same two settings the Stremio addon and Discover honour.
// A read failure yields no preferences, which is the permissive answer: a
// subscription that reports slightly too much beats one that reports
// nothing.
func (s pgStore) StreamPrefs(ctx context.Context, userID uuid.UUID) ([]string, string) {
	db, err := s.db()
	if err != nil {
		return nil, ""
	}
	settings, err := models.GetUserStremioSettingsData(ctx, db, userID)
	if err != nil || settings == nil {
		if err != nil {
			log.WithError(err).WithField("user_id", userID).Warn("failed to read stream settings")
		}
		return nil, ""
	}
	var resolutions []string
	for _, r := range settings.PreferredResolutions {
		if r.Enabled {
			resolutions = append(resolutions, r.Resolution)
		}
	}
	// Every resolution enabled is the same as no preference, and storing it
	// as one keeps the poller from re-deriving that on every run.
	if len(resolutions) == len(settings.PreferredResolutions) {
		resolutions = nil
	}
	return resolutions, strings.TrimSpace(settings.PreferredLanguage)
}

// HasStreamSources reports whether the poll pipeline would have anyone to
// ask: an addon URL (enabled or not — BuildPollStreamsService queries them
// all) or an enabled Torznab indexer.
func (s pgStore) HasStreamSources(ctx context.Context, userID uuid.UUID) (bool, error) {
	db, err := s.db()
	if err != nil {
		return false, err
	}
	addons, err := models.CountUserStremioAddonUrls(ctx, db, userID)
	if err != nil {
		return false, errors.Wrap(err, "failed to count addon urls")
	}
	if addons > 0 {
		return true, nil
	}
	indexers, err := models.GetUserTorznabIndexers(ctx, db, userID)
	if err != nil {
		return false, errors.Wrap(err, "failed to list torznab indexers")
	}
	return len(indexers) > 0, nil
}

func (s pgStore) UpsertMetadata(ctx context.Context, ct models.ContentType, md *models.VideoMetadata) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	if ct == models.ContentTypeSeries {
		_, err = models.UpsertSeriesMetadata(ctx, db, md)
	} else {
		_, err = models.UpsertMovieMetadata(ctx, db, md)
	}
	return err
}

func (s pgStore) UpdatePreferences(ctx context.Context, id, userID uuid.UUID, resolutions []string, lang *string) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	return models.UpdateReleaseSubscriptionPreferences(ctx, db, id, userID, resolutions, lang)
}

// NewStore builds the production store. Both the web process and the poll
// command hand it the same *cs.PG they already hold.
func NewStore(pg *cs.PG) pgStore {
	return pgStore{pg: pg}
}
