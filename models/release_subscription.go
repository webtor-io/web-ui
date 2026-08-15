package models

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
	pkgerrors "github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// Subscription kinds. A subscription is on content, not on a torrent: the
// user asks to hear about new releases of a film, or of one season of a
// series. Everything downstream — which query the poller sends, when the
// subscription ends — follows from this one field.
const (
	ReleaseSubscriptionKindMovie  = "movie"
	ReleaseSubscriptionKindSeason = "season"
)

// Subscription states.
//
// A fresh row starts at pending_baseline: the first poll records what
// already exists without mailing it, otherwise subscribing to a season
// halfway through would immediately post its back catalogue. Only after
// that does the subscription become active and start producing letters.
//
// completed is terminal and reached only by season subscriptions, when the
// season has finished airing and the series is no longer in production.
// Movie subscriptions never complete on their own — a user who didn't like
// the first rips is still waiting for the one they want.
const (
	ReleaseSubscriptionStatePendingBaseline = "pending_baseline"
	ReleaseSubscriptionStateActive          = "active"
	ReleaseSubscriptionStateCompleted       = "completed"
)

// ReleaseSubscription is one user's standing request to be told about new
// releases of a film or of one season of a series.
//
// Title and PosterURL are snapshots taken when the subscription is created
// rather than a join: the profile row and the email have to render even when
// the metadata cache has since been evicted, and neither needs the current
// rating or plot.
type ReleaseSubscription struct {
	tableName struct{} `pg:"release_subscription"`

	ID      uuid.UUID `pg:"release_subscription_id,pk,type:uuid,default:uuid_generate_v4()"`
	UserID  uuid.UUID `pg:"user_id"`
	Kind    string    `pg:"kind,notnull"`
	VideoID string    `pg:"video_id,notnull"`
	Season  *int16    `pg:"season"`

	Title     *string `pg:"title"`
	PosterURL *string `pg:"poster_url"`

	// Lang is the interface language at the moment of subscribing. It is the
	// fallback for the notification language, used when the account has no
	// language of its own yet (see user_settings.lang).
	Lang string `pg:"lang,notnull"`
	// Source records which surface the subscription came from, so the three
	// entry points can be compared without touching analytics.
	Source string `pg:"source,notnull"`

	Enabled bool   `pg:"enabled,notnull,use_zero"`
	State   string `pg:"state,notnull"`

	LastCheckedAt  *time.Time `pg:"last_checked_at"`
	NextCheckAt    time.Time  `pg:"next_check_at,notnull"`
	LastNotifiedAt *time.Time `pg:"last_notified_at"`

	CreatedAt time.Time `pg:"created_at,default:now()"`
	UpdatedAt time.Time `pg:"updated_at,default:now()"`

	User *User `pg:"rel:has-one,fk:user_id"`
}

// GetTitle returns the stored title, falling back to the video id so a row
// whose metadata lookup failed still renders as something clickable.
func (s *ReleaseSubscription) GetTitle() string {
	if s.Title != nil && *s.Title != "" {
		return *s.Title
	}
	return s.VideoID
}

// GetSeason returns the season number, or 0 for a movie subscription.
func (s *ReleaseSubscription) GetSeason() int {
	if s.Season == nil {
		return 0
	}
	return int(*s.Season)
}

// IsSeason reports whether this subscription follows a season of a series.
func (s *ReleaseSubscription) IsSeason() bool {
	return s.Kind == ReleaseSubscriptionKindSeason
}

// CheckedAt returns the last poll time as a value, zero when the poller has
// not reached this subscription yet. Templates need a time.Time, not a
// pointer, to hand to the time-ago helper.
func (s *ReleaseSubscription) CheckedAt() time.Time {
	if s.LastCheckedAt == nil {
		return time.Time{}
	}
	return *s.LastCheckedAt
}

// IsCompleted reports whether the subscription has run its course.
func (s *ReleaseSubscription) IsCompleted() bool {
	return s.State == ReleaseSubscriptionStateCompleted
}

// CreateReleaseSubscription inserts a subscription, returning false when the
// user already has one for this content. The conflict target is the
// expression index release_subscription_content_unique — a movie has no
// season, and NULL != NULL would otherwise let the same film in twice.
func CreateReleaseSubscription(ctx context.Context, db *pg.DB, s *ReleaseSubscription) (bool, error) {
	if s.Lang == "" {
		s.Lang = "en"
	}
	if s.Source == "" {
		s.Source = "other"
	}
	if s.State == "" {
		s.State = ReleaseSubscriptionStatePendingBaseline
	}
	if s.NextCheckAt.IsZero() {
		s.NextCheckAt = time.Now()
	}
	s.Enabled = true

	res, err := db.Model(s).
		Context(ctx).
		OnConflict("DO NOTHING").
		Insert()
	if err != nil {
		return false, pkgerrors.Wrap(err, "failed to create release subscription")
	}
	return res.RowsAffected() > 0, nil
}

// GetUserReleaseSubscriptions returns every subscription of a user, newest
// first — the order the profile list renders in.
func GetUserReleaseSubscriptions(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]ReleaseSubscription, error) {
	var subs []ReleaseSubscription
	err := db.Model(&subs).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, pkgerrors.Wrap(err, "failed to list release subscriptions")
	}
	return subs, nil
}

