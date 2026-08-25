package models

import (
	"context"
	"testing"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
)

// insertDedupeNotification inserts one notification row with an explicit
// key, created_at and mailed_at. insertTestNotification (in
// notification_prune_test.go) hardcodes the key and never sets mailed_at,
// which is exactly the column these tests are about.
func insertDedupeNotification(t *testing.T, db *pg.DB, userID uuid.UUID, key string, createdAt time.Time, mailedAt *time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	_, err := db.QueryOne(pg.Scan(&id), `
		INSERT INTO notification (key, title, template, body, user_id, created_at, updated_at, mailed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING notification_id
	`, key, "test", "test.html", "body", userID, createdAt, createdAt, mailedAt)
	if err != nil {
		t.Fatalf("insert notification %s: %v", key, err)
	}
	return id
}

// TestGetLastNotificationByKeyAndUserSeesUnmailedRows is the difference
// between the two dedupe queries, asserted against a real server because
// the difference is one SQL predicate and nothing in Go enforces it.
//
// The feed guard in Service.Send counts a row whose letter never left --
// that row IS the notification the reader can already see, so a redelivered
// event must find it. The mail guard must not count it, because the letter
// is still owed. A copy-paste that carried "mailed_at IS NOT NULL" into the
// new query would restore duplicate feed entries on every redelivery while
// every mock-backed test in services/notification stayed green, since those
// stub the store rather than run its SQL.
func TestGetLastNotificationByKeyAndUserSeesUnmailedRows(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	user := createTestUser(t, db, "dedupe-unmailed@example.com")
	const key = "vaulted-abc123"

	// One row, letter never sent: mailed_at NULL.
	id := insertDedupeNotification(t, db, user, key, time.Now().Add(-time.Hour), nil)

	got, err := GetLastNotificationByKeyAndUser(ctx, db, key, user)
	if err != nil {
		t.Fatalf("GetLastNotificationByKeyAndUser: %v", err)
	}
	if got == nil {
		t.Fatal("the feed guard's query missed an unmailed row -- a redelivered event would add a second feed entry")
	}
	if got.NotificationID != id {
		t.Fatalf("got row %s, want %s", got.NotificationID, id)
	}

	// The mail guard's query is the negative control: the same row must be
	// invisible to it, which is what keeps the owed letter owed.
	gotMailed, err := GetLastMailedNotificationByKeyAndUser(ctx, db, key, user)
	if err != nil {
		t.Fatalf("GetLastMailedNotificationByKeyAndUser: %v", err)
	}
	if gotMailed != nil {
		t.Fatalf("the mail guard's query returned an unmailed row (%s) -- a letter that never left would suppress its own retry", gotMailed.NotificationID)
	}
}

// TestGetLastNotificationByKeyAndUserIsScopedAndOrdered pins the rest of
// the predicate: newest first, and only this key and this user. A query
// that ignored the key would let one notification suppress an unrelated
// one; a query that ignored the user would let one account's entry
// suppress another's.
func TestGetLastNotificationByKeyAndUserIsScopedAndOrdered(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	userA := createTestUser(t, db, "dedupe-scope-a@example.com")
	userB := createTestUser(t, db, "dedupe-scope-b@example.com")

	base := time.Now().Add(-10 * time.Hour)
	insertDedupeNotification(t, db, userA, "expiring-7", base, nil)
	newest := insertDedupeNotification(t, db, userA, "expiring-7", base.Add(time.Hour), nil)
	// Same user, different key; and same key, different user. Neither may
	// be returned.
	insertDedupeNotification(t, db, userA, "expiring-3", base.Add(2*time.Hour), nil)
	insertDedupeNotification(t, db, userB, "expiring-7", base.Add(3*time.Hour), nil)

	got, err := GetLastNotificationByKeyAndUser(ctx, db, "expiring-7", userA)
	if err != nil {
		t.Fatalf("GetLastNotificationByKeyAndUser: %v", err)
	}
	if got == nil {
		t.Fatal("expected a row for (expiring-7, user A)")
	}
	if got.NotificationID != newest {
		t.Fatalf("got row %s, want %s -- the query must return the newest row for this key and user only", got.NotificationID, newest)
	}

	// A key this user has never been sent has no row at all -- the caller
	// reads that as "nothing to reuse", so a false hit here would silence a
	// notification entirely rather than merely duplicate one.
	none, err := GetLastNotificationByKeyAndUser(ctx, db, "expiring-1", userA)
	if err != nil {
		t.Fatalf("GetLastNotificationByKeyAndUser: %v", err)
	}
	if none != nil {
		t.Fatalf("got row %s for a key this user never received, want none", none.NotificationID)
	}
}

// TestMarkNotificationMailedRecordsTheRecipient exercises the UPDATE against a
// real Postgres, because the column is named "to" -- a reserved word. A
// mis-quoted identifier is a syntax error at runtime, not at compile time, and
// the failure would be quiet in the worst way: MarkMailed returns an error,
// mailed_at is never stamped, and every later run inside the window decides the
// letter is still owed and sends it again.
func TestMarkNotificationMailedRecordsTheRecipient(t *testing.T) {
	db := startTestPostgres(t)
	ctx := context.Background()

	user := createTestUser(t, db, "mark-mailed@example.com")
	const key = "vaulted-mark-mailed"

	// The row an earlier attempt left behind: no address, never mailed.
	id := insertDedupeNotification(t, db, user, key, time.Now().Add(-time.Hour), nil)

	if err := MarkNotificationMailed(ctx, db, id, "confirmed@example.com"); err != nil {
		t.Fatalf("MarkNotificationMailed: %v", err)
	}

	got, err := GetLastNotificationByKeyAndUser(ctx, db, key, user)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got == nil {
		t.Fatal("row disappeared")
	}
	if got.MailedAt == nil {
		t.Error("mailed_at was not stamped")
	}
	if got.To == nil {
		t.Fatal("row is stamped as mailed but records no recipient")
	}
	if *got.To != "confirmed@example.com" {
		t.Errorf("recipient recorded as %q, want confirmed@example.com", *got.To)
	}
}
