package notification

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/webtor-io/web-ui/models"
)

type notificationStore interface {
	GetLastByKeyAndTo(ctx context.Context, key, to string) (*models.Notification, error)
	Create(ctx context.Context, n *models.Notification) error
}

type pgNotificationStore struct {
	db *pg.DB
}

func (s *pgNotificationStore) GetLastByKeyAndTo(ctx context.Context, key, to string) (*models.Notification, error) {
	return models.GetLastNotificationByKeyAndTo(ctx, s.db, key, to)
}

func (s *pgNotificationStore) Create(ctx context.Context, n *models.Notification) error {
	return models.CreateNotification(ctx, s.db, n)
}