// CountUserReleaseSubscriptions counts a user's subscriptions, including
// switched-off and completed ones. The free-tier cap is about how much
// standing work an account may park on the poller, and a row a user can
// re-enable with one click is still that.
func CountUserReleaseSubscriptions(ctx context.Context, db *pg.DB, userID uuid.UUID) (int, error) {
	n, err := db.Model((*ReleaseSubscription)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Count()
	if err != nil {
		return 0, pkgerrors.Wrap(err, "failed to count release subscriptions")
	}
	return n, nil
}

// FindUserReleaseSubscription looks up one subscription by its content key.
// Returns (nil, nil) when the user isn't subscribed.
func FindUserReleaseSubscription(ctx context.Context, db *pg.DB, userID uuid.UUID, kind, videoID string, season *int16) (*ReleaseSubscription, error) {
	s := &ReleaseSubscription{}
	err := db.Model(s).
		Context(ctx).
		Where("user_id = ? AND kind = ? AND video_id = ?", userID, kind, videoID).
		Where("coalesce(season, -1) = coalesce(?, -1)", season).
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, pkgerrors.Wrap(err, "failed to find release subscription")
	}
	return s, nil
}

// GetUserReleaseSubscriptionByID reads one row, scoped to its owner so a
// guessed id from another account yields (nil, nil) rather than data.
func GetUserReleaseSubscriptionByID(ctx context.Context, db *pg.DB, id, userID uuid.UUID) (*ReleaseSubscription, error) {
	s := &ReleaseSubscription{}
	err := db.Model(s).
		Context(ctx).
		Where("release_subscription_id = ? AND user_id = ?", id, userID).
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, pkgerrors.Wrap(err, "failed to get release subscription")
	}
	return s, nil
}

// DeleteUserReleaseSubscription removes a subscription and, through the
// foreign key, everything it had ever seen. Idempotent.
func DeleteUserReleaseSubscription(ctx context.Context, db *pg.DB, id, userID uuid.UUID) error {
	_, err := db.Model((*ReleaseSubscription)(nil)).
		Context(ctx).
		Where("release_subscription_id = ? AND user_id = ?", id, userID).
		Delete()
	if err != nil {
		return pkgerrors.Wrap(err, "failed to delete release subscription")
	}
	return nil
}

// DeleteUserReleaseSubscriptionByContent removes a subscription addressed the
// way the client knows it — by content, not by row id. Returns whether a row
// was actually removed.
func DeleteUserReleaseSubscriptionByContent(ctx context.Context, db *pg.DB, userID uuid.UUID, kind, videoID string, season *int16) (bool, error) {
	res, err := db.Model((*ReleaseSubscription)(nil)).
		Context(ctx).
		Where("user_id = ? AND kind = ? AND video_id = ?", userID, kind, videoID).
		Where("coalesce(season, -1) = coalesce(?, -1)", season).
		Delete()
	if err != nil {
		return false, pkgerrors.Wrap(err, "failed to delete release subscription")
	}
	return res.RowsAffected() > 0, nil
}

// SetUserReleaseSubscriptionEnabled flips the profile toggle. A subscription
// switched back on is checked at the next poll rather than immediately — the
// toggle is not a "search now" button.
func SetUserReleaseSubscriptionEnabled(ctx context.Context, db *pg.DB, id, userID uuid.UUID, enabled bool) error {
	_, err := db.Model((*ReleaseSubscription)(nil)).
		Context(ctx).
		Set("enabled = ?", enabled).
		Where("release_subscription_id = ? AND user_id = ?", id, userID).
		Update()
	if err != nil {
		return pkgerrors.Wrap(err, "failed to update release subscription")
	}
	return nil
}

// ListDueReleaseSubscriptions returns the poller's batch: enabled, unfinished
// subscriptions whose next check has come round, oldest due first. The User
// relation is joined because the poller needs the address to mail and the
// account to build the user's own stream sources from.
func ListDueReleaseSubscriptions(ctx context.Context, db *pg.DB, now time.Time, limit int) ([]ReleaseSubscription, error) {
	var subs []ReleaseSubscription
	q := db.Model(&subs).
		Context(ctx).
		Relation("User").
		Where("release_subscription.enabled = ?", true).
		Where("release_subscription.state <> ?", ReleaseSubscriptionStateCompleted).
		Where("release_subscription.next_check_at <= ?", now).
		Order("release_subscription.next_check_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Select(); err != nil {
		return nil, pkgerrors.Wrap(err, "failed to list due release subscriptions")
	}
	return subs, nil
}

// MarkReleaseSubscriptionChecked stamps a completed poll and schedules the
// next one. State is written too because the baseline pass ends by promoting
// the row to active.
func MarkReleaseSubscriptionChecked(ctx context.Context, db *pg.DB, id uuid.UUID, state string, nextCheckAt time.Time) error {
	now := time.Now()
	_, err := db.Model((*ReleaseSubscription)(nil)).
		Context(ctx).
		Set("last_checked_at = ?", now).
		Set("next_check_at = ?", nextCheckAt).
		Set("state = ?", state).
		Where("release_subscription_id = ?", id).
		Update()
	if err != nil {
		return pkgerrors.Wrap(err, "failed to mark release subscription checked")
	}
	return nil
}

// MarkReleaseSubscriptionNotified records that a letter went out. Called only
// after the mailer reports success, so a failed send leaves the pending hits
// pending and the next run retries them.
func MarkReleaseSubscriptionNotified(ctx context.Context, db *pg.DB, id uuid.UUID) error {
	now := time.Now()
	_, err := db.Model((*ReleaseSubscription)(nil)).
		Context(ctx).
		Set("last_notified_at = ?", now).
		Where("release_subscription_id = ?", id).
		Update()
	if err != nil {
		return pkgerrors.Wrap(err, "failed to mark release subscription notified")
	}
	return nil
}
