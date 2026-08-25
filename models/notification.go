package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type Notification struct {
	tableName      struct{}   `pg:"notification"`
	NotificationID uuid.UUID  `pg:"notification_id,pk,type:uuid,default:uuid_generate_v4()"`
	Key            string     `pg:"key,notnull"`
	Title          string     `pg:"title,notnull"`
	Template       string     `pg:"template,notnull"`
	Body           string     `pg:"body,notnull"`
	To             *string    `pg:"to"`
	UserID         *uuid.UUID `pg:"user_id,type:uuid"`
	ReadAt         *time.Time `pg:"read_at"`
	MailedAt       *time.Time `pg:"mailed_at"`
	CreatedAt      time.Time  `pg:"created_at,notnull,default:now()"`
	UpdatedAt      time.Time  `pg:"updated_at,notnull,default:now()"`
}

// CreateNotification creates a new notification record
func CreateNotification(ctx context.Context, db pg.DBI, n *Notification) error {
	_, err := db.Model(n).Context(ctx).Insert()
	if err != nil {
		return errors.Wrap(err, "failed to create notification")
	}
	return nil
}

// GetLastMailedNotificationByKeyAndUser returns the newest notification for
// this key and user that was actually mailed (mailed_at IS NOT NULL). That
// qualifier is what makes a hit trustworthy as "already sent": a row left
// behind by a send that never happened -- no SMTP configured, or a failed
// dial -- has mailed_at NULL and is invisible here, so it cannot suppress a
// later attempt to mail the same key.
func GetLastMailedNotificationByKeyAndUser(ctx context.Context, db pg.DBI, key string, userID uuid.UUID) (*Notification, error) {
	n := &Notification{}
	err := db.Model(n).
		Context(ctx).
		Where("key = ? AND user_id = ? AND mailed_at IS NOT NULL", key, userID).
		Order("created_at DESC").
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get last mailed notification")
	}
	return n, nil
}

// GetLastNotificationByKeyAndUser returns the newest notification for this
// key and user whatever became of its letter -- mailed, failed, or never
// attempted at all. The absent mailed_at filter is the whole difference
// from GetLastMailedNotificationByKeyAndUser above, and it is deliberate:
// this query backs the feed guard, and the feed entry IS the notification,
// so a row existing at all already answers "has this user been told about
// this key". A redelivered event must find that row and reuse it rather
// than add a second copy of the same message to the feed.
//
// The two queries are kept apart rather than merged because they answer
// different questions and one row cannot answer both -- see Service.Send
// for the arrangement where the newest row is unmailed while an older row
// inside the same window was mailed.
//
// Covered by notification_key_user_created_idx (key, user_id, created_at
// DESC), the same index the mailed variant uses.
func GetLastNotificationByKeyAndUser(ctx context.Context, db pg.DBI, key string, userID uuid.UUID) (*Notification, error) {
	n := &Notification{}
	err := db.Model(n).
		Context(ctx).
		Where("key = ? AND user_id = ?", key, userID).
		Order("created_at DESC").
		Limit(1).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get last notification")
	}
	return n, nil
}

// MarkNotificationMailed stamps mailed_at on a notification row once an SMTP
// server has actually accepted the message. It is the only place that
// column is set, which is what lets the dedupe query above tell a real send
// apart from a feed entry whose letter never left.
func MarkNotificationMailed(ctx context.Context, db pg.DBI, id uuid.UUID) error {
	n := &Notification{NotificationID: id}
	_, err := db.Model(n).
		Context(ctx).
		Set("mailed_at = now()").
		Set("updated_at = now()").
		WherePK().
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark notification mailed")
	}
	return nil
}

// CountUnreadNotifications counts a user's notifications that have not been
// read yet. Backs the navbar badge, so it counts feed entries regardless of
// whether they were ever mailed -- a notification with no deliverable
// address is still unread.
func CountUnreadNotifications(ctx context.Context, db pg.DBI, userID uuid.UUID) (int, error) {
	count, err := db.Model((*Notification)(nil)).
		Context(ctx).
		Where("user_id = ? AND read_at IS NULL", userID).
		Count()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count unread notifications")
	}
	return count, nil
}

// ListNotificationsByUser returns a user's notifications, newest first,
// capped at limit. Backs the notifications page.
func ListNotificationsByUser(ctx context.Context, db pg.DBI, userID uuid.UUID, limit int) ([]Notification, error) {
	var ns []Notification
	err := db.Model(&ns).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to list notifications")
	}
	return ns, nil
}

// MarkAllNotificationsRead stamps read_at on every currently-unread
// notification belonging to a user. Only touches rows that are still
// unread, so it does not disturb the read_at already recorded on entries
// read individually.
func MarkAllNotificationsRead(ctx context.Context, db pg.DBI, userID uuid.UUID) error {
	_, err := db.Model((*Notification)(nil)).
		Context(ctx).
		Set("read_at = now()").
		Set("updated_at = now()").
		Where("user_id = ? AND read_at IS NULL", userID).
		Update()
	if err != nil {
		return errors.Wrap(err, "failed to mark all notifications read")
	}
	return nil
}

// PruneNotificationsKeepingNewest deletes all but the newest `keep` entries
// for every user. The ranking is computed per user_id via a window
// function and only rows past the cutoff for their own partition are
// deleted -- a global "keep the newest N rows" query (e.g. a plain
// `ORDER BY created_at OFFSET keep`) would let one busy account's recent
// notifications evict a quiet account's older, still-under-the-cap ones.
// Rows with no user_id (pre-migration entries with no owner to bound a feed
// for) are left untouched.
func PruneNotificationsKeepingNewest(ctx context.Context, db pg.DBI, keep int) error {
	_, err := db.ExecContext(ctx, `
		DELETE FROM notification
		WHERE notification_id IN (
			SELECT notification_id FROM (
				SELECT notification_id,
					ROW_NUMBER() OVER (
						PARTITION BY user_id
						ORDER BY created_at DESC, notification_id DESC
					) AS rank
				FROM notification
				WHERE user_id IS NOT NULL
			) ranked
			WHERE ranked.rank > ?
		)
	`, keep)
	if err != nil {
		return errors.Wrap(err, "failed to prune notifications")
	}
	return nil
}
