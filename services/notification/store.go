package notification

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
)

type notificationStore interface {
	// GetLastMailedByKeyAndUser backs the mail guard: the newest row for
	// this key and user whose letter actually went out.
	GetLastMailedByKeyAndUser(ctx context.Context, key string, userID uuid.UUID) (*models.Notification, error)
	// GetLastByKeyAndUser backs the feed guard: the newest row for this key
	// and user regardless of whether a letter ever left. An implementation
	// that filters on mailed_at here reintroduces duplicate feed entries on
	// redelivery -- that is the difference between the two methods, not an
	// accident of naming.
	GetLastByKeyAndUser(ctx context.Context, key string, userID uuid.UUID) (*models.Notification, error)
	Create(ctx context.Context, n *models.Notification) error
	MarkMailed(ctx context.Context, id uuid.UUID, to string) error
	CountUnread(ctx context.Context, userID uuid.UUID) (int, error)
	ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]models.Notification, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
	// PruneKeepingNewest deletes all but the newest `keep` entries for every
	// user. Rows are kept per user rather than globally: a global cap would
	// let one busy account evict a quiet one's only notification.
	PruneKeepingNewest(ctx context.Context, keep int) error
	// AccountLang returns the language the account browses in
	// (user_settings.lang), or "" when it has never been observed. It is
	// what lets a letter assembled far from any HTTP request -- a NATS
	// event handler, a cron run -- speak the reader's language. "" falls
	// back to the default language, never to an error: a lookup failure
	// must not stop a send.
	AccountLang(ctx context.Context, userID uuid.UUID) string
}

type pgNotificationStore struct {
	db *pg.DB
}

func (s *pgNotificationStore) GetLastMailedByKeyAndUser(ctx context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	return models.GetLastMailedNotificationByKeyAndUser(ctx, s.db, key, userID)
}

func (s *pgNotificationStore) GetLastByKeyAndUser(ctx context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	return models.GetLastNotificationByKeyAndUser(ctx, s.db, key, userID)
}

func (s *pgNotificationStore) Create(ctx context.Context, n *models.Notification) error {
	return models.CreateNotification(ctx, s.db, n)
}

func (s *pgNotificationStore) MarkMailed(ctx context.Context, id uuid.UUID, to string) error {
	return models.MarkNotificationMailed(ctx, s.db, id, to)
}

func (s *pgNotificationStore) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	return models.CountUnreadNotifications(ctx, s.db, userID)
}

func (s *pgNotificationStore) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]models.Notification, error) {
	return models.ListNotificationsByUser(ctx, s.db, userID, limit)
}

func (s *pgNotificationStore) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return models.MarkAllNotificationsRead(ctx, s.db, userID)
}

func (s *pgNotificationStore) PruneKeepingNewest(ctx context.Context, keep int) error {
	return models.PruneNotificationsKeepingNewest(ctx, s.db, keep)
}

// AccountLang mirrors release_subscription's pgStore.AccountLang (the same
// question asked by subscription letters); errors are swallowed because the
// caller always has a fallback -- the default language.
func (s *pgNotificationStore) AccountLang(ctx context.Context, userID uuid.UUID) string {
	us, err := models.GetUserSettings(ctx, s.db, userID)
	if err != nil {
		log.WithError(err).WithField("user_id", userID).Warn("failed to read account language for notification")
		return ""
	}
	return us.GetLang()
}
