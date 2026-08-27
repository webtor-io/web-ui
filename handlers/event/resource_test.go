package event

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

// --- fakes ---

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

// AccountLang is not exercised by this package's tests -- they assert on
// the event-handling wiring, not on letter language -- but the type still
// has to satisfy notificationStore.
func (j *memJournal) AccountLang(_ context.Context, _ uuid.UUID) string { return "" }

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

func newTestNotificationService(j *memJournal, m *recordingMailer) *notification.Service {
	return notification.NewWith(j, m, nil, "https://webtor.io", "../../templates/notification")
}

// --- tests ---

// A pledger whose address cannot be mailed is still told their resource was
// vaulted. The self-hosted admin account carries the literal "admin"
// sentinel (services/adminauth/pg_repo.go), and on a default self-hosted
// instance it is the only account there is -- so a guard here would make the
// vaulted notification unreachable for every user the feed exists for.
//
// Asserted against the real notification.Service over a real journal rather
// than a recording notifier: what has to hold is that a feed row lands, and
// that decision is Service.Send's.
func TestNotifyVaultedReachesTheFeedForUndeliverableAddress(t *testing.T) {
	journal := &memJournal{}
	mail := &recordingMailer{}
	ns := newTestNotificationService(journal, mail)

	adminID := uuid.NewV4()
	r := &vaultModels.Resource{ResourceID: "res1", Name: "Big Buck Bunny"}

	err := notifyVaulted(ns, []models.User{{UserID: adminID, Email: "admin"}}, r)
	if err != nil {
		t.Fatalf("notify vaulted: %v", err)
	}

	rows, err := ns.ListByUser(context.Background(), adminID, 10)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("feed rows: got %d, want 1 — the entry IS the notification when there is no mailbox", len(rows))
	}
	if rows[0].Key != "vaulted-res1" {
		t.Errorf("feed row key: got %q, want %q", rows[0].Key, "vaulted-res1")
	}
	if rows[0].UserID == nil || *rows[0].UserID != adminID {
		t.Errorf("feed row is not attributed to the user: %v", rows[0].UserID)
	}
	if rows[0].To != nil {
		t.Errorf("feed row carries To=%q; an undeliverable address must not be recorded as a destination", *rows[0].To)
	}
	if !strings.Contains(rows[0].Body, "Big Buck Bunny") {
		t.Errorf("feed row body does not name the resource:\n%s", rows[0].Body)
	}
	if len(mail.sent) != 0 {
		t.Errorf("letters sent: %v, want none — there is no address to send to", mail.sent)
	}
}

// A mailable pledger still gets both: the feed row and the letter. This is
// the control on the test above -- it pins that dropping the caller-side
// guard did not turn the mail path off.
func TestNotifyVaultedMailsADeliverableAddress(t *testing.T) {
	journal := &memJournal{}
	mail := &recordingMailer{}
	ns := newTestNotificationService(journal, mail)

	userID := uuid.NewV4()
	r := &vaultModels.Resource{ResourceID: "res1", Name: "Big Buck Bunny"}

	if err := notifyVaulted(ns, []models.User{{UserID: userID, Email: "viewer@example.com"}}, r); err != nil {
		t.Fatalf("notify vaulted: %v", err)
	}

	rows, err := ns.ListByUser(context.Background(), userID, 10)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("feed rows: got %d, want 1", len(rows))
	}
	if rows[0].To == nil || *rows[0].To != "viewer@example.com" {
		t.Errorf("feed row To: got %v, want viewer@example.com", rows[0].To)
	}
	if len(mail.sent) != 1 || mail.sent[0] != "viewer@example.com" {
		t.Errorf("letters sent: %v, want one to viewer@example.com", mail.sent)
	}
}

// Every pledger is notified, not just the mailable ones.
func TestNotifyVaultedNotifiesEveryPledger(t *testing.T) {
	journal := &memJournal{}
	mail := &recordingMailer{}
	ns := newTestNotificationService(journal, mail)

	adminID := uuid.NewV4()
	viewerID := uuid.NewV4()
	r := &vaultModels.Resource{ResourceID: "res1", Name: "Big Buck Bunny"}

	users := []models.User{
		{UserID: adminID, Email: "admin"},
		{UserID: viewerID, Email: "viewer@example.com"},
	}
	if err := notifyVaulted(ns, users, r); err != nil {
		t.Fatalf("notify vaulted: %v", err)
	}

	for _, u := range users {
		rows, err := ns.ListByUser(context.Background(), u.UserID, 10)
		if err != nil {
			t.Fatalf("list notifications for %q: %v", u.Email, err)
		}
		if len(rows) != 1 {
			t.Errorf("feed rows for %q: got %d, want 1", u.Email, len(rows))
		}
	}
	if len(mail.sent) != 1 {
		t.Errorf("letters sent: %v, want exactly one (only one address is mailable)", mail.sent)
	}
}

// A confirmed notification address wins over the account address, the same
// choice notification.RecipientEmail makes everywhere else. Worth pinning
// here because it is the one way a self-hosted admin can get real mail.
func TestNotifyVaultedPrefersNotificationAddress(t *testing.T) {
	journal := &memJournal{}
	mail := &recordingMailer{}
	ns := newTestNotificationService(journal, mail)

	adminID := uuid.NewV4()
	notify := "ops@example.com"
	r := &vaultModels.Resource{ResourceID: "res1", Name: "Big Buck Bunny"}

	users := []models.User{{UserID: adminID, Email: "admin", NotificationEmail: &notify}}
	if err := notifyVaulted(ns, users, r); err != nil {
		t.Fatalf("notify vaulted: %v", err)
	}

	if len(mail.sent) != 1 || mail.sent[0] != notify {
		t.Errorf("letters sent: %v, want one to %q", mail.sent, notify)
	}
}
