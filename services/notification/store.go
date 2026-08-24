package notification

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

type notificationStore interface {
	GetLastMailedByKeyAndUser(ctx context.Context, key string, userID uuid.UUID) (*models.Notification, error)
	Create(ctx context.Context, n *models.Notification) error
	MarkMailed(ctx context.Context, id uuid.UUID) error
}

type pgNotificationStore struct {
	db *pg.DB
}

func (s *pgNotificationStore) GetLastMailedByKeyAndUser(ctx context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	return models.GetLastMailedNotificationByKeyAndUser(ctx, s.db, key, userID)
}

func (s *pgNotificationStore) Create(ctx context.Context, n *models.Notification) error {
	return models.CreateNotification(ctx, s.db, n)
}

func (s *pgNotificationStore) MarkMailed(ctx context.Context, id uuid.UUID) error {
	return models.MarkNotificationMailed(ctx, s.db, id)
}
