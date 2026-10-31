package models

import (
	"context"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models/pgtest"
)

// startTestPostgres is pgtest.Start: a disposable Postgres with this repo's
// migrations applied.
//
// PruneNotificationsKeepingNewest's whole risk is a window-function
// partition boundary: whether DELETE ranks rows per user_id or across the
// whole table. That is a property of the SQL actually sent to a real
// server, not of Go code -- every other store test in this codebase (see
// services/notification/notification_test.go's mockStore) stubs the
// interface instead, which cannot catch this class of bug. Hence a real,
// disposable database for this one test.
func startTestPostgres(t *testing.T) *pg.DB {
	return pgtest.Start(t)
}

// createTestUser inserts a minimal row into "user" -- notification.user_id
// carries a foreign key to it -- and returns the generated id.
func createTestUser(t *testing.T, db *pg.DB, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	_, err := db.QueryOne(pg.Scan(&id), `INSERT INTO "user" (email) VALUES (?) RETURNING user_id`, email)
	if err != nil {
		t.Fatalf("insert test user %s: %v", email, err)
	}
	return id
}

// insertTestNotification inserts a notification row with an explicit
// created_at, bypassing the ORM's DEFAULT-on-zero-value handling so the
// test controls ordering directly instead of relying on real-time spacing.
func insertTestNotification(t *testing.T, db *pg.DB, userID uuid.UUID, createdAt time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	_, err := db.QueryOne(pg.Scan(&id), `
		INSERT INTO notification (key, title, template, body, user_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING notification_id
	`, "test-key", "test", "test.html", "body", userID, createdAt, createdAt)
	if err != nil {
		t.Fatalf("insert test notification: %v", err)
	}
	return id
}

// TestPruneNotificationsKeepingNewestPerUserPartition is the point of this
// task's Step 6: PruneNotificationsKeepingNewest must rank rows per user,
// not across the whole table. User A is given 105 notifications, all more
// recent than every one of user B's 3 -- the arrangement under which a
// naive `DELETE ... ORDER BY created_at DESC OFFSET keep` (no PARTITION BY
// user_id) still leaves user A with exactly the newest 100 (so a test
// asserting only that would pass) while deleting user B's entire history,
// because none of user B's rows rank in the global top 100. See this
// task's report for the manual psql run that reproduced exactly that
// failure against the naive query before this test was written.
func TestPruneNotificationsKeepingNewestPerUserPartition(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	userA := createTestUser(t, db, "prune-test-user-a@example.com")
	userB := createTestUser(t, db, "prune-test-user-b@example.com")

	const (
		aTotal = 105
		keep   = 100
	)

	// User A: 105 notifications, all within the last two minutes.
	recentBase := time.Now().Add(-2 * time.Minute)
	aIDs := make([]uuid.UUID, aTotal)
	for i := 0; i < aTotal; i++ {
		aIDs[i] = insertTestNotification(t, db, userA, recentBase.Add(time.Duration(i)*time.Second))
	}
	// aIDs is oldest-to-newest; the newest `keep` are the last `keep` entries.
	wantKeptA := aIDs[aTotal-keep:]

	// User B: 3 notifications, all far older than every one of user A's --
	// this is what makes a global (non-partitioned) cutoff wipe user B out
	// entirely rather than merely truncating it.
	oldBase := time.Now().Add(-72 * time.Hour)
	bIDs := []uuid.UUID{
		insertTestNotification(t, db, userB, oldBase),
		insertTestNotification(t, db, userB, oldBase.Add(1*time.Hour)),
		insertTestNotification(t, db, userB, oldBase.Add(2*time.Hour)),
	}

	if err := PruneNotificationsKeepingNewest(ctx, db, keep); err != nil {
		t.Fatalf("PruneNotificationsKeepingNewest: %v", err)
	}

	// Assertion 1: user A, who exceeded the cap, keeps exactly the newest
	// `keep` entries.
	var remainingA []Notification
	if err := db.Model(&remainingA).Where("user_id = ?", userA).Order("created_at ASC").Select(); err != nil {
		t.Fatalf("select remaining notifications for user A: %v", err)
	}
	if len(remainingA) != keep {
		t.Fatalf("user A: %d notifications remain, want %d", len(remainingA), keep)
	}
	for i, n := range remainingA {
		if n.NotificationID != wantKeptA[i] {
			t.Fatalf("user A: remaining[%d] = %s, want %s (the newest %d must survive, in order)", i, n.NotificationID, wantKeptA[i], keep)
		}
	}

	// Assertion 2: user B, who never approached the cap, is untouched. This
	// is the assertion that actually distinguishes a per-user partition
	// from a global cutoff -- see the manual psql reproduction referenced
	// in the doc comment above, where the naive query passed assertion 1
	// but zeroed this one out.
	var remainingB []Notification
	if err := db.Model(&remainingB).Where("user_id = ?", userB).Order("created_at ASC").Select(); err != nil {
		t.Fatalf("select remaining notifications for user B: %v", err)
	}
	if len(remainingB) != len(bIDs) {
		t.Fatalf("user B: %d notifications remain, want %d (untouched) -- pruning user A's feed must not touch user B's history", len(remainingB), len(bIDs))
	}
	for i, n := range remainingB {
		if n.NotificationID != bIDs[i] {
			t.Fatalf("user B: remaining[%d] = %s, want %s -- user B's history was disturbed by pruning user A's feed", i, n.NotificationID, bIDs[i])
		}
	}
}
