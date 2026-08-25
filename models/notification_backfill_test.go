package models

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// TestBackfillAdoptsOrphanJournalRows runs migration 72's own SQL against a
// real Postgres.
//
// The migration cannot be observed the usual way: the harness applies every
// migration before a test gets the database, so by then it has run over an
// empty table. Executing the file's own text over fixture rows is what
// actually pins the statement -- and it has to be the file, not a copy, or
// the test drifts away from the thing that ships.
func TestBackfillAdoptsOrphanJournalRows(t *testing.T) {
	db := startTestPostgres(t)

	owner := createTestUser(t, db, "owner@example.com")

	// The shape every pre-feed row has: addressed to someone, owned by no one.
	created := time.Now().Add(-90 * 24 * time.Hour)
	mine := insertOrphanNotification(t, db, "owner@example.com", "vaulted-mine", created)
	// Addressed to an account that does not exist.
	stranger := insertOrphanNotification(t, db, "gone@example.com", "vaulted-stranger", created)

	if _, err := db.Exec(readMigration(t, "72_notification_backfill_user.up.sql")); err != nil {
		t.Fatalf("run backfill: %v", err)
	}

	got := readNotification(t, db, mine)
	if got.UserID == nil {
		t.Fatal("row addressed to a real account was left ownerless -- it stays out of every feed, which is the bug this migration fixes")
	}
	if *got.UserID != owner {
		t.Errorf("adopted by %s, want %s", *got.UserID, owner)
	}
	if got.ReadAt == nil {
		t.Error("adopted row left unread -- years of already-delivered mail would land in the navbar badge")
	}
	if got.MailedAt != nil {
		t.Error("backfill stamped mailed_at; it must not assert a delivery it cannot verify -- an instance with no SMTP wrote these rows too")
	}

	other := readNotification(t, db, stranger)
	if other.UserID != nil {
		t.Errorf("row addressed to %q was adopted by a user; only an exact identity-email match may be", "gone@example.com")
	}
}

func insertOrphanNotification(t *testing.T, db *pg.DB, to, key string, createdAt time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	_, err := db.QueryOne(pg.Scan(&id),
		`INSERT INTO notification (key, title, template, body, "to", created_at, updated_at)
		 VALUES (?, 'T', 't.html', 'b', ?, ?, ?) RETURNING notification_id`,
		key, to, createdAt, createdAt)
	if err != nil {
		t.Fatalf("insert orphan notification: %v", err)
	}
	return id
}

func readNotification(t *testing.T, db *pg.DB, id uuid.UUID) *Notification {
	t.Helper()
	n := &Notification{NotificationID: id}
	if err := db.Model(n).WherePK().Select(); err != nil {
		t.Fatalf("read notification %s: %v", id, err)
	}
	return n
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "migrations", name))
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(b)
}
