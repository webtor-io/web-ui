package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/notification"
)

// --- shared fakes for the notification service ---
//
// These let a test in this package drive the real notification.Service
// (via notification.NewWith) instead of a recording mock, which is what
// makes "a feed row was written" an assertion rather than an inference.
// vault_test.go uses them too.

// memJournal is the notification table. GetLastMailedByKeyAndUser only ever
// considers rows with MailedAt set, the same restriction the real SQL
// applies -- a row nobody ever mailed must not look like a duplicate.
type memJournal struct {
	mu   sync.Mutex
	rows []*models.Notification
}

func (j *memJournal) GetLastMailedByKeyAndUser(_ context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.rows) - 1; i >= 0; i-- {
		r := j.rows[i]
		if r.Key == key && r.UserID != nil && *r.UserID == userID && r.MailedAt != nil {
			return r, nil
		}
	}
	return nil, nil
}

// GetLastByKeyAndUser is the feed guard's read: the newest row for this key
// and user whether or not it was ever mailed. No MailedAt condition here --
// that is the whole difference from the method above, and dropping it is
// what lets a redelivered event find its existing entry instead of adding a
// second one.
func (j *memJournal) GetLastByKeyAndUser(_ context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.rows) - 1; i >= 0; i-- {
		r := j.rows[i]
		if r.Key == key && r.UserID != nil && *r.UserID == userID {
			return r, nil
		}
	}
	return nil, nil
}

func (j *memJournal) Create(_ context.Context, n *models.Notification) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if n.NotificationID == (uuid.UUID{}) {
		n.NotificationID = uuid.NewV4()
	}
	n.CreatedAt = time.Now()
	j.rows = append(j.rows, n)
	return nil
}

func (j *memJournal) MarkMailed(_ context.Context, id uuid.UUID, to string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	for _, r := range j.rows {
		if r.NotificationID == id {
			r.MailedAt = &now
		}
	}
	return nil
}

func (j *memJournal) CountUnread(_ context.Context, userID uuid.UUID) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	n := 0
	for _, r := range j.rows {
		if r.UserID != nil && *r.UserID == userID && r.ReadAt == nil {
			n++
		}
	}
	return n, nil
}

func (j *memJournal) ListByUser(_ context.Context, userID uuid.UUID, limit int) ([]models.Notification, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []models.Notification
	for i := len(j.rows) - 1; i >= 0 && len(out) < limit; i-- {
		if r := j.rows[i]; r.UserID != nil && *r.UserID == userID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (j *memJournal) MarkAllRead(_ context.Context, userID uuid.UUID) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	for _, r := range j.rows {
		if r.UserID != nil && *r.UserID == userID && r.ReadAt == nil {
			r.ReadAt = &now
		}
	}
	return nil
}

func (j *memJournal) PruneKeepingNewest(_ context.Context, _ int) error { return nil }

// recordingMailer is the SMTP transport, minus the SMTP. Configured reports
// true so that a message the service declines to send is the service's own
// decision about the address, not a missing mail server.
type recordingMailer struct {
	mu   sync.Mutex
	sent []string
}

func (m *recordingMailer) Send(to, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, to)
	return nil
}

// --- tests for expiringRecipients ---

func expiringPledge(userID uuid.UUID, user *models.User) vaultModels.Pledge {
	return vaultModels.Pledge{
		PledgeID:   uuid.NewV4(),
		ResourceID: "res1",
		UserID:     userID,
		Amount:     1.0,
		Funded:     true,
		User:       user,
	}
}

// The expiry cron's recipient set keeps an account whose address cannot be
// mailed. Dropping it here is what used to make the whole run invisible to
// a self-hosted admin: the address is the sentinel "admin", so the account
// would be filtered out before anything was sent, and with it went the feed
// entry that is the only warning such an instance ever gets.
func TestExpiringRecipientsKeepsUndeliverableAddress(t *testing.T) {
	adminID := uuid.NewV4()
	userID := uuid.NewV4()

	got := expiringRecipients([]vaultModels.Pledge{
		expiringPledge(adminID, &models.User{UserID: adminID, Email: "admin"}),
		expiringPledge(userID, &models.User{UserID: userID, Email: "viewer@example.com"}),
	})

	if len(got) != 2 {
		t.Fatalf("recipients: got %d, want 2 — an unmailable account is still owed a feed entry: %v", len(got), got)
	}
	if got[adminID.String()] != "admin" {
		t.Errorf("admin address: got %q, want %q", got[adminID.String()], "admin")
	}
	if got[userID.String()] != "viewer@example.com" {
		t.Errorf("viewer address: got %q", got[userID.String()])
	}
}

// A pledge with no account joined is still dropped: there is no user to
// attribute a notification to, so there is nothing to write.
func TestExpiringRecipientsDropsPledgeWithoutUser(t *testing.T) {
	got := expiringRecipients([]vaultModels.Pledge{expiringPledge(uuid.NewV4(), nil)})
	if len(got) != 0 {
		t.Fatalf("recipients: got %v, want none — a pledge without an account has no recipient", got)
	}
}

// A confirmed notification address wins over the account address, the same
// choice notification.RecipientEmail makes everywhere else.
func TestExpiringRecipientsPrefersNotificationAddress(t *testing.T) {
	adminID := uuid.NewV4()
	notify := "ops@example.com"

	got := expiringRecipients([]vaultModels.Pledge{
		expiringPledge(adminID, &models.User{UserID: adminID, Email: "admin", NotificationEmail: &notify}),
	})

	if got[adminID.String()] != notify {
		t.Errorf("address: got %q, want the confirmed notification address %q", got[adminID.String()], notify)
	}
}

// End to end for the site: the address expiringRecipients kept is handed to
// the real notification service, and a feed row comes out of it. This is
// what the caller-side guard used to prevent -- not a letter (there was
// never going to be one), but the entry itself.
func TestExpiringNotificationReachesTheFeedForUndeliverableAddress(t *testing.T) {
	adminID := uuid.NewV4()
	mail := &recordingMailer{}
	ns := notification.NewWith(&memJournal{}, mail, nil, "https://webtor.io", "templates/notification")

	recipients := expiringRecipients([]vaultModels.Pledge{
		expiringPledge(adminID, &models.User{UserID: adminID, Email: "admin"}),
	})
	addr, ok := recipients[adminID.String()]
	if !ok {
		t.Fatalf("the unmailable account was filtered out of the recipient set")
	}

	resources := []vaultModels.Resource{{ResourceID: "res1", Name: "Big Buck Bunny"}}
	if err := ns.SendExpiring(addr, adminID, 3, resources); err != nil {
		t.Fatalf("send expiring: %v", err)
	}

	rows, err := ns.ListByUser(context.Background(), adminID, 10)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("feed rows: got %d, want 1 — the entry IS the notification when there is no mailbox", len(rows))
	}
	if rows[0].Key != "expiring-3" {
		t.Errorf("feed row key: got %q, want %q", rows[0].Key, "expiring-3")
	}
	if rows[0].UserID == nil || *rows[0].UserID != adminID {
		t.Errorf("feed row is not attributed to the user: %v", rows[0].UserID)
	}
	if rows[0].To != nil {
		t.Errorf("feed row carries To=%q; an undeliverable address must not be recorded as a destination", *rows[0].To)
	}
	if !strings.Contains(rows[0].Body, "Big Buck Bunny") {
		t.Errorf("feed row body does not name the expiring resource:\n%s", rows[0].Body)
	}
	if len(mail.sent) != 0 {
		t.Errorf("letters sent: %v, want none — there is no address to send to", mail.sent)
	}
}
