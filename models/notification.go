package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type Notification struct {
	tableName      struct{}  `pg:"notification"`
	NotificationID uuid.UUID `pg:"notification_id,pk,type:uuid,default:uuid_generate_v4()"`
	Key            string    `pg:"key,notnull"`
	Title          string    `pg:"title,notnull"`
	Template       string    `pg:"template,notnull"`
	Body           string    `pg:"body,notnull"`
	To             string    `pg:"to,notnull"`
	CreatedAt      time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt      time.Time `pg:"updated_at,notnull,default:now()"`
}

// CreateNotification creates a new notification record
func CreateNotification(ctx context.Context, db pg.DBI, n *Notification) error {
	_, err := db.Model(n).Context(ctx).Insert()
	if err != nil {
		return errors.Wrap(err, "failed to create notification")
	}
	return nil
}

// GetLastNotificationByKeyAndTo returns the last notification by key and recipient
func GetLastNotificationByKeyAndTo(ctx context.Context, db pg.DBI, key string, to string) (*Notification, error) {
	n := &Notification{}
	err := db.Model(n).
		Context(ctx).
		Where("key = ? AND \"to\" = ?", key, to).
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
