package models

import (
	"context"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// ReleaseSubscriptionHit is one infohash a subscription has seen. The table
// is the memory that keeps a release out of a second email: the poller
// inserts every hash it finds and only the rows that were new to the table
// end up in a letter.
//
// IsBaseline marks the rows written by the very first poll of a subscription
// — what already existed when the user subscribed. They are recorded as
// notified so they never reach anyone, and kept apart so "how much was
// already there" stays answerable.
type ReleaseSubscriptionHit struct {
	tableName struct{} `pg:"release_subscription_hit"`

	SubscriptionID uuid.UUID `pg:"release_subscription_id,pk"`
	InfoHash       string    `pg:"infohash,pk"`

	Name       *string `pg:"name"`
	Size       *int64  `pg:"size"`
	SourceName *string `pg:"source_name"`
	Season     *int16  `pg:"season"`
	Episode    *int16  `pg:"episode"`

	IsBaseline  bool       `pg:"is_baseline,notnull,use_zero"`
	FirstSeenAt time.Time  `pg:"first_seen_at,default:now()"`
	NotifiedAt  *time.Time `pg:"notified_at"`
}

// GetName returns the release name, or the infohash when the source named
// nothing — an email row still has to say something.
func (h *ReleaseSubscriptionHit) GetName() string {
	if h.Name != nil && strings.TrimSpace(*h.Name) != "" {
		return *h.Name
	}
	return h.InfoHash
}

// InsertReleaseSubscriptionHits records what a poll found and returns how
// many of them were new. Conflicts are the normal case, not an error: most
// of what a poll sees it has already seen.
//
// Callers pass baseline=true for the first poll of a subscription, which
// stores the rows already notified — the user subscribed to hear about what
// comes next, not about the back catalogue.
func InsertReleaseSubscriptionHits(ctx context.Context, db *pg.DB, hits []ReleaseSubscriptionHit, baseline bool) (int, error) {
	if len(hits) == 0 {
		return 0, nil
	}
	now := time.Now()
	rows := make([]*ReleaseSubscriptionHit, 0, len(hits))
	for i := range hits {
		h := hits[i]
		h.InfoHash = strings.ToLower(strings.TrimSpace(h.InfoHash))
		if h.InfoHash == "" {
			continue
		}
		h.FirstSeenAt = now
		h.IsBaseline = baseline
		if baseline {
			h.NotifiedAt = &now
		} else {
			h.NotifiedAt = nil
		}
		rows = append(rows, &h)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res, err := db.Model(&rows).
		Context(ctx).
		OnConflict("(release_subscription_id, infohash) DO NOTHING").
		Insert()
	if err != nil {
		return 0, errors.Wrap(err, "failed to insert release subscription hits")
	}
	return res.RowsAffected(), nil
}

// ListPendingReleaseSubscriptionHits returns what this subscription still
// owes the user, oldest find first — the body of the next email.
func ListPendingReleaseSubscriptionHits(ctx context.Context, db *pg.DB, subscriptionID uuid.UUID) ([]ReleaseSubscriptionHit, error) {
	var hits []ReleaseSubscriptionHit
	err := db.Model(&hits).
		Context(ctx).
		Where("release_subscription_id = ?", subscriptionID).
		Where("notified_at IS NULL").
		Order("first_seen_at ASC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list pending release subscription hits")
	}
	return hits, nil
}

// MarkReleaseSubscriptionHitsNotified closes out the hashes that made it
// into a letter. Scoped to the exact hashes sent rather than "everything
// pending" so a find that landed between building the email and sending it
// stays pending instead of being silently swallowed.
func MarkReleaseSubscriptionHitsNotified(ctx context.Context, db *pg.DB, subscriptionID uuid.UUID, infohashes []string) error {
	if len(infohashes) == 0 {
		return nil
	}
	_, err := db.Model((*ReleaseSubscriptionHit)(nil)).
		Context(ctx).
		Set("notified_at = ?", time.Now()).
		Where("release_subscription_id = ?", subscriptionID).
		Where("infohash IN (?)", pg.In(infohashes)).
		Where("notified_at IS NULL").
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark release subscription hits notified")
	}
	return nil
}

// CountReleaseSubscriptionHits returns how many releases a subscription has
// seen in total — the "found N releases" figure in the profile row.
func CountReleaseSubscriptionHits(ctx context.Context, db *pg.DB, subscriptionID uuid.UUID) (int, error) {
	n, err := db.Model((*ReleaseSubscriptionHit)(nil)).
		Context(ctx).
		Where("release_subscription_id = ?", subscriptionID).
		Where("is_baseline = ?", false).
		Count()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count release subscription hits")
	}
	return n, nil
}

// ListUserReleaseSubscriptionHits returns every hit of every subscription a
// user owns. Used by the GDPR export, which has to include the rows keyed to
// the account, not just the subscriptions themselves.
func ListUserReleaseSubscriptionHits(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]ReleaseSubscriptionHit, error) {
	var hits []ReleaseSubscriptionHit
	err := db.Model(&hits).
		Context(ctx).
		Join("JOIN release_subscription AS s ON s.release_subscription_id = release_subscription_hit.release_subscription_id").
		Where("s.user_id = ?", userID).
		Order("release_subscription_hit.first_seen_at ASC").
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list user release subscription hits")
	}
	return hits, nil
}
