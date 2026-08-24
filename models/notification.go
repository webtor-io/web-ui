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
